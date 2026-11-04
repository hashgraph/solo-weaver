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

// readProvisionerRecordFrom reads provisioner.yaml inside dir and fails the
// test when it does not exist.
func readProvisionerRecordFrom(t *testing.T, dir string) ProvisionerRecord {
	t.Helper()
	rec, ok, err := readProvisionerRecord(dir)
	require.NoError(t, err)
	require.True(t, ok, "provisioner.yaml should exist")
	return rec
}

// writeLegacyStateWithVersion writes a pre-split state.yaml into dir that
// records cliVersion the way such a file did: inside its state record.
func writeLegacyStateWithVersion(t *testing.T, dir, cliVersion string) {
	t.Helper()
	path := filepath.Join(dir, StateFileName)
	b, err := yaml.Marshal(NewState(path))
	require.NoError(t, err)
	b, err = withLegacyProvisionerFields(b, ProvisionerRecord{CLI: ProvisionerInfo{Version: cliVersion, StateVersion: ModelVersion}})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(path, b, 0o644))
}

// TestPersistProvisionerVersion_RecordsTheRunningCLI: the write creates
// provisioner.yaml describing the running binary and the state model it writes.
func TestPersistProvisionerVersion_RecordsTheRunningCLI(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, persistProvisionerVersionIn(dir))

	rec := readProvisionerRecordFrom(t, dir)
	assert.Equal(t, version.Get().Version, rec.CLI.Version)
	assert.Equal(t, ModelVersion, rec.CLI.StateVersion)
}

// TestPersistProvisionerVersion_OnlyWritesProvisionerFile: it is called from
// the startup pass and from Kubernetes setup, neither of which owns any
// component, so it must not create a component file.
func TestPersistProvisionerVersion_OnlyWritesProvisionerFile(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, persistProvisionerVersionIn(dir))

	for _, id := range AllComponentIDs {
		_, err := os.Stat(componentFilePath(dir, id))
		require.Truef(t, os.IsNotExist(err), "%s state file must not be created by a version-only persist", id)
	}
}

// TestPersistProvisionerVersion_Idempotent: repeated calls succeed and keep the
// same record.
func TestPersistProvisionerVersion_Idempotent(t *testing.T) {
	dir := t.TempDir()

	for i := range 3 {
		require.NoErrorf(t, persistProvisionerVersionIn(dir), "call %d should succeed", i)
	}
	assert.Equal(t, version.Get().Version, readProvisionerRecordFrom(t, dir).CLI.Version)
}

// TestPersistProvisionerVersion_FailsFastWhenLocked: a writer holding the
// provisioner lock makes the write fail at once, leaving the file untouched.
func TestPersistProvisionerVersion_FailsFastWhenLocked(t *testing.T) {
	dir := t.TempDir()
	release, err := AcquireProvisionerLock(dir, 0)
	require.NoError(t, err)
	defer release()

	err = persistProvisionerVersionIn(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "provisioner.yaml is being modified")

	_, ok, err := readProvisionerRecord(dir)
	require.NoError(t, err)
	require.False(t, ok, "a refused write must not create provisioner.yaml")
}

// TestPersistThenReadProvisionerVersion_RoundTrip: writer and reader agree on
// the file's path and shape — the round-trip #789 depends on.
func TestPersistThenReadProvisionerVersion_RoundTrip(t *testing.T) {
	t.Cleanup(models.SetPaths(t.TempDir()))
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))

	absent, err := ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Empty(t, absent, "no recorded version must read back as an empty version")

	require.NoError(t, PersistProvisionerVersion())

	readBack, err := ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Equal(t, version.Get().Version, readBack,
		"the reader must observe the version the writer persisted")
}

// TestReadProvisionerVersion_FallsBackToLegacyStateFile: on a host the
// per-component split has not reached yet, provisioner.yaml does not exist but
// state.yaml has a real recorded version. RunStartupMigrations reads it to
// decide which migrations apply — including the split itself — before any has
// run, so the reader must fall back to state.yaml rather than report "" and
// re-run every version-gated migration.
func TestReadProvisionerVersion_FallsBackToLegacyStateFile(t *testing.T) {
	t.Cleanup(models.SetPaths(t.TempDir()))
	dir := models.Paths().StateDir
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, StateFileName), []byte(v034StateYAML), 0o644))

	got, err := ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Equal(t, "0.34.0", got, "must read the version from the legacy file, not treat it as absent")
}

// TestReadProvisionerVersion_PrefersProvisionerFileOverLegacy: once the split
// has happened, provisioner.yaml is authoritative even if a stale state.yaml
// is still sitting around.
func TestReadProvisionerVersion_PrefersProvisionerFileOverLegacy(t *testing.T) {
	t.Cleanup(models.SetPaths(t.TempDir()))
	dir := models.Paths().StateDir
	writeLegacyStateWithVersion(t, dir, "0.0.0-stale-legacy")
	require.NoError(t, writeProvisionerRecord(dir, ProvisionerRecord{CLI: ProvisionerInfo{Version: "9.9.9-current"}}))

	got, err := ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Equal(t, "9.9.9-current", got, "provisioner.yaml must win once it exists")
}

// TestReadProvisionerVersion_IgnoresMachineFile: a machine.yaml written before
// the version moved to provisioner.yaml still carries it, but such files exist
// only on development hosts, which are reprovisioned rather than migrated.
func TestReadProvisionerVersion_IgnoresMachineFile(t *testing.T) {
	t.Cleanup(models.SetPaths(t.TempDir()))
	dir := models.Paths().StateDir
	writeLegacyStateWithVersion(t, dir, "1.2.3-in-machine-file")
	require.NoError(t, os.Rename(filepath.Join(dir, StateFileName), filepath.Join(dir, "machine.yaml")))

	got, err := ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestFlushingHostStateDoesNotRecordTheVersion: a command's refresh and flush
// no longer stamp the running version anywhere; only the version-record path
// writes it.
func TestFlushingHostStateDoesNotRecordTheVersion(t *testing.T) {
	dir := t.TempDir()
	sm, err := NewStateManager(WithState(newTestState(filepath.Join(dir, StateFileName))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	require.NoError(t, sm.FlushScoped(ComponentProfile, ComponentFirewall, ComponentSystem))

	_, ok, err := readProvisionerRecord(dir)
	require.NoError(t, err)
	assert.False(t, ok, "a component flush must not create provisioner.yaml")

	for _, id := range []ComponentID{ComponentProfile, ComponentFirewall, ComponentSystem} {
		b, err := os.ReadFile(componentFilePath(dir, id))
		require.NoError(t, err)
		var doc ProvisionerVersionDoc
		require.NoError(t, yaml.Unmarshal(b, &doc))
		assert.Emptyf(t, doc.State.Provisioner.Version, "%s.yaml must not carry the provisioner version", id)
		assert.Emptyf(t, doc.State.Version, "%s.yaml must not carry the state model version", id)
	}
}
