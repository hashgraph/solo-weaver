// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/stretchr/testify/require"
)

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755))
}

func platformSuffixedDaemonName() string {
	return fmt.Sprintf("%s-%s-%s", software.DaemonBinaryName, runtime.GOOS, runtime.GOARCH)
}

func TestFindColocatedDaemonBinary(t *testing.T) {
	t.Parallel()

	t.Run("finds the plain name", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		want := filepath.Join(dir, software.DaemonBinaryName)
		writeExecutable(t, want)

		require.Equal(t, want, findColocatedDaemonBinary(dir))
	})

	t.Run("finds the platform-suffixed build artifact", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		want := filepath.Join(dir, platformSuffixedDaemonName())
		writeExecutable(t, want)

		require.Equal(t, want, findColocatedDaemonBinary(dir))
	})

	// A directory holding both is the install target after a previous run left the
	// canonical name behind; the canonical name is what the service unit expects.
	t.Run("prefers the plain name over the suffixed one", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		want := filepath.Join(dir, software.DaemonBinaryName)
		writeExecutable(t, want)
		writeExecutable(t, filepath.Join(dir, platformSuffixedDaemonName()))

		require.Equal(t, want, findColocatedDaemonBinary(dir))
	})

	// A non-executable file of the right name is a leftover or a partial download,
	// not something that can be installed as the daemon.
	t.Run("ignores a non-executable candidate", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, software.DaemonBinaryName), []byte("x"), 0o644))

		require.Empty(t, findColocatedDaemonBinary(dir))
	})

	t.Run("ignores a directory of the right name", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, software.DaemonBinaryName), 0o755))

		require.Empty(t, findColocatedDaemonBinary(dir))
	})

	t.Run("returns empty when nothing matches", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, findColocatedDaemonBinary(t.TempDir()))
	})
}

// The step must never fail the self-install: an official install has no sibling
// daemon binary to find, and the daemon is downloaded from the catalog later.
func TestInstallColocatedDaemonBinary_NoSiblingSucceedsWithoutInstalling(t *testing.T) {
	t.Parallel()
	binDir := t.TempDir()

	step, err := InstallColocatedDaemonBinary(binDir).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	// The test binary's own directory holds no daemon binary, so nothing is copied.
	_, err = os.Stat(filepath.Join(binDir, software.DaemonBinaryName))
	require.True(t, os.IsNotExist(err), "no daemon binary should have been installed")
}

func TestCopyBinaryAtomic(t *testing.T) {
	t.Parallel()

	t.Run("installs the source and makes it executable", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		require.NoError(t, os.WriteFile(src, []byte("new-binary"), 0o755))
		dst := filepath.Join(dir, "sub", "dst")

		require.NoError(t, copyBinaryAtomic(src, dst))

		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		require.Equal(t, "new-binary", string(got))

		info, err := os.Stat(dst)
		require.NoError(t, err)
		require.NotZero(t, info.Mode().Perm()&0o111, "installed binary must be executable")
	})

	// The point of the atomic path: a failure must not damage what was already
	// installed. An unreadable source fails after the destination directory has
	// been touched but before anything is renamed into place.
	t.Run("leaves an existing destination intact when the copy fails", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		dst := filepath.Join(dir, "dst")
		require.NoError(t, os.WriteFile(dst, []byte("existing-binary"), 0o755))

		require.Error(t, copyBinaryAtomic(filepath.Join(dir, "missing"), dst))

		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		require.Equal(t, "existing-binary", string(got), "a failed install must not truncate the old binary")
	})

	t.Run("leaves no temp files behind", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		require.NoError(t, os.WriteFile(src, []byte("x"), 0o755))
		dst := filepath.Join(dir, "dst")

		require.NoError(t, copyBinaryAtomic(src, dst))
		require.Error(t, copyBinaryAtomic(filepath.Join(dir, "missing"), dst))

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			require.NotContains(t, e.Name(), ".tmp.", "temp files must be cleaned up on both paths")
		}
	})
}

func TestSameFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	writeExecutable(t, a)
	writeExecutable(t, b)

	require.True(t, sameFile(a, a), "a path is the same file as itself")
	require.False(t, sameFile(a, b), "distinct files with identical content are not the same file")
	require.False(t, sameFile(a, filepath.Join(dir, "missing")), "a missing destination is not the same file")
	require.False(t, sameFile(filepath.Join(dir, "missing"), a), "a missing source is not the same file")

	// A symlinked destination resolves to the same inode — the case that matters,
	// since /usr/local/bin entries are symlinks into the weaver bin dir.
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(a, link))
	require.True(t, sameFile(a, link), "a symlink to the source is the same file")
}
