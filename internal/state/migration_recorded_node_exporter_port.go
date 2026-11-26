// SPDX-License-Identifier: Apache-2.0

// migration_recorded_node_exporter_port.go adds node-exporter's port to the
// block node's recorded host-firewall config (machineState.firewall.inClusterPorts)
// on hosts recorded before the port joined the default in-cluster ports. The
// record re-creates the firewall when no table is live (a re-enable, or an
// upgrade after the table was removed), so without it the port would close
// again. The firewall's own config is migrated by internal/workflows' migration
// of the same version.

package state

import (
	"context"
	"path/filepath"
	"slices"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// recordedNodeExporterPortMinVersion is the CLI release that adds
// node-exporter's port to the default in-cluster ports.
const recordedNodeExporterPortMinVersion = "0.37.0"

// RecordedNodeExporterPortMigration adds node-exporter's port to the recorded
// in-cluster ports.
type RecordedNodeExporterPortMigration struct {
	migration.CLIVersionMigration
	// added is set by Execute, so Rollback undoes only its own change.
	added bool
}

// NewRecordedNodeExporterPortMigration constructs the migration.
func NewRecordedNodeExporterPortMigration() *RecordedNodeExporterPortMigration {
	return &RecordedNodeExporterPortMigration{
		CLIVersionMigration: migration.NewCLIVersionMigration(
			"recorded-node-exporter-port-v"+recordedNodeExporterPortMinVersion,
			"Add node-exporter's port to machineState.firewall.inClusterPorts on hosts recorded "+
				"before it joined the default in-cluster ports",
			recordedNodeExporterPortMinVersion,
		),
	}
}

// stateFiles is every state file Execute or Rollback can write in dir.
func (m *RecordedNodeExporterPortMigration) stateFiles(dir string) []string {
	return []string{componentFilePath(dir, ComponentFirewall)}
}

// Execute appends the port when the verdict is NodeExporterPortAdd. A narrowed
// list is left alone; the host firewall's migration warns the operator.
//
// It never fails the startup pass: that would stop every command, including the
// ones that repair the record. A failure warns instead, and the record keeps its
// old ports.
func (m *RecordedNodeExporterPortMigration) Execute(_ context.Context, _ *migration.Context) error {
	appended := false
	err := updateRecordedFirewall(func(fw *HostFirewallState) (bool, error) {
		verdict, err := firewall.VerdictForNodeExporterPort(firewall.PortStrings(fw.InClusterPorts))
		if err != nil {
			return false, errorx.IllegalFormat.Wrap(err, "invalid recorded in-cluster ports %v", fw.InClusterPorts)
		}
		switch verdict {
		case firewall.NodeExporterPortAdd:
			fw.InClusterPorts = append(slices.Clone(fw.InClusterPorts), models.NodeExporterPort)
			appended = true
			return true, nil
		case firewall.NodeExporterPortNarrowed:
			logx.As().Info().Ints("inClusterPorts", fw.InClusterPorts).
				Msg("recorded in-cluster ports were narrowed from the defaults; not adding node-exporter's port")
		}
		return false, nil
	})
	if err != nil {
		logx.As().Warn().Err(err).Int("port", models.NodeExporterPort).
			Msg("could not add node-exporter's port to the recorded host-firewall config; if the firewall is re-created " +
				"from that record, open the port with `solo-provisioner network firewall add --in-cluster-port 9100`")
		return nil
	}
	m.added = appended
	return nil
}

// Rollback removes the port if Execute added it.
func (m *RecordedNodeExporterPortMigration) Rollback(_ context.Context, _ *migration.Context) error {
	if !m.added {
		return nil
	}
	err := updateRecordedFirewall(func(fw *HostFirewallState) (bool, error) {
		i := slices.Index(fw.InClusterPorts, models.NodeExporterPort)
		if i < 0 {
			return false, nil
		}
		fw.InClusterPorts = slices.Delete(slices.Clone(fw.InClusterPorts), i, i+1)
		return true, nil
	})
	if err == nil {
		m.added = false
	}
	return err
}

// recordedPortLockWait bounds the wait for a command that already holds the
// firewall record, so a startup pass is not held up behind a long reconfigure.
// A var so tests need not wait.
var recordedPortLockWait = 10 * time.Second

// updateRecordedFirewall applies fn to the recorded host-firewall config and
// writes it back when fn reports a change. It holds the firewall record's lock:
// the startup migrations lock does not stop a command already past startup that
// owns it. No record is not an error.
func updateRecordedFirewall(fn func(*HostFirewallState) (bool, error)) error {
	dir := models.Paths().StateDir
	release, err := AcquireComponentLocks(dir, []ComponentID{ComponentFirewall}, recordedPortLockWait)
	if err != nil {
		return err
	}
	defer release()

	sm, err := newStateManager(WithStateFile(filepath.Join(dir, StateFileName)))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager for the recorded node-exporter port migration")
	}
	if err := sm.RefreshScoped(ComponentFirewall); err != nil {
		if errorx.IsOfType(err, NotFoundError) {
			return nil
		}
		return errorx.IllegalState.Wrap(err, "failed to read the recorded host firewall")
	}

	st := sm.State()
	if st.MachineState.Firewall == nil {
		return nil
	}
	fw := *st.MachineState.Firewall
	changed, err := fn(&fw)
	if err != nil || !changed {
		return err
	}
	st.MachineState.Firewall = &fw
	sm.Set(st)
	if err := sm.writeUnrecorded(ComponentFirewall); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write the recorded host firewall")
	}
	return nil
}
