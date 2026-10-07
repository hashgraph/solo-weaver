// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func legacyStateFixture(t *testing.T, dir string) State {
	t.Helper()
	s := NewState(filepath.Join(dir, StateFileName))
	s.Version = "v2"
	s.MachineState.Profile = "mainnet"
	s.MachineState.Software["crio"] = SoftwareState{Name: "cri-o", Version: "1.30.0", Installed: true}
	s.BlockNodeState.ReleaseInfo.Name = "block-node"
	s.ConsensusNodes = map[string]ConsensusNodeState{"node1": {NodeId: 1}}
	s.TeleportState.NodeAgent = TeleportNodeAgentState{Installed: true, Configured: true}
	return s
}

// writeLegacyStateFixture writes s as a single state.yaml the way a
// pre-#1231 CLI would have, independent of the per-component flush this
// migration exists to replace.
func writeLegacyStateFixture(t *testing.T, path string, s State) {
	t.Helper()
	b, err := yaml.Marshal(s)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, b, 0o644))
}

func TestPerComponentStateMigration_AppliesOnlyWithLegacyFileAndMissingComponents(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.False(t, applies, "no legacy file yet: nothing to migrate")

	writeLegacyStateFixture(t, legacyPath, legacyStateFixture(t, dir))
	applies, err = m.Applies(nil)
	require.NoError(t, err)
	require.True(t, applies, "legacy file exists and no component files split out yet")
}

func TestPerComponentStateMigration_ExecuteSplitsAllComponentsAndBacksUpLegacy(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	writeLegacyStateFixture(t, legacyPath, legacyStateFixture(t, dir))
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}

	require.NoError(t, m.Execute(context.Background(), nil))

	for _, id := range AllComponentIDs {
		_, err := os.Stat(componentFilePath(dir, id))
		require.NoErrorf(t, err, "%s state file should exist after migration", id)
	}

	_, err := os.Stat(legacyPath)
	require.True(t, os.IsNotExist(err), "legacy state.yaml should be renamed out of the way")
	_, err = os.Stat(legacyPath + legacyStateBackupSuffix)
	require.NoError(t, err, "legacy state.yaml should survive as a .pre-v1231 backup")

	// The split content round-trips: read it back through a real manager and
	// confirm the owning sections landed in the right files.
	sm, err := NewStateManager(WithStateFile(legacyPath))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	got := sm.State()
	require.Equal(t, "mainnet", got.MachineState.Profile)
	require.Equal(t, "block-node", got.BlockNodeState.ReleaseInfo.Name)
	require.Contains(t, got.ConsensusNodes, "node1")
	require.True(t, got.TeleportState.NodeAgent.Configured)

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.False(t, applies, "re-running after a successful migration is a no-op")
}

func TestPerComponentStateMigration_ExecuteIsIdempotentAfterASimulatedCrash(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	writeLegacyStateFixture(t, legacyPath, legacyStateFixture(t, dir))
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}

	// Simulate a crash partway through a prior Execute: one component file
	// already exists (from the "crashed" run), but the legacy file was never
	// renamed because Execute never got that far.
	require.NoError(t, os.WriteFile(componentFilePath(dir, ComponentTeleport), []byte("stale: true\n"), 0o644))

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.True(t, applies, "legacy file still present: the crashed run must be retried")

	require.NoError(t, m.Execute(context.Background(), nil))

	for _, id := range AllComponentIDs {
		_, err := os.Stat(componentFilePath(dir, id))
		require.NoErrorf(t, err, "%s state file should exist after retrying the migration", id)
	}
	_, err = os.Stat(legacyPath)
	require.True(t, os.IsNotExist(err), "legacy state.yaml should be renamed out of the way on the retried run")
}

func TestPerComponentStateMigration_FreshInstallNeverApplies(t *testing.T) {
	dir := t.TempDir()
	m := &PerComponentStateMigration{legacyStateFileOverride: filepath.Join(dir, StateFileName)}

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.False(t, applies, "a host with no legacy state.yaml at all has nothing to split")
}

func TestPerComponentStateMigration_RollbackRecomposesLegacyFileAndRemovesComponents(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	writeLegacyStateFixture(t, legacyPath, legacyStateFixture(t, dir))
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}
	require.NoError(t, m.Execute(context.Background(), nil))

	require.NoError(t, m.Rollback(context.Background(), nil))

	_, err := os.Stat(legacyPath)
	require.NoError(t, err, "rollback should recreate state.yaml")
	for _, id := range AllComponentIDs {
		_, err := os.Stat(componentFilePath(dir, id))
		require.True(t, os.IsNotExist(err), "%s state file should be removed by rollback", id)
	}

	b, err := os.ReadFile(legacyPath)
	require.NoError(t, err)
	var recomposed State
	require.NoError(t, yaml.Unmarshal(b, &recomposed))
	require.Equal(t, "mainnet", recomposed.MachineState.Profile)
	require.Equal(t, "block-node", recomposed.BlockNodeState.ReleaseInfo.Name)
	require.Contains(t, recomposed.ConsensusNodes, "node1")
	require.True(t, recomposed.TeleportState.NodeAgent.Configured)
}
