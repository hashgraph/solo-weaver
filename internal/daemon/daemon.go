// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/automa-saga/daemonkit"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon/blocknode"
	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/tools/clientcmd"
)

// component groups the MonitorRunner instances for one daemon component (e.g.
// consensus-node). Each monitor runs in its own supervised goroutine started by
// componentSupervisor.
//
// probe is optional: components with no external dependencies (host-only) leave
// it nil and are treated as immediately ready by the composite probe runner.
// tracker records per-monitor state for the /status endpoint.
type component struct {
	name     string
	monitors []daemonkit.MonitorRunner
	probe    daemonkit.ComponentProbe
	tracker  *daemonkit.StatusTracker
}

// Daemon is the controller for solo-provisioner-daemon. It composes the
// sub-systems and owns their lifecycle via Run.
//
// Goroutine map:
//   - Socket server        — always on; HTTP control plane on daemon.sock
//   - componentSupervisor  — one supervised goroutine per enabled monitor;
//     crashes are absorbed per-monitor with exponential back-off (#662/#663)
//   - runComponentProbes   — background loop; retries disk probes until all
//     prerequisites are satisfied; results visible via GET /status
type Daemon struct {
	paths      models.WeaverPaths
	cfg        DaemonConfig
	server     *daemonkit.Server
	components []component
	// probeErrors holds the last probe result per component name.
	// nil = all probes passed (or no probes). Written by runComponentProbes,
	// read by statusSnapshot — both via atomic.Pointer to avoid locks.
	probeErrors atomic.Pointer[map[string]daemonkit.StatusError]
}

// New constructs a Daemon from WeaverPaths. It reads daemon.yaml from
// paths.DaemonConfigPath and fails fast if the config is missing or invalid.
func New(paths models.WeaverPaths) (*Daemon, error) {
	cfg, err := LoadDaemonConfig(paths.DaemonConfigPath)
	if err != nil {
		return nil, err
	}
	return NewFromConfig(paths, cfg)
}

// NewFromConfig constructs a Daemon from a pre-resolved DaemonConfig.
// Components are skipped when their Enabled flag is false. Individual monitors
// within a component are skipped when their toggle is false.
func NewFromConfig(paths models.WeaverPaths, cfg DaemonConfig) (*Daemon, error) {
	var components []component
	var componentHandlers []daemonkit.ComponentHandler

	cn := cfg.Components.ConsensusNode
	if cn != nil && cn.Enabled {
		result, err := consensus.NewComponent(consensus.ComponentConfig{
			NodeID:           cn.NodeID,
			KubeconfigPath:   cn.Kubeconfig,
			Orbit:            cn.Orbit,
			UpgradeEnabled:   cn.Monitors.Upgrade,
			MigrationEnabled: cn.Monitors.Migration,
			UpgradeEventsDir: paths.DaemonConsensusUpgradeEventsDir,
			HomeDir:          paths.HomeDir,
			UpgradeDir:       cn.EffectiveUpgradeDir(),
			MigrateEventsDir: paths.DaemonConsensusMigrateEventsDir,
		})
		if err != nil {
			return nil, err
		}
		if len(result.Monitors) > 0 {
			comp := component{
				name:     "consensus-node",
				monitors: result.Monitors,
				probe:    daemonkit.BuildComponentProbe("consensus-node", result.Monitors),
				tracker:  daemonkit.NewStatusTracker(),
			}
			components = append(components, comp)

			if result.MigrationMonitor != nil {
				// migrationStateFn captures comp.tracker by reference — safe because
				// comp is appended to the slice and never moved after this point.
				migrationStateFn := func() daemonkit.MonitorState {
					return comp.tracker.Snapshot()[result.MigrationMonitor.Name()]
				}
				componentHandlers = append(componentHandlers,
					consensus.NewConsensusNodeHandler(result.MigrationMonitor, migrationStateFn))
			}
		}
	}

	bn := cfg.Components.BlockNode
	if bn != nil && bn.Enabled {
		result, err := blocknode.NewComponent(blocknode.ComponentConfig{
			TrafficShaperEnabled: bn.Monitors.TrafficShaper,
		})
		if err != nil {
			return nil, err
		}
		if len(result.Monitors) > 0 {
			comp := component{
				name:     "block-node",
				monitors: result.Monitors,
				probe:    nil,
				tracker:  daemonkit.NewStatusTracker(),
			}
			components = append(components, comp)

			if result.TrafficShaperMonitor != nil {
				// trafficShaperStateFn captures comp.tracker by reference — safe
				// because comp is appended to the slice and never moved after
				// this point.
				trafficShaperStateFn := func() daemonkit.MonitorState {
					return comp.tracker.Snapshot()[result.TrafficShaperMonitor.Name()]
				}
				componentHandlers = append(componentHandlers,
					blocknode.NewBlockNodeHandler(result.TrafficShaperMonitor, trafficShaperStateFn))
			}
		}
	}

	d := &Daemon{
		paths:      paths,
		cfg:        cfg,
		components: components,
	}
	d.server = daemonkit.NewServer(paths.DaemonSockPath, daemonkit.ServerOptions{
		StatusFn:          func() any { return d.statusSnapshot() },
		ComponentHandlers: componentHandlers,
		// daemonkit is silent by default; route its logs through the global
		// slog default, which cmd/daemon/main.go binds to the logx handler.
		Logger: slog.Default(),
	}, daemonkit.ServerConfig{})
	return d, nil
}

