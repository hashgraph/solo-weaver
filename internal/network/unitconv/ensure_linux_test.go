// SPDX-License-Identifier: Apache-2.0

//go:build linux

package unitconv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAtomicWriteUnit covers the write half of EnsureUnit. Filesystem-only; the
// daemon-reload and enable around it are covered by the integration suites in
// internal/workflows.
func TestAtomicWriteUnit(t *testing.T) {
	t.Run("writes the content at 0644", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "unit.service")
		require.NoError(t, atomicWriteUnit(path, embeddedUnit))

		written, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, embeddedUnit, written)

		info, err := os.Stat(path)
		require.NoError(t, err)
		// systemd ignores a unit it cannot read; 0600 from CreateTemp is not enough.
		require.Equal(t, os.FileMode(0o644), info.Mode().Perm(),
			"the unit must be world-readable, not left at the temp file's mode")
	})

	t.Run("creates a missing unit directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "systemd", "system", "unit.service")
		require.NoError(t, atomicWriteUnit(path, embeddedUnit))
		require.FileExists(t, path)
	})

	// The point of the temp-file dance: a reader either sees the old unit or the
	// new one, never a truncated file systemd would fail to parse at boot.
	t.Run("replaces an existing unit and leaves no temp file behind", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "unit.service")
		require.NoError(t, os.WriteFile(path, []byte("[Unit]\nAfter=local-fs.target\n"), 0o644))

		require.NoError(t, atomicWriteUnit(path, embeddedUnit))

		written, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, embeddedUnit, written)
		requireNoTempFiles(t, dir)
	})

	t.Run("a rename that cannot succeed is an error, not a silent no-op", func(t *testing.T) {
		dir := t.TempDir()
		// A directory where the unit belongs: the rename onto it fails, and the
		// caller must hear about it rather than daemon-reload an unchanged unit.
		path := filepath.Join(dir, "unit.service")
		require.NoError(t, os.Mkdir(path, 0o755))

		require.Error(t, atomicWriteUnit(path, embeddedUnit))
		// The failed write must not litter /usr/lib/systemd/system.
		requireNoTempFiles(t, dir)
	})

	t.Run("an unusable unit directory is an error", func(t *testing.T) {
		// A regular file where the parent directory belongs: MkdirAll fails.
		notADir := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o644))

		require.Error(t, atomicWriteUnit(filepath.Join(notADir, "unit.service"), embeddedUnit))
	})
}

// requireNoTempFiles asserts the temp file atomicWriteUnit stages through was
// cleaned up — by the rename on success, by the deferred remove on failure.
func requireNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.False(t, strings.HasPrefix(e.Name(), ".unitconv-"),
			"a staged temp file was left behind: %s", e.Name())
	}
}
