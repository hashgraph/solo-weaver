// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readProvisionerVersionFrom parses the provisioner version out of a state file
// at an arbitrary path using the EXACT same doc struct and unmarshaller that the
// production reader (ReadProvisionerVersionFromDisk) uses. This lets the test
// assert the round-trip a real second invocation would observe, without the
// reader's fixed models.Paths() path.
func readProvisionerVersionFrom(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc ProvisionerVersionDoc
	require.NoError(t, unmarshalStateDoc(data, &doc))
	return doc.State.Provisioner.Version
}

// TestPersistProvisionerVersion_CreatesFileWhenAbsent proves that on a
// pre-state-tracking host (no state.yaml) PersistProvisionerVersion writes a
// state file whose provisioner.version is the running binary's version — which
// is exactly what ReadProvisionerVersionFromDisk returns on the next invocation.
func TestPersistProvisionerVersion_CreatesFileWhenAbsent(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "state.yaml")

	_, err := os.Stat(tmp)
	require.True(t, os.IsNotExist(err), "precondition: state file must not exist")

	err = PersistProvisionerVersion(
		WithState(newTestState(tmp)),
		WithFileManager(newTestFileManager(t)),
	)
	require.NoError(t, err)

	_, err = os.Stat(tmp)
	require.NoError(t, err, "state file should now exist")

	assert.Equal(t, version.Get().Version, readProvisionerVersionFrom(t, tmp))
}

// TestPersistProvisionerVersion_PreservesExistingState proves the write is a
// merge, not a clobber: reality-detected fields already on disk (here a software
// entry) survive, and only provisioner.version is advanced to the current binary.
func TestPersistProvisionerVersion_PreservesExistingState(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "state.yaml")
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
	assert.Equal(t, version.Get().Version, readProvisionerVersionFrom(t, tmp))

	reloaded, err := NewStateManager(WithState(newTestState(tmp)), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, reloaded.Refresh())
	sw, ok := reloaded.State().MachineState.Software[swKey]
	require.True(t, ok, "pre-existing software entry must be preserved")
	assert.Equal(t, "1.30.0", sw.Version)
}

// TestPersistProvisionerVersion_Idempotent proves repeated calls are safe (the
// optimistic-concurrency baseline is set on each call), so a per-command hook
// never errors on the second run.
func TestPersistProvisionerVersion_Idempotent(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "state.yaml")
	fm := newTestFileManager(t)

	for i := range 3 {
		err := PersistProvisionerVersion(WithState(newTestState(tmp)), WithFileManager(fm))
		require.NoErrorf(t, err, "call %d should succeed", i)
	}
	assert.Equal(t, version.Get().Version, readProvisionerVersionFrom(t, tmp))
}