// componentSupervisor starts one supervised goroutine per monitor in every
// enabled component. It returns nil only after all monitors have stopped.
func (d *Daemon) componentSupervisor(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, comp := range d.components {
		tracker := comp.tracker
		for _, m := range comp.monitors {
			wg.Add(1)
			m := m
			go func() {
				defer wg.Done()
				daemonkit.SupervisedMonitor(ctx, m, daemonkit.SupervisorOptions{
					Tracker: tracker,
					Logger:  slog.Default(),
				})
			}()
		}
	}
	wg.Wait()
	logx.As().Info().
		Str("reason", "ComponentSupervisorStopped").
		Msg("All component monitors stopped")
	return nil
}

// componentProbeInterval is the delay between probe retry rounds.
// Overridable in tests via init() to avoid 30-second waits.
var componentProbeInterval = 30 * time.Second

// runComponentProbes is a background loop that retries each component's disk
// prerequisite probe every componentProbeInterval until all pass or ctx is cancelled.
// Results are stored in d.probeErrors and surfaced via GET /status so that
// `solo-provisioner daemon service check` can report them with actionable warnings.
// This loop does not gate READY=1 — the daemon is functional (socket listening)
// as soon as it starts; component readiness is a separate concern.
func (d *Daemon) runComponentProbes(ctx context.Context) {
	componentProbes := make([]daemonkit.ComponentProbe, 0, len(d.components))
	for _, comp := range d.components {
		if comp.probe != nil {
			componentProbes = append(componentProbes, comp.probe)
		}
	}
	if len(componentProbes) == 0 {
		return
	}

	// firstFailedAt records when each component's probe first started failing
	// so that StatusError.Since reflects the outage start, not the last check.
	firstFailedAt := make(map[string]time.Time, len(componentProbes))

	for {
		errs := make(map[string]daemonkit.StatusError, len(componentProbes))
		var wg sync.WaitGroup
		var mu sync.Mutex

		for _, p := range componentProbes {
			wg.Add(1)
			p := p
			go func() {
				defer wg.Done()
				err := p.Probe(ctx)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					// Preserve the original failure timestamp across retries.
					if _, seen := firstFailedAt[p.ComponentName()]; !seen {
						firstFailedAt[p.ComponentName()] = time.Now()
					}
					se := daemonkit.StatusError{
						Reason:  "ComponentProbeError",
						Message: err.Error(),
						Since:   firstFailedAt[p.ComponentName()].UTC().Format(time.RFC3339),
					}
					// Read reason and resolution attached by daemonkit.TaggedProbe
					// directly from the kit-native ProbeError fields. The kit carries
					// these as plain struct fields (no errorx property registry), so
					// the shared pkg/models registry stays untouched.
					var pe *daemonkit.ProbeError
					if errors.As(err, &pe) {
						if pe.Reason != "" {
							se.Reason = pe.Reason
						}
						if pe.Resolution != "" {
							se.Resolution = pe.Resolution
						}
					}
					errs[p.ComponentName()] = se
					logx.As().Warn().Err(err).
						Str("reason", "ComponentProbeNotReady").
						Str("component", p.ComponentName()).
						Msg("Component prerequisites not yet satisfied — run: solo-provisioner daemon service check")
				} else {
					delete(firstFailedAt, p.ComponentName())
				}
			}()
		}
		wg.Wait()

		if len(errs) == 0 {
			d.probeErrors.Store(nil)
			logx.As().Info().Str("reason", "AllComponentProbesReady").
				Msg("All component prerequisites satisfied")
			// Intentional one-shot exit: once all probes pass we stop re-checking.
			// The assumption is that RBAC and disk permissions are stable after
			// initial setup. If a prerequisite is later revoked, the monitor's own
			// ConnectivityError / crash path surfaces the failure via /status —
			// we do not need a separate background re-probe cycle for that.
			return
		}
		d.probeErrors.Store(&errs)

		select {
		case <-ctx.Done():
			return
		case <-time.After(componentProbeInterval):
		}
	}
}

