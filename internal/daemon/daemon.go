// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon/blocknode"
	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/internal/daemon/core"
	"github.com/hashgraph/solo-weaver/pkg/models"
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
	monitors []core.MonitorRunner
	probe    core.ComponentProbe
	tracker  *core.StatusTracker
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
	paths       models.WeaverPaths
	cfg         DaemonConfig
	server      *Server
	components  []component
	// probeErrors holds the last probe result per component name.
	// nil = probe passed (or no probe). Written by runComponentProbes,
	// read by statusSnapshot — both via atomic.Pointer to avoid locks.
	probeErrors atomic.Pointer[map[string]string]
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
	var componentHandlers []core.ComponentHandler

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
				probe:    core.BuildComponentProbe("consensus-node", result.Monitors),
				tracker:  core.NewStatusTracker(),
			}
			components = append(components, comp)

			if result.MigrationMonitor != nil {
				// migrationStateFn captures comp.tracker by reference — safe because
				// comp is appended to the slice and never moved after this point.
				migrationStateFn := func() core.MonitorState {
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
			components = append(components, component{
				name:     "block-node",
				monitors: result.Monitors,
				probe:    nil,
				tracker:  core.NewStatusTracker(),
			})
		}
	}

	d := &Daemon{
		paths:      paths,
		cfg:        cfg,
		components: components,
	}
	d.server = NewServer(paths.DaemonSockPath, ServerOptions{
		StatusFn:          d.statusSnapshot,
		ComponentHandlers: componentHandlers,
	}, ServerConfig{})
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
				core.SupervisedMonitor(ctx, m, tracker)
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
	componentProbes := make([]core.ComponentProbe, 0, len(d.components))
	for _, comp := range d.components {
		if comp.probe != nil {
			componentProbes = append(componentProbes, comp.probe)
		}
	}
	if len(componentProbes) == 0 {
		return
	}

	for {
		errs := make(map[string]string, len(componentProbes))
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
					errs[p.ComponentName()] = err.Error()
					logx.As().Warn().Err(err).
						Str("reason", "ComponentProbeNotReady").
						Str("component", p.ComponentName()).
						Msg("Component prerequisites not yet satisfied — run: solo-provisioner daemon service check")
				}
			}()
		}
		wg.Wait()

		if len(errs) == 0 {
			d.probeErrors.Store(nil)
			logx.As().Info().Str("reason", "AllComponentProbesReady").
				Msg("All component prerequisites satisfied")
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
			Monitors: make(map[string]core.MonitorState),
		}
		if comp.tracker != nil {
			for name, state := range comp.tracker.Snapshot() {
				cs.Monitors[name] = state
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
	defer func() { _ = sdNotify(sdStopping) }()

	defer func() {
		if r := recover(); r != nil {
			logx.As().Error().
				Str("reason", "DaemonPanic").
				Interface("panic", r).
				Msg("Unhandled panic in daemon — exiting for systemd restart")
			_ = sdNotify(sdStopping)
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
			return fmt.Errorf("consensus-node kubeconfig preflight failed — daemon cannot start: %w", err)
		}
	}
	if bn := d.cfg.Components.BlockNode; bn != nil && bn.Enabled {
		if _, err := clientcmd.BuildConfigFromFlags("", bn.Kubeconfig); err != nil {
			return fmt.Errorf("block-node kubeconfig preflight failed — daemon cannot start: %w", err)
		}
	}

	// READY=1 means the daemon process is up and its socket is serving.
	// Component prerequisite health is tracked separately by runComponentProbes
	// and surfaced via GET /status — operators use `daemon service check` to
	// inspect it. This keeps systemd's start timeout from firing on environments
	// where disk prerequisites (upgrade dir, permissions) are not yet in place.
	if err := sdNotify(sdReady); err != nil {
		logx.As().Warn().Err(err).Str("reason", "SdNotifyReadyFailed").Msg("Failed to send READY=1 to systemd")
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return d.server.Start(ctx) })
	eg.Go(func() error { return d.componentSupervisor(ctx) })

	go d.runComponentProbes(ctx)

	return eg.Wait()
}
