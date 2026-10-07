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
// pre-split CLI would have, independent of the per-component flush this
// migration exists to replace.
func writeLegacyStateFixture(t *testing.T, path string, s State) {
	t.Helper()
	b, err := yaml.Marshal(s)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, b, 0o644))
}

func TestPerComponentStateMigration_AppliesWheneverTheLegacyFileExists(t *testing.T) {
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

// TestPerComponentStateMigration_AppliesAfterACrashBetweenLastWriteAndRename
// covers a crash strictly between Execute's last component-file write and its
// rename of the legacy file: every component file is already correct, but
// state.yaml is still present. Applies() must stay true (checking "any
// component missing" instead would return false here, since nothing is
// missing), and a retried Execute must succeed and finally rename the file.
func TestPerComponentStateMigration_AppliesAfterACrashBetweenLastWriteAndRename(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	writeLegacyStateFixture(t, legacyPath, legacyStateFixture(t, dir))
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}

	// Simulate Execute having completed every component write but crashed
	// before the rename: write all five component files directly, leave
	// state.yaml in place.
	for _, id := range AllComponentIDs {
		require.NoError(t, os.WriteFile(componentFilePath(dir, id), []byte("state:\n"), 0o644))
	}

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.True(t, applies, "state.yaml is still present, so the rename must still be retried")

	require.NoError(t, m.Execute(context.Background(), nil))

	_, err = os.Stat(legacyPath)
	require.True(t, os.IsNotExist(err), "the retried Execute must finish the rename this time")
	applies, err = m.Applies(nil)
	require.NoError(t, err)
	require.False(t, applies, "fully done now: the legacy file is gone")
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

// TestPerComponentStateMigration_RollbackRestoresTheExactBackup verifies
// Rollback restores Execute's .pre-v1231 backup verbatim rather than
// recomposing from the current component files — in particular it must not
// stamp provisioner.version with whatever binary happens to be running
// Rollback, and it must not pick up changes made to a component file after
// Execute ran.
func TestPerComponentStateMigration_RollbackRestoresTheExactBackup(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	fixture := legacyStateFixture(t, dir)
	fixture.ProvisionerState.Version = "1.2.3-pre-migration"
	writeLegacyStateFixture(t, legacyPath, fixture)
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}
	require.NoError(t, m.Execute(context.Background(), nil))

	// Simulate real post-migration usage: a command changes blocknode.yaml
	// after the split. Rollback must not pick this up — it restores the
	// pre-migration snapshot, not current reality.
	sm, err := NewStateManager(WithStateFile(legacyPath))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	changed := sm.State()
	changed.BlockNodeState.ReleaseInfo.Name = "changed-after-migration"
	require.NoError(t, sm.Set(changed).FlushScoped(ComponentBlockNode))

	require.NoError(t, m.Rollback(context.Background(), nil))

	_, err = os.Stat(legacyPath)
	require.NoError(t, err, "rollback should recreate state.yaml")
	for _, id := range AllComponentIDs {
		_, err := os.Stat(componentFilePath(dir, id))
		require.True(t, os.IsNotExist(err), "%s state file should be removed by rollback", id)
	}
	_, err = os.Stat(legacyPath + legacyStateBackupSuffix)
	require.True(t, os.IsNotExist(err), "the backup should be consumed by a successful rollback")

	b, err := os.ReadFile(legacyPath)
	require.NoError(t, err)
	var restored State
	require.NoError(t, yaml.Unmarshal(b, &restored))
	require.Equal(t, "1.2.3-pre-migration", restored.ProvisionerState.Version,
		"must restore the exact pre-migration version, not stamp the current binary's")
	require.Equal(t, "block-node", restored.BlockNodeState.ReleaseInfo.Name,
		"must restore the pre-migration snapshot, not a post-migration change")
}

// TestPerComponentStateMigration_RollbackWithoutABackupRecomposesCurrentFiles
// covers the fallback path: no .pre-v1231 backup exists (Execute's rename
// failed and fell back to removing the legacy file, or there simply never was
// one), so Rollback does the best it can from whatever the component files
// currently hold.
func TestPerComponentStateMigration_RollbackWithoutABackupRecomposesCurrentFiles(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	writeLegacyStateFixture(t, legacyPath, legacyStateFixture(t, dir))
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}
	require.NoError(t, m.Execute(context.Background(), nil))
	require.NoError(t, os.Remove(legacyPath+legacyStateBackupSuffix), "simulate no backup surviving")

	require.NoError(t, m.Rollback(context.Background(), nil))

	b, err := os.ReadFile(legacyPath)
	require.NoError(t, err)
	var recomposed State
	require.NoError(t, yaml.Unmarshal(b, &recomposed))
	require.Equal(t, "mainnet", recomposed.MachineState.Profile)
	require.Equal(t, "block-node", recomposed.BlockNodeState.ReleaseInfo.Name)
	require.Contains(t, recomposed.ConsensusNodes, "node1")
	require.True(t, recomposed.TeleportState.NodeAgent.Configured)
}

// TestPerComponentStateMigration_RenameFailureRemovesLegacyFileInstead covers
// a failed rename of state.yaml to its backup name: leaving state.yaml in
// place would make Applies() true again the moment any component file is
// later lost for an unrelated reason, triggering a destructive re-split from
// stale content. Execute must remove the legacy file instead.
func TestPerComponentStateMigration_RenameFailureRemovesLegacyFileInstead(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	writeLegacyStateFixture(t, legacyPath, legacyStateFixture(t, dir))
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}

	// Force the rename to fail: put a non-empty directory where the backup
	// would go, so os.Rename(legacyPath, backupPath) errors instead of
	// replacing it.
	backupPath := legacyPath + legacyStateBackupSuffix
	require.NoError(t, os.MkdirAll(backupPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(backupPath, "occupied"), []byte("x"), 0o644))

	require.NoError(t, m.Execute(context.Background(), nil))

	_, err := os.Stat(legacyPath)
	require.True(t, os.IsNotExist(err), "the legacy file must be removed, not left behind, when the rename fails")

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.False(t, applies, "with the legacy file gone, a later missing component file must not re-trigger the migration")
}
