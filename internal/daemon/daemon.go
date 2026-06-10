// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/pkg/eventlog"
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
	monitors []MonitorRunner
	probe    ComponentProbe
	tracker  *StatusTracker
}

// Daemon is the controller for solo-provisioner-daemon. It composes the
// sub-systems and owns their lifecycle via Run.
//
// Goroutine map:
//   - Socket server        — always on; HTTP control plane on daemon.sock
//   - componentSupervisor  — one supervised goroutine per enabled monitor;
//     crashes are absorbed per-monitor with exponential back-off (#662/#663)
type Daemon struct {
	paths         models.WeaverPaths
	cfg           DaemonConfig
	server        *Server
	components    []component
	migrateLogger *eventlog.EventLogger
}

// New constructs a Daemon from WeaverPaths. It reads daemon.yaml from
// paths.DaemonConfigPath and fails fast if the config is missing or invalid —
// the daemon must not start without a valid node_id, kubeconfig, and orbit.
// Use NewFromConfig when the caller has already resolved the config (e.g. with
// CLI flag overrides applied).
func New(paths models.WeaverPaths) (*Daemon, error) {
	cfg, err := LoadDaemonConfig(paths.DaemonConfigPath)
	if err != nil {
		return nil, err
	}
	return NewFromConfig(paths, cfg)
}

// NewFromConfig constructs a Daemon from a pre-resolved DaemonConfig. The
// caller is responsible for loading and validating cfg (e.g. via LoadDaemonConfig
// followed by applying CLI flag overrides). cfg must pass Validate() before
// this function is called.
//
// Components are skipped when their Enabled flag is false. Individual monitors
// within a component are skipped when their toggle is false. The resulting
// Daemon.components slice contains only the monitors that will actually run.
func NewFromConfig(paths models.WeaverPaths, cfg DaemonConfig) (*Daemon, error) {
	var components []component
	var migrateLogger *eventlog.EventLogger

	cn := cfg.Components.ConsensusNode
	if cn != nil && cn.Enabled {
		var cnMonitors []MonitorRunner

		if cn.Monitors.Upgrade {
			um, err := consensus.NewUpgradeMonitor(consensus.UpgradeMonitorConfig{
				NodeID:           cn.NodeID,
				KubeconfigPath:   cn.Kubeconfig,
				Namespace:        cn.Orbit,
				UpgradeEventsDir: paths.DaemonConsensusUpgradeEventsDir,
				HomeDir:          paths.HomeDir,
				UpgradeDir:       cn.EffectiveUpgradeDir(),
			})
			if err != nil {
				return nil, err
			}
			cnMonitors = append(cnMonitors, um)
		}

		if cn.Monitors.Migration {
			if ml, err := eventlog.NewAppend(paths.DaemonConsensusMigrateEventsDir, "consensus-migrate-events.jsonl"); err != nil {
				logx.As().Warn().Err(err).
					Str("reason", "MigrateLoggerInitFailed").
					Str("dir", paths.DaemonConsensusMigrateEventsDir).
					Msg("Failed to open migrate event logger — migration events will not be persisted")
			} else {
				migrateLogger = ml
			}

			mm := consensus.NewMigrationMonitorWith(
				cn.NodeID,
				migrateLogger,
				&consensus.NoopDecommissioner{},
				consensus.MigrationMonitorConfig{},
				paths.DaemonConsensusMigrateEventsDir,
			).WithCriteria(
				consensus.SoakDuration{}, // zero Period → defaults to DefaultSoakPeriod (48h)
				consensus.UploaderBacklogCleared{},
				&consensus.NoPodRestarts{
					KubeconfigPath: cn.Kubeconfig,
					Namespace:      cn.Orbit,
					PodLabelSelector: fmt.Sprintf(
						"operator.solo.hedera.com/orbit=%s,operator.solo.hedera.com/node-id=%s",
						cn.Orbit, cn.NodeID,
					),
				},
				consensus.ConsensusParticipationNominal{},
			)
			cnMonitors = append(cnMonitors, mm)
		}

		if len(cnMonitors) > 0 {
			components = append(components, component{
				name:     "consensus-node",
				monitors: cnMonitors,
				probe:    buildComponentProbe("consensus-node", cnMonitors),
				tracker:  NewStatusTracker(),
			})
		}
	}

	bn := cfg.Components.BlockNode
	if bn != nil && bn.Enabled {
		var bnMonitors []MonitorRunner
		if bn.Monitors.Upgrade {
			bnMonitors = append(bnMonitors, &blockNodeUpgradeMonitor{})
		}
		if len(bnMonitors) > 0 {
			// Stub monitors have no external probe — nil probe means immediately ready.
			components = append(components, component{
				name:     "block-node",
				monitors: bnMonitors,
				probe:    nil,
				tracker:  NewStatusTracker(),
			})
		}
	}

	d := &Daemon{
		paths:         paths,
		cfg:           cfg,
		components:    components,
		migrateLogger: migrateLogger,
	}

	// Build the server. Find the migration monitor (for soak endpoints) and
	// provide a statusFn closure (for GET /status).
	var mm *consensus.MigrationMonitor
	for _, comp := range d.components {
		for _, mon := range comp.monitors {
			if m, ok := mon.(*consensus.MigrationMonitor); ok {
				mm = m
				break
			}
		}
	}
	d.server = NewServer(paths.DaemonSockPath, mm, d.statusSnapshot, ServerConfig{})
	return d, nil
}

