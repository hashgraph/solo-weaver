// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/version"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// readProvisionerVersionFrom parses the provisioner version from the machine
// component file inside dir (unlike the reader's fixed models.Paths() path).
func readProvisionerVersionFrom(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(componentFilePath(dir, ComponentMachine))
	require.NoError(t, err)
	var doc ProvisionerVersionDoc
	require.NoError(t, unmarshalStateDoc(data, &doc))
	return doc.State.Provisioner.Version
}

// TestPersistProvisionerVersion_CreatesFileWhenAbsent: with no machine file, the write creates one holding the running binary's version.
func TestPersistProvisionerVersion_CreatesFileWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "state.yaml")
	machineFile := componentFilePath(dir, ComponentMachine)

	_, err := os.Stat(machineFile)
	require.True(t, os.IsNotExist(err), "precondition: machine state file must not exist")

	err = PersistProvisionerVersion(
		WithState(newTestState(tmp)),
		WithFileManager(newTestFileManager(t)),
	)
	require.NoError(t, err)

	_, err = os.Stat(machineFile)
	require.NoError(t, err, "machine state file should now exist")

	assert.Equal(t, version.Get().Version, readProvisionerVersionFrom(t, dir))
}

// TestPersistProvisionerVersion_PreservesExistingState: the write merges — an existing software entry survives; only provisioner.version advances.
func TestPersistProvisionerVersion_PreservesExistingState(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "state.yaml")
	fm := newTestFileManager(t)

	const oldVersion = "0.0.0-pre-existing"
	const swKey = "crio"

	// Seed a rich state file the way a provisioned cluster would have one.
	seed := newTestState(tmp)
	seed.ProvisionerState.Version = oldVersion
	seed.MachineState.Software[swKey] = SoftwareState{Name: "cri-o", Version: "1.30.0", Installed: true}

	seedMgr, err := NewStateManager(WithState(seed), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, seedMgr.Refresh())
	require.NoError(t, seedMgr.FlushState())
	require.NotEqual(t, oldVersion, version.Get().Version, "sentinel must differ from current version")

	// Record the provisioner version against the existing file.
	err = PersistProvisionerVersion(
		WithState(newTestState(tmp)),
		WithFileManager(fm),
	)
	require.NoError(t, err)

	// Version advanced; the pre-existing software entry is untouched.
	assert.Equal(t, version.Get().Version, readProvisionerVersionFrom(t, dir))

	reloaded, err := NewStateManager(WithState(newTestState(tmp)), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, reloaded.Refresh())
	sw, ok := reloaded.State().MachineState.Software[swKey]
	require.True(t, ok, "pre-existing software entry must be preserved")
	assert.Equal(t, "1.30.0", sw.Version)
}

// TestPersistProvisionerVersion_Idempotent: repeated calls succeed (the concurrency baseline resets each call).
func TestPersistProvisionerVersion_Idempotent(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "state.yaml")
	fm := newTestFileManager(t)

	for i := range 3 {
		err := PersistProvisionerVersion(WithState(newTestState(tmp)), WithFileManager(fm))
		require.NoErrorf(t, err, "call %d should succeed", i)
	}
	assert.Equal(t, version.Get().Version, readProvisionerVersionFrom(t, dir))
}

// TestPersistThenReadProvisionerVersion_RoundTrip: writer and reader agree on the state-file path and shape — the round-trip #789 depends on.
func TestPersistThenReadProvisionerVersion_RoundTrip(t *testing.T) {
	home := t.TempDir()
	restore := models.SetPaths(home)
	t.Cleanup(restore)

	machineFile := componentFilePath(models.Paths().StateDir, ComponentMachine)

	// Precondition: state dir exists but no machine state file — the reader returns "".
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))
	_, statErr := os.Stat(machineFile)
	require.True(t, os.IsNotExist(statErr), "precondition: machine state file must not exist yet")

	absent, err := ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Empty(t, absent, "absent machine state file must read back as an empty version")

	// Write the running version at the default path (no opts → the reader's path).
	require.NoError(t, PersistProvisionerVersion())

	_, statErr = os.Stat(machineFile)
	require.NoError(t, statErr, "machine state file should now exist at the reader's path")

	// The reader observes exactly the version just written (non-empty).
	readBack, err := ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.NotEmpty(t, readBack, "recorded version must read back non-empty, unlike the absent case")
	assert.Equal(t, version.Get().Version, readBack,
		"the reader must observe the version the writer persisted")
}

// TestReadProvisionerVersion_FallsBackToLegacyStateFile: on a host that has not
// run the per-component split migration yet, machine.yaml does not exist, but
// the legacy state.yaml already has a real recorded version. RunStartupMigrations
// reads this to decide which migrations apply — including the split migration
// itself — before any migration for this invocation has run, so a reader that
// only looked at machine.yaml would see "" and re-run every version-gated
// migration on every upgrade from a pre-split host. The reader must fall back
// to the legacy file instead.
func TestReadProvisionerVersion_FallsBackToLegacyStateFile(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(models.SetPaths(home))
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))

	legacy := NewState(filepath.Join(models.Paths().StateDir, StateFileName))
	legacy.ProvisionerState.Version = "1.2.3-legacy"
	b, err := yaml.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(models.Paths().StateDir, StateFileName), b, 0o644))

	_, statErr := os.Stat(componentFilePath(models.Paths().StateDir, ComponentMachine))
	require.True(t, os.IsNotExist(statErr), "precondition: machine.yaml must not exist yet")

	got, err := ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Equal(t, "1.2.3-legacy", got, "must read the version from the legacy file, not treat it as absent")
}

// TestReadProvisionerVersion_PrefersMachineFileOverLegacy: once the split has
// happened, machine.yaml is authoritative even if a stale state.yaml is still
// sitting around.
func TestReadProvisionerVersion_PrefersMachineFileOverLegacy(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(models.SetPaths(home))
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))

	legacy := NewState(filepath.Join(models.Paths().StateDir, StateFileName))
	legacy.ProvisionerState.Version = "0.0.0-stale-legacy"
	b, err := yaml.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(models.Paths().StateDir, StateFileName), b, 0o644))

	machine := NewState(filepath.Join(models.Paths().StateDir, StateFileName))
	machine.ProvisionerState.Version = "9.9.9-current"
	mb, err := yaml.Marshal(machine)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(componentFilePath(models.Paths().StateDir, ComponentMachine), mb, 0o644))

	got, err := ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Equal(t, "9.9.9-current", got, "machine.yaml must win once it exists")
}

// TestPersistProvisionerVersion_OnlyCreatesMachineFile verifies the version
// backfill writes only machine.yaml. It's called from startup-migration
// backfill and the cluster-install tail step, neither of which manages
// cluster/blocknode/consensus/teleport — writing those files anyway is
// exactly the class of stray write the per-component split exists to prevent.
func TestPersistProvisionerVersion_OnlyCreatesMachineFile(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "state.yaml")

	require.NoError(t, PersistProvisionerVersion(
		WithState(newTestState(tmp)),
		WithFileManager(newTestFileManager(t)),
	))

	_, err := os.Stat(componentFilePath(dir, ComponentMachine))
	require.NoError(t, err, "machine.yaml must exist")

	for _, id := range []ComponentID{ComponentCluster, ComponentBlockNode, ComponentConsensus, ComponentTeleport} {
		_, err := os.Stat(componentFilePath(dir, id))
		require.Truef(t, os.IsNotExist(err), "%s state file must not be created by a version-only persist", id)
	}
}
