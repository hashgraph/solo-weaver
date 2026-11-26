// SPDX-License-Identifier: Apache-2.0

// migration_in_cluster_node_exporter_port.go opens node-exporter's port to the
// pod CIDR in the host firewall of hosts set up before that port joined the
// default in-cluster ports. Those hosts wrote the port list out in full, so a
// new default never reaches them. The block node's recorded firewall config is
// migrated by internal/state's migration of the same version.
//
// Registered in cmd/cli/commands/root.go RegisterMigrations() under
// migration.ScopeStartup.

package workflows

import (
	"context"
	"os"
	"strconv"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// inClusterNodeExporterPortMinVersion is the CLI release that adds
// node-exporter's port to the default in-cluster ports.
const inClusterNodeExporterPortMinVersion = "0.37.0"

// Seams so tests can script privilege and the firewall.
var (
	inClusterPortGeteuid         = os.Geteuid
	inClusterPortFirewallManager = func() *firewall.Manager { return firewall.NewManager() }
)

// InClusterNodeExporterPortMigration adds node-exporter's port to the host
// firewall's in-cluster ports.
type InClusterNodeExporterPortMigration struct {
	migration.CLIVersionMigration
}

// NewInClusterNodeExporterPortMigration constructs the migration.
func NewInClusterNodeExporterPortMigration() *InClusterNodeExporterPortMigration {
	return &InClusterNodeExporterPortMigration{
		CLIVersionMigration: migration.NewCLIVersionMigration(
			"in-cluster-node-exporter-port-v"+inClusterNodeExporterPortMinVersion,
			"Open node-exporter's port to the pod CIDR in the host firewall of hosts set up "+
				"before it joined the default in-cluster ports",
			inClusterNodeExporterPortMinVersion,
		),
	}
}

// Applies adds a root check to the version boundary: the firewall config and
// the apply lock are root-only, so an unprivileged caller could only fail.
func (m *InClusterNodeExporterPortMigration) Applies(mctx *migration.Context) (bool, error) {
	if euid := inClusterPortGeteuid(); euid != 0 {
		logx.As().Debug().Int("euid", euid).
			Msg("in-cluster node-exporter port migration: not root; leaving it to a privileged invocation")
		return false, nil
	}
	return m.CLIVersionMigration.Applies(mctx)
}

// Execute adds the port when the verdict is NodeExporterPortAdd. Failures only
// warn and name the manual command, so a host with a broken firewall config
// still runs every other command.
func (m *InClusterNodeExporterPortMigration) Execute(ctx context.Context, _ *migration.Context) error {
	port := strconv.Itoa(models.NodeExporterPort)
	manual := "to open it, run `solo-provisioner network firewall add --in-cluster-port " + port + "`"
	l := logx.As()

	mgr := inClusterPortFirewallManager()
	configured, err := mgr.Configured()
	if err != nil {
		l.Warn().Err(err).Msg("in-cluster node-exporter port migration: could not check for a host firewall; skipping — " + manual)
		return nil
	}
	if !configured {
		l.Debug().Msg("in-cluster node-exporter port migration: no host firewall on this host")
		return nil
	}
	t, err := mgr.Table(ctx)
	if err != nil {
		l.Warn().Err(err).Msg("in-cluster node-exporter port migration: could not load the host firewall; skipping — " + manual)
		return nil
	}
	verdict, err := firewall.VerdictForNodeExporterPort(t.InCluster.Ports)
	if err != nil {
		l.Warn().Err(err).Msg("in-cluster node-exporter port migration: the host firewall's in-cluster ports do not parse; skipping — " + manual)
		return nil
	}

	switch verdict {
	case firewall.NodeExporterPortAdd:
	case firewall.NodeExporterPortNarrowed:
		l.Warn().Strs("inClusterPorts", t.InCluster.Ports).
			Msg("in-cluster node-exporter port migration: the host firewall's in-cluster ports were narrowed from the " +
				"defaults, so node-exporter's port was not added and Alloy cannot scrape host metrics — " + manual)
		return nil
	default:
		l.Debug().Strs("inClusterPorts", t.InCluster.Ports).Msg("in-cluster node-exporter port migration: nothing to add")
		return nil
	}

	if err := mgr.Add(ctx, firewall.RuleInCluster, nil, []string{port}); err != nil {
		l.Warn().Err(err).Msg("in-cluster node-exporter port migration: could not open node-exporter's port to the pod CIDR; " +
			"Alloy cannot scrape host metrics while the host firewall is on — " + manual)
		return nil
	}
	l.Info().Str("port", port).Msg("Opened node-exporter's port to the pod CIDR in the host firewall")
	return nil
}

// Rollback is a no-op: the port is part of the default posture, and the next
// startup pass finds it present.
func (m *InClusterNodeExporterPortMigration) Rollback(context.Context, *migration.Context) error {
	logx.As().Warn().Msg("Rollback for the in-cluster node-exporter port migration is not supported")
	return nil
}