// componentSupervisor starts one supervised goroutine per monitor in every
// enabled component. It returns nil only after all monitors have stopped (which
// happens when ctx is cancelled or a monitor exits cleanly). It never returns a
// non-nil error so that a crashing monitor does not cancel the top-level
// errgroup and take down server.Start or the daemon process.
func (d *Daemon) componentSupervisor(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, comp := range d.components {
		tracker := comp.tracker // nil-safe: supervisedMonitor handles nil tracker
		for _, m := range comp.monitors {
			wg.Add(1)
			m := m // pin loop variable for goroutine
			go func() {
				defer wg.Done()
				supervisedMonitor(ctx, m, tracker)
			}()
		}
	}
	wg.Wait()
	logx.As().Info().
		Str("reason", "ComponentSupervisorStopped").
		Msg("All component monitors stopped")
	return nil
}

// runCompositeProbe runs all component probes concurrently. It fires READY=1
// only when every probe returns nil. If any probe fails (ctx cancelled) READY=1
// is not sent; systemd's TimeoutStartSec will mark the service failed.
func (d *Daemon) runCompositeProbe(ctx context.Context) {
	var wg sync.WaitGroup
	results := make(chan error, len(d.components))

	for _, comp := range d.components {
		if comp.probe == nil {
			continue // nothing to probe
		}
		wg.Add(1)
		p := comp.probe
		go func() {
			defer wg.Done()
			if err := p.Probe(ctx); err != nil {
				logx.As().Error().Err(err).
					Str("reason", "ComponentProbeAborted").
					Str("component", p.ComponentName()).
					Msg("Component probe aborted — not sending READY=1")
				results <- err
			}
		}()
	}

	wg.Wait()
	close(results)

	for err := range results {
		if err != nil {
			return // at least one probe failed; READY=1 not sent
		}
	}

	if err := sdNotify(sdReady); err != nil {
		logx.As().Warn().Err(err).Str("reason", "SdNotifyReadyFailed").Msg("Failed to send READY=1 to systemd")
	}
}

// statusSnapshot builds a StatusResponse from the current tracker snapshots.
// It is passed to the Server as a closure for the GET /status handler.
func (d *Daemon) statusSnapshot() StatusResponse {
	resp := StatusResponse{
		Components: make(map[string]ComponentStatus, len(d.components)),
	}
	for _, comp := range d.components {
		cs := ComponentStatus{
			Monitors: make(map[string]MonitorState),
		}
		if comp.tracker != nil {
			for name, state := range comp.tracker.Snapshot() {
				cs.Monitors[name] = state
			}
		}
		resp.Components[comp.name] = cs
	}
	return resp
}

// buildComponentProbe collects RequiredProbe() from every ProbableMonitor in
// monitors and wraps them in a CompositeProbe named componentName. Returns nil
// when no monitor declares a prerequisite (host-only component); the supervisor
// treats a nil probe as immediately ready.
func buildComponentProbe(componentName string, monitors []MonitorRunner) ComponentProbe {
	var leafProbes []Probe
	for _, m := range monitors {
		if pm, ok := m.(ProbableMonitor); ok {
			leafProbes = append(leafProbes, pm.RequiredProbe())
		}
	}
	if len(leafProbes) == 0 {
		return nil
	}
	return NewCompositeProbe(componentName, leafProbes...)
}

// Run starts all sub-systems and blocks until ctx is cancelled or a critical
// sub-system exits with an error. It is the single entry point called from
// cmd/daemon/main.go.
//
// Infrastructure goroutines in the top-level errgroup:
//   - server.Start  — HTTP control plane; a failure here is fatal (daemon restarts)
//   - componentSupervisor — absorbs all monitor crashes; never returns non-nil
//
// A top-level recover logs any unhandled panic with a structured message before
// calling os.Exit(2), ensuring the reason is captured in the daemon log before
// systemd restarts the process. Sub-system panics are caught earlier (runWatch,
// handleExecute) and converted to errors so this path is a last resort only.
func (d *Daemon) Run(ctx context.Context) error {
	// Signal systemd that we are stopping on any return path (ctx cancel or error).
	defer func() { _ = sdNotify(sdStopping) }()

	defer func() {
		if r := recover(); r != nil {
			logx.As().Error().
				Str("reason", "DaemonPanic").
				Interface("panic", r).
				Msg("Unhandled panic in daemon — exiting for systemd restart")
			// sdStopping must be sent explicitly here because os.Exit(2) bypasses
			// all pending defers. The sdNotify(sdStopping) defer was registered
			// first, so in LIFO order it would run last — after this panic recovery
			// returns. But os.Exit never returns, so that defer is never reached.
			_ = sdNotify(sdStopping)
			os.Exit(2)
		}
	}()
	if d.migrateLogger != nil {
		defer func() {
			if err := d.migrateLogger.Close(); err != nil {
				logx.As().Warn().Err(err).Str("reason", "MigrateLoggerCloseFailed").Msg("Failed to close migrate event logger")
			}
		}()
	}

	// Preflight: each enabled component's kubeconfig must exist and be parseable
	// before we start anything. A missing or invalid file is a configuration error
	// that will never self-heal, so we fail fast here rather than burning systemd's
	// TimeoutStartSec on pointless retries.
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

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return d.server.Start(ctx) })
	eg.Go(func() error { return d.componentSupervisor(ctx) })

	// Run all component probes concurrently. The server is already accepting
	// requests; READY=1 fires only when every probe passes. Each probe retries
	// internally until success or ctx cancellation (systemd TimeoutStartSec).
	go d.runCompositeProbe(ctx)

	return eg.Wait()
}
