// SPDX-License-Identifier: Apache-2.0

package fsx

import (
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestClose(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "fsx")
	require.NoError(t, err)
	defer RemoveAll(tmpDir)

	tmpfile, err := os.CreateTemp(tmpDir, "testfile")
	require.NoError(t, err)

	Close(tmpfile) // Should close without panic or error
	// Closing again should not panic, but will log error
	Close(tmpfile)
}

func TestRemove(t *testing.T) {
	tmpfile, err := ioutil.TempFile("", "testfile")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	path := tmpfile.Name()
	tmpfile.Close()

	Remove(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file %q should be removed", path)
	}

	// Removing non-existent file should not panic
	Remove(path)
}

func TestAtomicWriteFile_WritesContentAndPermissions(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "output")
	payload := []byte("hello atomic")

	require.NoError(t, AtomicWriteFile(dst, payload, 0o440))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, payload, got)

	info, err := os.Stat(dst)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o440), info.Mode().Perm())
}

func TestAtomicWriteFile_NoTempFileLeft_OnSuccess(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "output")

	require.NoError(t, AtomicWriteFile(dst, []byte("x"), 0o644))

	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the final file should remain after a successful write")
}

func TestAtomicWriteFile_OverwritesExistingFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "output")
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o644))

	require.NoError(t, AtomicWriteFile(dst, []byte("new"), 0o644))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, []byte("new"), got)
}

func TestRemoveAll(t *testing.T) {
	tmpdir, err := ioutil.TempDir("", "testdir")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	subfile := filepath.Join(tmpdir, "subfile")
	if err := os.WriteFile(subfile, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to create subfile: %v", err)
	}

	RemoveAll(tmpdir)
	if _, err := os.Stat(tmpdir); !os.IsNotExist(err) {
		t.Errorf("directory %q should be removed", tmpdir)
	}

	// Removing non-existent directory should not panic
	RemoveAll(tmpdir)
}
