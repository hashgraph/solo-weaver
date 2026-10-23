// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func legacyStateFixture(t *testing.T, dir string) State {
	t.Helper()
	s := NewState(filepath.Join(dir, StateFileName))
	s.MachineState.Profile = "mainnet"
	s.MachineState.Software["crio"] = SoftwareState{Name: "cri-o", Version: "1.30.0", Installed: true}
	s.BlockNodeState.ReleaseInfo.Name = "block-node"
	s.ConsensusNodes = map[string]ConsensusNodeState{"node1": {NodeId: 1}}
	s.TeleportState.NodeAgent = TeleportNodeAgentState{Installed: true, Configured: true}
	return s
}

// v034StateYAML is a state.yaml written by v0.34.0's own state manager (commit
// 48194a97, the last release before the per-component split) for a block-node
// host. Only stateFile and executable were changed, from the test run's temp
// paths to a host's. Unlike the fixtures below, it does not come from this
// package's writer.
const v034StateYAML = `hash: 98160b8bf9c0d95cf7423e1d5aee9f85f76be61f24e1f62e442659c692f54a89
hashAlgo: sha256
stateFile: /opt/solo/weaver/state/state.yaml
lastSync: "2026-10-23T20:30:33.359696+03:00"
state:
    version: v2
    provisioner:
        version: 0.34.0
        commit: 48194a97acc53754ae8afc28f883efe9c1ce88ee
        goversion: go1.26.7
        executable: /opt/solo/weaver/bin/solo-provisioner
    machineState:
        profile: local
        software:
            crio:
                name: crio
                version: 1.33.4
                installed: true
                configured: true
            teleport:
                name: teleport
                version: 18.6.4
                installed: false
                configured: false
        hardware:
            memory:
                type: memory
                info: 3 GB available
        firewall:
            disabled: true
    clusterState:
        clusterinfo:
            kubeconfigEnv: ""
            kubeconfigPath: ""
            serverVersion:
                major: ""
                minor: ""
                emulationmajor: ""
                emulationminor: ""
                mincompatibilitymajor: ""
                mincompatibilityminor: ""
                gitversion: ""
                gitcommit: ""
                gittreestate: ""
                builddate: ""
                goversion: ""
                compiler: ""
                platform: ""
            host: ""
            proxy: ""
            clusters: {}
            contexts: {}
            currentContext: ""
            namespace: ""
        created: false
    blockNodeState:
        name: block-node
        version: ""
        appVersion: ""
        namespace: block-node
        chartRef: ""
        chartName: ""
        status: unknown
        storage:
            basePath: ""
            archivePath: ""
            livePath: ""
            logPath: ""
            verificationPath: ""
            pluginsPath: ""
            applicationStatePath: ""
            liveSize: ""
            archiveSize: ""
            logSize: ""
            verificationSize: ""
            pluginsSize: ""
            applicationStateSize: ""
        trafficShapingDisabled: true
    teleportState:
        nodeAgent:
            installed: false
            configured: false
        clusterAgent:
            installed: false
`

// legacyProvisionerFixture is what legacyStateFixture's state.yaml records
// about the CLI that last wrote it.
var legacyProvisionerFixture = ProvisionerRecord{CLI: ProvisionerInfo{
	Version: "0.34.0", Commit: "abc1234", Executable: "/usr/local/bin/solo-provisioner", StateVersion: "v2",
}}

// writeLegacyStateFixture writes s as a single state.yaml the way a
// pre-split CLI would have, independent of the per-component flush this
// migration exists to replace.
func writeLegacyStateFixture(t *testing.T, path string, s State) {
	t.Helper()
	writeLegacyStateFile(t, path, s, legacyProvisionerFixture)
}

// writeLegacyStateFile is writeLegacyStateFixture with the provisioner fields
// to record, which a pre-split state.yaml kept inside its state record.
func writeLegacyStateFile(t *testing.T, path string, s State, rec ProvisionerRecord) {
	t.Helper()
	b, err := yaml.Marshal(s)
	require.NoError(t, err)
	b, err = withLegacyProvisionerFields(b, rec)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, b, 0o644))
}

// readLegacyProvisionerFields reads the provisioner fields of the state.yaml at
// path.
func readLegacyProvisionerFields(t *testing.T, path string) ProvisionerVersionDoc {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc ProvisionerVersionDoc
	require.NoError(t, yaml.Unmarshal(b, &doc))
	return doc
}

