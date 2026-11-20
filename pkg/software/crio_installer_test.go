// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

// Test_generateExpectedCrioSocketDropIn pins the crio.service drop-in that bridges
// cri-o's sandbox API socket to the default /var/run/crio/crio.sock path that
// vendored cAdvisor dials with a hardcoded const (issue #22). Without this bridge
// the "crio-images" imagefs label is never registered and kubelet's eviction
// manager logs `non-existent label "crio-images"` on every sync.
func Test_generateExpectedCrioSocketDropIn(t *testing.T) {
	installer, err := NewCrioInstaller()
	require.NoError(t, err)

	ci, ok := installer.(*crioInstaller)
	require.True(t, ok, "expected *crioInstaller")

	content, err := ci.generateExpectedCrioSocketDropIn()
	require.NoError(t, err)

	sandboxSocket := filepath.Join(models.Paths().SandboxDir, "var/run/crio/crio.sock")

	// systemd override section
	require.Contains(t, content, "[Service]")

	// ExecStartPost recreates the default-path symlink on every start (surviving the
	// tmpfs /run wipe on reboot); it must mkdir the parent and point at the sandbox socket.
	require.Contains(t, content, "ExecStartPost=")
	require.Contains(t, content, "mkdir -p /var/run/crio")
	require.Contains(t, content, "ln -sfn "+sandboxSocket+" /var/run/crio/crio.sock")

	// ExecStopPost removes the symlink; the leading '-' keeps cleanup from failing the unit.
	require.Contains(t, content, "ExecStopPost=-")
	require.Contains(t, content, "rm -f /var/run/crio/crio.sock")

	// systemd native exec form only — the interpolated sandbox path must never be
	// handed to a shell for re-parsing (no "sh -c" exec wrapper).
	require.NotContains(t, content, "sh -c")

	// The drop-in bridges to the sandbox socket; it must never rebind cri-o's listen socket.
	require.NotContains(t, content, "crio.api.listen")
}

// Test_crioSocketDropInPaths pins the sandbox and host locations of the socket-bridge drop-in.
func Test_crioSocketDropInPaths(t *testing.T) {
	require.Equal(t, "10-sandbox-socket.conf", CrioSocketDropInFile)
	require.Equal(t, "crio.service.d", CrioServiceDropInDir)

	require.Equal(t,
		filepath.Join(models.Paths().SandboxDir, "usr", "lib", "systemd", "system", CrioServiceDropInDir),
		getCrioServiceDropInDir())
	require.Equal(t,
		filepath.Join(getCrioServiceDropInDir(), CrioSocketDropInFile),
		getCrioSocketDropInPath())
	require.Equal(t, "/usr/lib/systemd/system/crio.service.d", getSystemCrioServiceDropInDir())
}

// writeCrioRelease writes every cri-o release file into the sandbox with
// content, and returns the binaries and the configs.
func writeCrioRelease(t *testing.T, content string) (binaries, configs []string) {
	t.Helper()
	sandbox := models.Paths().SandboxDir
	binaries, configs = sortedPaths(crioInstalledBinaryPaths(sandbox)), sortedPaths(crioReleaseConfigs(sandbox))
	for _, p := range append(append([]string{}, binaries...), configs...) {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	return binaries, configs
}

func backupPath(t *testing.T, backupDir, p string) string {
	t.Helper()
	rel, err := filepath.Rel(models.Paths().SandboxDir, p)
	require.NoError(t, err)
	return filepath.Join(backupDir, rel)
}

func TestMoveCrioReleaseAside_SettleKeepsTheConfigs(t *testing.T) {
	restorePaths := models.SetPaths(t.TempDir())
	defer restorePaths()
	sandbox := models.Paths().SandboxDir
	binaries, configs := writeCrioRelease(t, "old")
	ours := filepath.Join(sandbox, crioConfdDir, "20-ours.conf")
	require.NoError(t, os.WriteFile(ours, []byte("ours"), 0o644))
	backupDir := CrioReleaseBackupDir("1.34.15")

	_, settle, err := MoveCrioReleaseAside(backupDir)
	require.NoError(t, err)

	for _, p := range append(append([]string{}, binaries...), configs...) {
		assert.NoFileExists(t, p)
		assert.FileExists(t, backupPath(t, backupDir, p))
	}
	assert.FileExists(t, ours, "files that are not the release's stay")

	writeCrioRelease(t, "new") // the new release's install
	require.NoError(t, settle())
	for _, p := range binaries {
		assert.NoFileExists(t, backupPath(t, backupDir, p), "the old binaries are not kept")
	}
	for _, p := range configs {
		data, err := os.ReadFile(backupPath(t, backupDir, p))
		require.NoError(t, err)
		assert.Equal(t, "old", string(data), "the old configs are kept for the operator")
	}
}

func TestMoveCrioReleaseAside_RestorePutsTheReleaseBack(t *testing.T) {
	restorePaths := models.SetPaths(t.TempDir())
	defer restorePaths()
	binaries, configs := writeCrioRelease(t, "old")
	backupDir := CrioReleaseBackupDir("1.34.15")

	restore, _, err := MoveCrioReleaseAside(backupDir)
	require.NoError(t, err)
	// A failed install leaves part of the new release behind.
	require.NoError(t, os.WriteFile(binaries[0], []byte("new"), 0o755))
	require.NoError(t, restore())

	for _, p := range append(append([]string{}, binaries...), configs...) {
		data, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.Equal(t, "old", string(data), p)
	}
}

// A re-run after a failed install keeps the release that was there first.
func TestMoveCrioReleaseAside_ReRunKeepsTheFirstBackup(t *testing.T) {
	restorePaths := models.SetPaths(t.TempDir())
	defer restorePaths()
	binaries, _ := writeCrioRelease(t, "old")
	backupDir := CrioReleaseBackupDir("1.34.15")
	_, _, err := MoveCrioReleaseAside(backupDir)
	require.NoError(t, err)
	writeCrioRelease(t, "partial")

	_, _, err = MoveCrioReleaseAside(backupDir)
	require.NoError(t, err)

	data, err := os.ReadFile(backupPath(t, backupDir, binaries[0]))
	require.NoError(t, err)
	assert.Equal(t, "old", string(data))
	assert.NoFileExists(t, binaries[0])
}

func TestMoveCrioReleaseAside_FailsWhenTheBackupCannotBeWritten(t *testing.T) {
	restorePaths := models.SetPaths(t.TempDir())
	defer restorePaths()
	writeCrioRelease(t, "old")
	backupDir := CrioReleaseBackupDir("1.34.15")
	require.NoError(t, os.MkdirAll(filepath.Dir(backupDir), 0o755))
	require.NoError(t, os.WriteFile(backupDir, []byte("a file, not a directory"), 0o644))

	_, _, err := MoveCrioReleaseAside(backupDir)

	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalState), err.Error())
}
