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
)

// readProvisionerVersionFrom parses the provisioner version from a state file at an arbitrary path (unlike the reader's fixed models.Paths() path).
func readProvisionerVersionFrom(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var doc ProvisionerVersionDoc
	require.NoError(t, unmarshalStateDoc(data, &doc))
	return doc.State.Provisioner.Version
}

// TestPersistProvisionerVersion_CreatesFileWhenAbsent: with no state.yaml, the write creates one holding the running binary's version.
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

// TestPersistProvisionerVersion_PreservesExistingState: the write merges — an existing software entry survives; only provisioner.version advances.
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

// TestPersistProvisionerVersion_Idempotent: repeated calls succeed (the concurrency baseline resets each call).
func TestPersistProvisionerVersion_Idempotent(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "state.yaml")
	fm := newTestFileManager(t)

	for i := range 3 {
		err := PersistProvisionerVersion(WithState(newTestState(tmp)), WithFileManager(fm))
		require.NoErrorf(t, err, "call %d should succeed", i)
	}
	assert.Equal(t, version.Get().Version, readProvisionerVersionFrom(t, tmp))
}

// TestPersistThenReadProvisionerVersion_RoundTrip: writer and reader agree on the state-file path and shape — the round-trip #789 depends on.
func TestPersistThenReadProvisionerVersion_RoundTrip(t *testing.T) {
	home := t.TempDir()
	restore := models.SetPaths(home)
	t.Cleanup(restore)

	stateFile := filepath.Join(models.Paths().StateDir, StateFileName)

	// Precondition: state dir exists but no state.yaml — the reader returns "".
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))
	_, statErr := os.Stat(stateFile)
	require.True(t, os.IsNotExist(statErr), "precondition: state file must not exist yet")

	absent, err := ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Empty(t, absent, "absent state.yaml must read back as an empty version")

	// Write the running version at the default path (no opts → the reader's path).
	require.NoError(t, PersistProvisionerVersion())

	_, statErr = os.Stat(stateFile)
	require.NoError(t, statErr, "state file should now exist at the reader's path")

	// The reader observes exactly the version just written (non-empty).
	readBack, err := ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.NotEmpty(t, readBack, "recorded version must read back non-empty, unlike the absent case")
	assert.Equal(t, version.Get().Version, readBack,
		"the reader must observe the version the writer persisted")
}