// writeComponentFromState writes id's section of s to its component file, the
// way a crashed Execute would have left it before renaming state.yaml.
func writeComponentFromState(t *testing.T, dir string, id ComponentID, s State) {
	t.Helper()
	b, err := yaml.Marshal(projectComponentSection(s, id))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(componentFilePath(dir, id), b, 0o644))
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
	legacy := legacyStateFixture(t, dir)
	writeLegacyStateFixture(t, legacyPath, legacy)
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}

	// Simulate Execute having completed every component write but crashed
	// before the rename: write all five component files from state.yaml's
	// content, leave state.yaml in place.
	for _, id := range AllComponentIDs {
		writeComponentFromState(t, dir, id, legacy)
	}

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.True(t, applies, "state.yaml is still present, so the rename must still be retried")

	require.NoError(t, m.Execute(context.Background(), nil))

	_, err = os.Stat(legacyPath)
	require.True(t, os.IsNotExist(err), "the retried Execute must finish the rename this time")
	_, err = os.Stat(legacyPath + legacyStateBackupSuffix)
	require.NoError(t, err, "matching leftovers are a normal retry: state.yaml goes to the .legacy backup, not aside as diverged")
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
	require.NoError(t, err, "legacy state.yaml should survive as a .legacy backup")

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

	// The recorded version moved to provisioner.yaml, as state.yaml held it:
	// not the running binary's.
	require.Equal(t, legacyProvisionerFixture, readProvisionerRecordFrom(t, dir))

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.False(t, applies, "re-running after a successful migration is a no-op")
}

// TestPerComponentStateMigration_SplitsAReleasedStateFile runs the split over a
// state.yaml a released CLI wrote, so the provisioner fields are read the way
// v0.34.0 laid them out, not the way this package's writer would.
func TestPerComponentStateMigration_SplitsAReleasedStateFile(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	require.NoError(t, os.WriteFile(legacyPath, []byte(v034StateYAML), 0o644))

	require.NoError(t, (&PerComponentStateMigration{legacyStateFileOverride: legacyPath}).Execute(context.Background(), nil))

	require.Equal(t, ProvisionerRecord{CLI: ProvisionerInfo{
		Version:      "0.34.0",
		Commit:       "48194a97acc53754ae8afc28f883efe9c1ce88ee",
		GoVersion:    "go1.26.7",
		Executable:   "/opt/solo/weaver/bin/solo-provisioner",
		StateVersion: "v2",
	}}, readProvisionerRecordFrom(t, dir))

	read := func(id ComponentID) State {
		b, err := os.ReadFile(componentFilePath(dir, id))
		require.NoError(t, err)
		var doc ProvisionerVersionDoc
		require.NoError(t, yaml.Unmarshal(b, &doc))
		require.Emptyf(t, doc.State.Version, "%s.yaml must not carry the state model version", id)
		require.Emptyf(t, doc.State.Provisioner, "%s.yaml must not carry the provisioner", id)
		var s State
		require.NoError(t, yaml.Unmarshal(b, &s))
		return s
	}
	require.Equal(t, "local", read(ComponentProfile).MachineState.Profile)
	require.True(t, read(ComponentFirewall).MachineState.Firewall.Disabled)
	require.True(t, read(ComponentSystem).MachineState.Software["crio"].Installed)
	blockNode := read(ComponentBlockNode).BlockNodeState
	require.Equal(t, "block-node", blockNode.ReleaseInfo.Name)
	require.True(t, blockNode.TrafficShapingDisabled)
}

func TestPerComponentStateMigration_ExecuteIsIdempotentAfterASimulatedCrash(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	legacy := legacyStateFixture(t, dir)
	writeLegacyStateFixture(t, legacyPath, legacy)
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}

	// Simulate a crash partway through a prior Execute: one component file
	// already exists (from the "crashed" run), but the legacy file was never
	// renamed because Execute never got that far.
	writeComponentFromState(t, dir, ComponentTeleport, legacy)

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

// TestPerComponentStateMigration_ExecuteKeepsComponentFilesWhenLegacyDiverges
// covers a state.yaml that reappears after the split with content older than
// the component files (e.g. an operator restored an old backup by hand).
// Execute must not regenerate the component files from it: it keeps them and
// moves state.yaml aside for inspection.
func TestPerComponentStateMigration_ExecuteKeepsComponentFilesWhenLegacyDiverges(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	stale := legacyStateFixture(t, dir)
	writeLegacyStateFixture(t, legacyPath, stale)
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}
	require.NoError(t, m.Execute(context.Background(), nil))

	// A later command updates blocknode.yaml, then the old state.yaml is put back.
	newer := stale
	newer.BlockNodeState.ReleaseInfo.Name = "block-node-upgraded"
	writeComponentFromState(t, dir, ComponentBlockNode, newer)
	writeLegacyStateFixture(t, legacyPath, stale)

	var logs bytes.Buffer
	prevLogger := *logx.As()
	logx.SetLogger(zerolog.New(&logs))
	t.Cleanup(func() { logx.SetLogger(prevLogger) })

	require.NoError(t, m.Execute(context.Background(), &migration.Context{}))
	require.Contains(t, logs.String(), `"divergedComponent":"blocknode"`,
		"the operator must be told state.yaml was moved aside; production passes no Logger in the migration context")

	b, err := os.ReadFile(componentFilePath(dir, ComponentBlockNode))
	require.NoError(t, err)
	var onDisk State
	require.NoError(t, yaml.Unmarshal(b, &onDisk))
	require.Equal(t, "block-node-upgraded", onDisk.BlockNodeState.ReleaseInfo.Name,
		"the newer component file must not be overwritten from the stale state.yaml")

	_, err = os.Stat(legacyPath)
	require.True(t, os.IsNotExist(err), "the diverged state.yaml must be moved out of the way")
	aside, err := filepath.Glob(legacyPath + ".diverged-*")
	require.NoError(t, err)
	require.Len(t, aside, 1, "the diverged state.yaml must be kept for inspection")

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.False(t, applies)
}

