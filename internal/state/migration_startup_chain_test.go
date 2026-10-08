// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestStartupStateMigrations_ChainKeepsEveryMigrationsChanges runs the state
// startup migrations in their registered order on a pre-split host that has
// both a state.yaml needing a schema fix and a legacy marker file. Each
// migration's change must survive into the per-component files: the marker's
// software entry (unified-state) and the sshPort→mgmtPorts rewrite
// (mgmt-ports-v1), alongside the existing block node data.
func TestStartupStateMigrations_ChainKeepsEveryMigrationsChanges(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(models.SetPaths(home))
	dir := models.Paths().StateDir
	require.NoError(t, os.MkdirAll(dir, 0o755))

	legacyYAML := []byte(`state:
  machineState:
    profile: mainnet
    firewall:
      sshPort: 2222
  blockNodeState:
    name: block-node
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, StateFileName), legacyYAML, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crio.installed"), []byte("installed at version 1.30.0\n"), 0o644))

	mctx := &migration.Context{}
	for _, m := range []migration.Migration{
		NewUnifiedStateMigration(),
		NewHelmReleaseSchemaV2Migration(),
		NewMgmtPortsV1Migration(),
		NewPerComponentStateMigration(),
	} {
		applies, err := m.Applies(mctx)
		require.NoError(t, err, m.ID())
		if applies {
			require.NoError(t, m.Execute(context.Background(), mctx), m.ID())
		}
	}

	readComponent := func(id ComponentID) State {
		b, err := os.ReadFile(componentFilePath(dir, id))
		require.NoError(t, err, "%s state file must exist", id)
		var s State
		require.NoError(t, yaml.Unmarshal(b, &s))
		return s
	}

	machine := readComponent(ComponentMachine)
	require.True(t, machine.MachineState.Software["crio"].Installed, "the marker file's software entry must survive the split")
	require.NotNil(t, machine.MachineState.Firewall)
	require.Equal(t, []int{2222}, machine.MachineState.Firewall.MgmtPorts, "the sshPort→mgmtPorts rewrite must survive the split")
	require.Equal(t, "mainnet", machine.MachineState.Profile)
	require.Equal(t, "block-node", readComponent(ComponentBlockNode).BlockNodeState.ReleaseInfo.Name)

	_, err := os.Stat(filepath.Join(dir, StateFileName))
	require.True(t, os.IsNotExist(err), "state.yaml must be split and moved out of the way")
	diverged, err := filepath.Glob(filepath.Join(dir, StateFileName+".diverged-*"))
	require.NoError(t, err)
	require.Empty(t, diverged, "a normal upgrade must not be treated as a diverged state.yaml")
}
