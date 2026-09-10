// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeInfraVersions writes manifests/infrastructure-versions.yaml into a package.
func writeInfraVersions(t *testing.T, pkgDir, content string) {
	t.Helper()
	dir := filepath.Join(pkgDir, "manifests")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "infrastructure-versions.yaml"), []byte(content), 0o644))
}

func TestPlaceInfraVersions_CopiesAndOverwrites(t *testing.T) {
	pkg := t.TempDir()
	writeInfraVersions(t, pkg, "schemaVersion: 1\n")
	dest := filepath.Join(t.TempDir(), "config", "infrastructure-versions.yaml")
	// A stale copy already at the trusted location must be overwritten (it is a
	// destination, not a human-owned override).
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, os.WriteFile(dest, []byte("stale content"), 0o644))

	require.NoError(t, placeInfraVersions(pkg, dest))

	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "schemaVersion: 1\n", string(got))
}

func TestPlaceInfraVersions_CreatesDestDir(t *testing.T) {
	pkg := t.TempDir()
	writeInfraVersions(t, pkg, "x: 1\n")
	dest := filepath.Join(t.TempDir(), "nested", "config", "infrastructure-versions.yaml")

	require.NoError(t, placeInfraVersions(pkg, dest))
	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "x: 1\n", string(got))
}

func TestPlaceInfraVersions_BacksUpPreviousOnChange(t *testing.T) {
	pkg := t.TempDir()
	writeInfraVersions(t, pkg, "version: 2\n")
	dest := filepath.Join(t.TempDir(), "infrastructure-versions.yaml")
	require.NoError(t, os.WriteFile(dest, []byte("version: 1\n"), 0o644))

	require.NoError(t, placeInfraVersions(pkg, dest))

	got, _ := os.ReadFile(dest)
	assert.Equal(t, "version: 2\n", string(got), "new content placed")
	bak, err := os.ReadFile(dest + infraVersionsBackupSuffix)
	require.NoError(t, err, "previous file should be backed up")
	assert.Equal(t, "version: 1\n", string(bak))
}

func TestPlaceInfraVersions_NoBackupWhenFreshOrUnchanged(t *testing.T) {
	pkg := t.TempDir()
	writeInfraVersions(t, pkg, "version: 1\n")
	dest := filepath.Join(t.TempDir(), "infrastructure-versions.yaml")

	// Fresh destination: nothing to back up.
	require.NoError(t, placeInfraVersions(pkg, dest))
	_, err := os.Stat(dest + infraVersionsBackupSuffix)
	assert.True(t, os.IsNotExist(err), "no prior file ⇒ no backup")

	// Unchanged re-run: idempotent skip, still no backup.
	require.NoError(t, placeInfraVersions(pkg, dest))
	_, err = os.Stat(dest + infraVersionsBackupSuffix)
	assert.True(t, os.IsNotExist(err), "identical re-run ⇒ no backup churn")
}

func TestPlaceInfraVersions_AbsentIsNoop(t *testing.T) {
	pkg := t.TempDir() // no manifests/ dir
	dest := filepath.Join(t.TempDir(), "infrastructure-versions.yaml")

	require.NoError(t, placeInfraVersions(pkg, dest))
	_, err := os.Stat(dest)
	assert.True(t, os.IsNotExist(err), "no package manifest ⇒ nothing placed, no constraint")
}

func TestPlaceInfraVersions_Idempotent(t *testing.T) {
	pkg := t.TempDir()
	writeInfraVersions(t, pkg, "x: 1\n")
	dest := filepath.Join(t.TempDir(), "infrastructure-versions.yaml")

	require.NoError(t, placeInfraVersions(pkg, dest))
	require.NoError(t, placeInfraVersions(pkg, dest), "re-running must be a no-op, not an error")
	got, _ := os.ReadFile(dest)
	assert.Equal(t, "x: 1\n", string(got))
}

func TestPlaceInfraVersions_SymlinkSourceIsFatal(t *testing.T) {
	pkg := t.TempDir()
	mdir := filepath.Join(pkg, "manifests")
	require.NoError(t, os.MkdirAll(mdir, 0o755))
	target := filepath.Join(pkg, "real.yaml")
	require.NoError(t, os.WriteFile(target, []byte("x: 1\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(mdir, "infrastructure-versions.yaml")))
	dest := filepath.Join(t.TempDir(), "infrastructure-versions.yaml")

	err := placeInfraVersions(pkg, dest)
	require.Error(t, err)
	assert.False(t, errorx.IsTemporary(err), "a symlinked manifest is fatal, not retried")
	r, _ := errx.ReasonOf(err)
	assert.Equal(t, ReasonConfigFileSymlink, r)
}
