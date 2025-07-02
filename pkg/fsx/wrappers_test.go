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
