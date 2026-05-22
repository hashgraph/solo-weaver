// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/pkg/filepruner"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"golang.org/x/sync/errgroup"
)

const (
	upgradeEventLayout = "20060102T150405Z"
	upgradeEventMaxAge = 365 * 24 * time.Hour
	upgradeEventKeep   = 50
	upgradeEventGlob   = "consensus-upgrade-*.jsonl"
)

// Daemon is the controller for solo-provisioner-daemon. It composes the
// sub-systems and owns their lifecycle via Run.
//
// Goroutine map:
//   - Socket server       — always on; HTTP control plane on daemon.sock
//   - UpgradeMonitor      — always on; K8s watch for ReadyForProvisionerDaemon (#519)
//   - MigrationMonitor    — dispatch loop always on; per-activation goroutine on demand (#520)
type Daemon struct {
	paths            models.WeaverPaths
	server           *Server
	upgradeMonitor   *consensus.UpgradeMonitor
	migrationMonitor *consensus.MigrationMonitor
}

// New constructs a Daemon from WeaverPaths. It reads daemon.yaml from
// paths.DaemonConfigPath and fails fast if the config is missing or invalid —
// the daemon must not start without a valid kubeconfig and orbit.
func New(paths models.WeaverPaths) (*Daemon, error) {
	pruneUpgradeEventLogs(paths.HomeDir, paths.DaemonConsensusUpgradeEventsDir)

	cfg, err := LoadDaemonConfig(paths.DaemonConfigPath)
	if err != nil {
		return nil, err
	}

	um, err := consensus.NewUpgradeMonitor(consensus.UpgradeMonitorConfig{
		KubeconfigPath: cfg.Kubeconfig,
		Namespace:      cfg.Orbit,
	})
	if err != nil {
		return nil, err
	}

	mm := consensus.NewMigrationMonitor()
	d := &Daemon{
		paths:            paths,
		upgradeMonitor:   um,
		migrationMonitor: mm,
	}
	d.server = NewServer(paths.DaemonSockPath, mm, ServerConfig{}) // zero value → all defaults
	return d, nil
}

// pruneUpgradeEventLogs applies the retention policy to per-operation upgrade
// JSONL files on daemon startup. A failure is logged as a warning and does not
// block startup — a retained extra file is less harmful than a failed daemon.
// homeDir is used to verify dir is within the weaver home tree, preventing
// accidental pruning of arbitrary filesystem paths.
func pruneUpgradeEventLogs(homeDir, dir string) {
	if dir == "" || !filepath.IsAbs(dir) {
		logx.As().Warn().
			Str("reason", "UpgradeEventLogPruneSkipped").
			Str("dir", dir).
			Msg("Skipping upgrade event log pruning — dir is empty or relative")
		return
	}
	if _, err := sanity.ValidatePathWithinBase(homeDir, dir); err != nil {
		logx.As().Warn().Err(err).
			Str("reason", "UpgradeEventLogPruneSkipped").
			Str("dir", dir).
			Str("home", homeDir).
			Msg("Skipping upgrade event log pruning — dir is outside weaver home")
		return
	}
	p := filepruner.New(filepruner.FilenameTimestampStrategy{
		Layout: upgradeEventLayout,
		MaxAge: upgradeEventMaxAge,
	})
	if err := p.Prune(dir, upgradeEventGlob, upgradeEventKeep); err != nil {
		logx.As().Warn().Err(err).
			Str("reason", "UpgradeEventLogPruneFailed").
			Str("dir", dir).
			Msg("Failed to prune upgrade event logs on startup — continuing")
	}
}

// NewWithComponents constructs a Daemon from pre-built sub-systems. Intended
// for tests that need to inject a custom Server (e.g. with a short timeout).
func NewWithComponents(paths models.WeaverPaths, srv *Server, mm *consensus.MigrationMonitor) *Daemon {
	return &Daemon{
		paths:            paths,
		server:           srv,
		upgradeMonitor:   nil,
		migrationMonitor: mm,
	}
}

// Run starts all sub-systems and blocks until ctx is cancelled or a critical
// sub-system exits with an error. It is the single entry point called from
// cmd/daemon/main.go.
//
// A top-level recover logs any unhandled panic with a structured message before
// calling os.Exit(2), ensuring the reason is captured in the daemon log before
// systemd restarts the process. Sub-system panics are caught earlier (runWatch,
// handleExecute) and converted to errors so this path is a last resort only.
func (d *Daemon) Run(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			logx.As().Error().
				Str("reason", "DaemonPanic").
				Interface("panic", r).
				Msg("Unhandled panic in daemon — exiting for systemd restart")
			os.Exit(2)
		}
	}()

	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return d.server.Start(ctx) })
	if d.upgradeMonitor != nil {
		eg.Go(func() error { return d.upgradeMonitor.Run(ctx) })
	}
	eg.Go(func() error { return d.migrationMonitor.Run(ctx) })
	return eg.Wait()
}