func TestPerComponentStateMigration_FreshInstallNeverApplies(t *testing.T) {
	dir := t.TempDir()
	m := &PerComponentStateMigration{legacyStateFileOverride: filepath.Join(dir, StateFileName)}

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.False(t, applies, "a host with no legacy state.yaml at all has nothing to split")
}

// TestPerComponentStateMigration_RollbackRestoresTheExactBackup verifies
// Rollback restores Execute's .legacy backup verbatim rather than
// recomposing from the current component files — in particular it must keep
// the version recorded before the split, and it must not pick up changes made
// to a component file after Execute ran.
func TestPerComponentStateMigration_RollbackRestoresTheExactBackup(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	writeLegacyStateFile(t, legacyPath, legacyStateFixture(t, dir),
		ProvisionerRecord{CLI: ProvisionerInfo{Version: "1.2.3-pre-migration", StateVersion: "v2"}})
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
	require.NoError(t, sm.Set(changed).Commit(ActionHistory{}, ComponentBlockNode))

	require.NoError(t, m.Rollback(context.Background(), nil))

	_, err = os.Stat(legacyPath)
	require.NoError(t, err, "rollback should recreate state.yaml")
	for _, id := range AllComponentIDs {
		_, err := os.Stat(componentFilePath(dir, id))
		require.True(t, os.IsNotExist(err), "%s state file should be removed by rollback", id)
	}
	_, err = os.Stat(legacyPath + legacyStateBackupSuffix)
	require.True(t, os.IsNotExist(err), "the backup should be consumed by a successful rollback")
	_, err = os.Stat(provisionerFilePath(dir))
	require.True(t, os.IsNotExist(err), "provisioner.yaml should be removed by rollback")

	b, err := os.ReadFile(legacyPath)
	require.NoError(t, err)
	var restored State
	require.NoError(t, yaml.Unmarshal(b, &restored))
	require.Equal(t, "1.2.3-pre-migration", readLegacyProvisionerFields(t, legacyPath).State.Provisioner.Version,
		"must restore the exact pre-migration version")
	require.Equal(t, "block-node", restored.BlockNodeState.ReleaseInfo.Name,
		"must restore the pre-migration snapshot, not a post-migration change")
}

// TestPerComponentStateMigration_RollbackAfterAFailedSplitKeepsStateYAML: when
// Execute fails before its rename (here, writing provisioner.yaml), state.yaml
// is untouched and is the only place the recorded version lives. Rollback must
// leave it as it is, not rebuild it from the component files, which do not
// carry the version.
func TestPerComponentStateMigration_RollbackAfterAFailedSplitKeepsStateYAML(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, StateFileName)
	require.NoError(t, os.WriteFile(legacyPath, []byte(v034StateYAML), 0o644))
	// A directory where provisioner.yaml goes makes that write fail.
	require.NoError(t, os.Mkdir(provisionerFilePath(dir), 0o755))
	m := &PerComponentStateMigration{legacyStateFileOverride: legacyPath}

	require.Error(t, m.Execute(context.Background(), nil))
	require.NoError(t, os.Remove(provisionerFilePath(dir)), "a real write failure leaves no directory behind")
	require.NoError(t, m.Rollback(context.Background(), nil))

	b, err := os.ReadFile(legacyPath)
	require.NoError(t, err)
	require.Equal(t, v034StateYAML, string(b), "state.yaml must be left exactly as it was")
}

// TestPerComponentStateMigration_RollbackWithoutABackupRecomposesCurrentFiles
// covers the fallback path: no .legacy backup exists (Execute's rename
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

	// The provisioner fields go back inside the state record, and
	// provisioner.yaml goes away.
	fields := readLegacyProvisionerFields(t, legacyPath)
	require.Equal(t, "v2", fields.State.Version)
	wantProvisioner := legacyProvisionerFixture.CLI
	wantProvisioner.StateVersion = ""
	require.Equal(t, wantProvisioner, fields.State.Provisioner)
	_, err = os.Stat(provisionerFilePath(dir))
	require.True(t, os.IsNotExist(err), "provisioner.yaml should be removed by rollback")
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
	require.Equal(t, legacyProvisionerFixture, readProvisionerRecordFrom(t, dir),
		"the recorded version must reach provisioner.yaml before state.yaml goes away")

	applies, err := m.Applies(nil)
	require.NoError(t, err)
	require.False(t, applies, "with the legacy file gone, a later missing component file must not re-trigger the migration")
}
