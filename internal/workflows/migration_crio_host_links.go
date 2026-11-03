// SPDX-License-Identifier: Apache-2.0

// migration_crio_host_links.go adds crio and crictl to PATH on already-provisioned
// clusters (issue #1026).
//
// crio Configure() now symlinks crio and crictl into /usr/local/bin and links the
// crictl config to /etc/crictl.yaml, but the configure-crio step skips Configure()
// when IsConfigured() is true. Clusters installed earlier never get the links, so
// this migration creates them on the first run after crossing the version boundary.
//
// Registered in cmd/cli/commands/root.go RegisterMigrations() under
// migration.ScopeStartup.

package workflows

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
)

// crioHostLinksMinVersion is the CLI release that ships the crio/crictl host links
// together with this migration.
const crioHostLinksMinVersion = "0.36.1"

// Seams so tests can stub the host-facing operations.
var (
	crioHostLinksPresent = software.CrioHostLinksPresent
	linkCrioToHost       = software.LinkCrioToHost
)

// CrioHostLinksMigration links crio, crictl and the crictl config into the host on
// existing clusters.
type CrioHostLinksMigration struct {
	migration.CLIVersionMigration
}

// NewCrioHostLinksMigration constructs the migration.
func NewCrioHostLinksMigration() *CrioHostLinksMigration {
	return &CrioHostLinksMigration{
		CLIVersionMigration: migration.NewCLIVersionMigration(
			"crio-host-links-v"+crioHostLinksMinVersion,
			"Add crio and crictl to PATH and link the crictl config on existing clusters",
			crioHostLinksMinVersion,
		),
	}
}

// Execute creates the missing links. It no-ops when cri-o is not installed or the
// links already exist; probe failures are Debug-logged and skipped, like the other
// startup migrations.
func (m *CrioHostLinksMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	installed, err := crioInstalled()
	if err != nil {
		logx.As().Debug().Err(err).Msg("Crio host links migration: failed to probe cri-o installation; skipping")
		return nil
	}
	if !installed {
		logx.As().Debug().Msg("Crio host links migration: cri-o not installed on this host; skipping")
		return nil
	}

	present, err := crioHostLinksPresent()
	if err != nil {
		logx.As().Debug().Err(err).Msg("Crio host links migration: failed to probe existing links; skipping")
		return nil
	}
	if present {
		logx.As().Info().Msg("Crio host links migration: nothing to do (links already present)")
		return nil
	}

	logx.As().Info().Msg("Linking crio and crictl into the host on existing cluster (issue #1026)")

	if err := linkCrioToHost(); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to link crio and crictl into the host").
			WithProperty(models.ErrPropertyResolution, []string{
				"Re-run the provisioner; if it persists, check that /opt/solo/weaver/sandbox/usr/local/bin/crio and crictl exist.",
			})
	}
	return nil
}

// Rollback is a no-op: the links are the required posture and are removed by cri-o
// RemoveConfiguration.
func (m *CrioHostLinksMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	logx.As().Warn().Msg("Rollback for crio host links migration is not supported")
	return nil
}