// statusSnapshot builds a StatusResponse from the current tracker snapshots
// and the latest probe results. ProbeErrors is non-empty when any component's
// disk prerequisites are not yet satisfied.
func (d *Daemon) statusSnapshot() StatusResponse {
	resp := StatusResponse{
		Components: make(map[string]ComponentStatus, len(d.components)),
	}
	for _, comp := range d.components {
		cs := ComponentStatus{
			Monitors: make(map[string]daemonkit.MonitorState),
		}
		if comp.tracker != nil {
			for name, state := range comp.tracker.Snapshot() {
				cs.Monitors[name] = state
			}
		}
		// Overlay connectivity errors from any monitor that implements
		// ConnectivityMonitor. The tracker only knows whether the goroutine
		// is alive ("running"); this step surfaces watch-loop failures that
		// are invisible to the supervisor (the goroutine is alive and retrying).
		for _, m := range comp.monitors {
			if cm, ok := m.(daemonkit.ConnectivityMonitor); ok {
				if cerr := cm.ConnectivityError(); cerr != nil {
					ms := cs.Monitors[m.Name()]
					ms.State = "degraded"
					ms.Error = cerr
					cs.Monitors[m.Name()] = ms
				}
			}
		}
		resp.Components[comp.name] = cs
	}
	if pe := d.probeErrors.Load(); pe != nil && len(*pe) > 0 {
		resp.ProbeErrors = *pe
	}
	return resp
}

// Run starts all sub-systems and blocks until ctx is cancelled or a critical
// sub-system exits with an error.
func (d *Daemon) Run(ctx context.Context) error {
	defer func() { _ = daemonkit.NotifyStopping() }()

	defer func() {
		if r := recover(); r != nil {
			logx.As().Error().
				Str("reason", "DaemonPanic").
				Interface("panic", r).
				Msg("Unhandled panic in daemon — exiting for systemd restart")
			_ = daemonkit.NotifyStopping()
			os.Exit(2)
		}
	}()

	// Close any monitor that implements io.Closer (e.g. MigrationMonitor closing
	// its event logger). Deferred so it runs after the supervisor has fully stopped.
	defer func() {
		for _, comp := range d.components {
			for _, m := range comp.monitors {
				if c, ok := m.(interface{ Close() error }); ok {
					if err := c.Close(); err != nil {
						logx.As().Warn().Err(err).
							Str("reason", "MonitorCloseFailed").
							Str("monitor", m.Name()).
							Msg("Failed to close monitor")
					}
				}
			}
		}
	}()

	// Preflight: each enabled component's kubeconfig must exist and be parseable.
	if cn := d.cfg.Components.ConsensusNode; cn != nil && cn.Enabled {
		if _, err := clientcmd.BuildConfigFromFlags("", cn.Kubeconfig); err != nil {
			return errorx.ExternalError.Wrap(err, "consensus-node kubeconfig preflight failed — daemon cannot start")
		}
	}
	// block-node kubeconfig preflight is deferred until the traffic-shaper monitor
	// requires K8s access (currently stubbed; it polls a remote API only).

	// READY=1 means the daemon process is up and its socket is serving.
	// Component prerequisite health is tracked separately by runComponentProbes
	// and surfaced via GET /status — operators use `daemon service check` to
	// inspect it. This keeps systemd's start timeout from firing on environments
	// where disk prerequisites (upgrade dir, permissions) are not yet in place.
	if err := daemonkit.NotifyReady(); err != nil {
		logx.As().Warn().Err(err).Str("reason", "SdNotifyReadyFailed").Msg("Failed to send READY=1 to systemd")
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return d.server.Start(ctx) })
	eg.Go(func() error { return d.componentSupervisor(ctx) })
	// Tracked in the errgroup so Run awaits it (nil return never cancels the group)
	eg.Go(func() error {
		d.runComponentProbes(ctx)
		return nil
	})

	return eg.Wait()
}
