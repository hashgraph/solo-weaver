// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"

	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"golang.org/x/sync/errgroup"
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

func New(paths models.WeaverPaths) *Daemon {
	mm := consensus.NewMigrationMonitor()
	d := &Daemon{
		paths:            paths,
		upgradeMonitor:   consensus.NewUpgradeMonitor(),
		migrationMonitor: mm,
	}
	d.server = NewServer(paths.DaemonSockPath, mm, ServerConfig{})
	return d
}

// NewWithComponents constructs a Daemon from pre-built sub-systems. Intended
// for tests that need to inject a custom Server (e.g. with a short timeout).
func NewWithComponents(paths models.WeaverPaths, srv *Server, mm *consensus.MigrationMonitor) *Daemon {
	return &Daemon{
		paths:            paths,
		server:           srv,
		upgradeMonitor:   consensus.NewUpgradeMonitor(),
		migrationMonitor: mm,
	}
}

// Run starts all sub-systems and blocks until ctx is cancelled or a critical
// sub-system exits with an error. It is the single entry point called from
// cmd/daemon/main.go.
func (d *Daemon) Run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return d.server.Start(ctx) })
	eg.Go(func() error { return d.upgradeMonitor.Run(ctx) })
	eg.Go(func() error { return d.migrationMonitor.Run(ctx) })
	return eg.Wait()
}
