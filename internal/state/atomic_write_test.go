// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordDirSyncs replaces syncDir with one that records each directory it is
// asked to sync and returns err, and restores it when the test ends.
func recordDirSyncs(t *testing.T, err error) *[]string {
	t.Helper()
	orig := syncDir
	t.Cleanup(func() { syncDir = orig })

	synced := &[]string{}
	syncDir = func(dir string) error {
		*synced = append(*synced, dir)
		return err
	}
	return synced
}

func TestAtomicWriteFile_DirectorySyncFailure(t *testing.T) {
	syncErr := errors.New("disk gone")
	recordDirSyncs(t, syncErr)
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o640))

	err := atomicWriteFile(path, []byte("new"))

	require.ErrorIs(t, err, syncErr)
	// The rename already happened: the new content is in place and no temp file is left.
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "new", string(got))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestAtomicWriteFile_ParentSyncFailureIsBestEffort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")
	emptyWarnings(t)
	orig := syncDir
	t.Cleanup(func() { syncDir = orig })
	syncDir = func(d string) error {
		if d == filepath.Dir(dir) {
			return errors.New("disk gone")
		}
		return nil
	}

	require.NoError(t, atomicWriteFile(path, []byte("new")))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "new", string(got))
	warnings := TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], filepath.Dir(dir))
}

func TestAtomicWriteFile_NewDirectorySyncsParent(t *testing.T) {
	synced := recordDirSyncs(t, nil)
	parent := t.TempDir()
	dir := filepath.Join(parent, "state")

	require.NoError(t, atomicWriteFile(filepath.Join(dir, "state.yaml"), []byte("new")))

	require.Equal(t, []string{dir, parent}, *synced)
}

func TestAtomicWriteFile_ExistingDirectorySyncsParent(t *testing.T) {
	emptyWarnings(t)
	synced := recordDirSyncs(t, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "state.yaml")

	require.NoError(t, atomicWriteFile(path, []byte("new")))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "new", string(got))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	require.Equal(t, []string{dir, filepath.Dir(dir)}, *synced)
	require.Empty(t, TakeWarnings())
}
