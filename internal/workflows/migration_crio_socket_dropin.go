// SPDX-License-Identifier: Apache-2.0

// migration_crio_socket_dropin.go installs the cri-o cAdvisor socket-bridge
// systemd drop-in on already-provisioned clusters.
//
// PR #910 (#22) writes a crio.service drop-in that symlinks the sandbox cri-o
// socket to the default /var/run/crio/crio.sock path so kubelet's eviction
// manager can register the "crio-images" imagefs label. That drop-in is written
// only by crio Configure(), which the configure-crio step skips when
// IsConfigured() (a persisted-state read) is true. Existing clusters installed
// before #910 therefore never get it and keep logging `non-existent label
// "crio-images"` on every sync.
//
// This migration closes that gap: on the first provisioner run after upgrading
// across the 0.25.0 version boundary it writes the drop-in (reusing the same
// template as a fresh install), reloads systemd so the unit override persists
// across restarts/reboots, and creates the default-path socket symlink directly
// so cAdvisor picks it up on its next sync without restarting the running runtime.
//
// Registered in cmd/cli/commands/root.go RegisterMigrations() under
// migration.ScopeStartup.

package workflows

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
)

// crioSocketDropInMinVersion is the CLI release that ships the cAdvisor
// socket-bridge drop-in (PR #910) together with this migration. The migration
// applies when upgrading from a version below this boundary to this version or
// later.
const crioSocketDropInMinVersion = "0.25.0"

// Seams so tests can stub the host-facing operations.
var (
	crioInstalled               = software.CrioInstalled
	crioSocketBridgePresent     = software.CrioSocketBridgePresent
	reconfigureCrioSocketDropIn = software.ReconfigureCrioSocketDropIn
	ensureCrioSocketSymlink     = software.EnsureCrioSocketSymlink
	crioDaemonReload            = soos.DaemonReload
)

// CrioSocketDropInMigration installs the cri-o cAdvisor socket-bridge drop-in on
// already-provisioned clusters so kubelet's eviction manager can register the
// crio-images imagefs label.
type CrioSocketDropInMigration struct {
	migration.CLIVersionMigration
}

// NewCrioSocketDropInMigration constructs the migration.
func NewCrioSocketDropInMigration() *CrioSocketDropInMigration {
	return &CrioSocketDropInMigration{
		CLIVersionMigration: migration.NewCLIVersionMigration(
			"crio-socket-dropin-v"+crioSocketDropInMinVersion,
			"Install the cri-o cAdvisor socket-bridge systemd drop-in on existing clusters so "+
				"kubelet's eviction manager can register the crio-images imagefs label",
			crioSocketDropInMinVersion,
		),
	}
}

// Execute writes the socket-bridge drop-in, reloads systemd, and creates the
// default-path socket symlink.
//
// It is idempotent: it no-ops when cri-o is not installed on the host and when
// the drop-in host symlink already exists. Host-state probe failures are treated
// as "skip" (Debug-logged) rather than hard failures, matching the other startup
// migrations.
func (m *CrioSocketDropInMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	installed, err := crioInstalled()
	if err != nil {
		logx.As().Debug().Err(err).
			Msg("crio socket drop-in migration: failed to probe cri-o installation; skipping")
		return nil
	}
	if !installed {
		logx.As().Debug().Msg("crio socket drop-in migration: cri-o not installed on this host; skipping")
		return nil
	}

	present, err := crioSocketBridgePresent()
	if err != nil {
		logx.As().Debug().Err(err).
			Msg("crio socket drop-in migration: failed to probe drop-in presence; skipping")
		return nil
	}
	if present {
		logx.As().Info().Msg("crio socket drop-in migration: nothing to do (drop-in already present)")
		return nil
	}

	logx.As().Info().Msg("Installing cri-o cAdvisor socket-bridge drop-in on existing cluster (issue #22)")

	if err := reconfigureCrioSocketDropIn(); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write cri-o socket-bridge drop-in").
			WithProperty(models.ErrPropertyResolution, []string{
				"Re-run the provisioner; if it persists, re-run 'kube cluster install' to repair cri-o configuration.",
			})
	}

	// Reload so the drop-in is loaded and survives future restarts/reboots.
	if err := crioDaemonReload(ctx); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to reload systemd after writing the cri-o socket-bridge drop-in").
			WithProperty(models.ErrPropertyResolution, []string{
				"Run: sudo systemctl daemon-reload",
			})
	}

	// Create the default-path symlink now so vendored cAdvisor finds the socket on
	// its next eviction-manager sync, without restarting the running runtime.
	if err := ensureCrioSocketSymlink(); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create the default-path cri-o socket symlink").
			WithProperty(models.ErrPropertyResolution, []string{
				"Verify the sandbox cri-o socket exists and re-run the provisioner.",
			})
	}

	return nil
}

// Rollback is a no-op: re-running a known-good provisioner version re-applies the
// drop-in. We intentionally do not remove it — it is the required posture for the
// crio-images imagefs label.
func (m *CrioSocketDropInMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	logx.As().Warn().Msg("Rollback for crio socket drop-in migration is not supported")
	return nil
}
