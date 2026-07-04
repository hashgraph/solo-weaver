// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/models"
	osx "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireRoot skips the test if not running as root. Creating and removing
// symlinks under /usr/lib/systemd/system requires root privileges.
func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("test requires root privileges (euid != 0) — run via task vm:test:integration")
	}
}

// daemonTestPaths returns a WeaverPaths rooted at a temp sandbox so that unit
// file placement tests do not touch the real weaver home. The symlink path
// still targets /usr/lib/systemd/system (requires root in the test VM).
func daemonTestPaths(t *testing.T) models.WeaverPaths {
	t.Helper()
	home := t.TempDir()
	paths := models.NewWeaverPaths(home)
	return *paths
}

// cleanupDaemonService removes any left-over symlink, sandbox file, and
// daemon-reloads. Errors are intentionally ignored — this is cleanup only.
func cleanupDaemonService(t *testing.T, paths models.WeaverPaths) {
	t.Helper()
	ctx := context.Background()
	_ = osx.StopService(ctx, daemonServiceName)
	_ = osx.DisableService(ctx, daemonServiceName)
	removeDaemonServiceFiles(paths.DaemonServiceSandboxPath, paths.DaemonServiceSymlinkPath)
	_ = osx.DaemonReload(ctx)
}

// Test_DaemonService_FilePlacement_Integration verifies that
// installDaemonServiceFiles writes the sandbox file and creates the
// /usr/lib/systemd/system symlink pointing to it, and that
// removeDaemonServiceFiles tears both down.
func Test_DaemonService_FilePlacement_Integration(t *testing.T) {
	requireRoot(t)

	paths := daemonTestPaths(t)
	cleanupDaemonService(t, paths)
	t.Cleanup(func() { cleanupDaemonService(t, paths) })

	// Install files
	err := installDaemonServiceFiles(paths.DaemonServiceSandboxPath, paths.DaemonServiceSymlinkPath, nil)
	require.NoError(t, err)

	// Sandbox file must exist and be non-empty
	fi, err := os.Stat(paths.DaemonServiceSandboxPath)
	require.NoError(t, err, "sandbox unit file should exist")
	assert.Greater(t, fi.Size(), int64(0))

	// Symlink must exist and resolve to the sandbox path
	target, err := os.Readlink(paths.DaemonServiceSymlinkPath)
	require.NoError(t, err, "system symlink should exist")
	assert.Equal(t, filepath.Clean(paths.DaemonServiceSandboxPath), filepath.Clean(target))

	// Remove files
	removeDaemonServiceFiles(paths.DaemonServiceSandboxPath, paths.DaemonServiceSymlinkPath)

	_, err = os.Lstat(paths.DaemonServiceSymlinkPath)
	assert.True(t, os.IsNotExist(err), "symlink should be gone after removal")

	_, err = os.Stat(paths.DaemonServiceSandboxPath)
	assert.True(t, os.IsNotExist(err), "sandbox file should be gone after removal")
}

// Test_DaemonService_EnableDisable_Integration verifies that after placing the
// unit file and running daemon-reload, the daemon service can be enabled and
// then disabled. It does not start the service (Type=notify requires the daemon
// binary to send READY=1, which is not available in the test environment).
func Test_DaemonService_EnableDisable_Integration(t *testing.T) {
	requireRoot(t)

	paths := daemonTestPaths(t)
	cleanupDaemonService(t, paths)
	t.Cleanup(func() { cleanupDaemonService(t, paths) })

	ctx := context.Background()

	// Place files + daemon-reload
	require.NoError(t, installDaemonServiceFiles(paths.DaemonServiceSandboxPath, paths.DaemonServiceSymlinkPath, nil))
	require.NoError(t, osx.DaemonReload(ctx))

	// Enable
	require.NoError(t, osx.EnableService(ctx, daemonServiceName))
	enabled, err := osx.IsServiceEnabled(ctx, daemonServiceName)
	require.NoError(t, err)
	assert.True(t, enabled, "service should be enabled after EnableService")

	// Disable
	require.NoError(t, osx.DisableService(ctx, daemonServiceName))
	enabled, err = osx.IsServiceEnabled(ctx, daemonServiceName)
	require.NoError(t, err)
	assert.False(t, enabled, "service should not be enabled after DisableService")
}

// Test_InstallDaemonServiceStep_Rollback_Integration verifies that the
// rollback handler of InstallDaemonServiceStep cleans up the sandbox file and
// symlink and disables the service — restoring a clean pre-install state.
//
// Note: the step's Execute call will fail at RestartService (the daemon binary
// is not present in the test environment), so we exercise the rollback path
// directly after a partial install via the helper functions.
func Test_InstallDaemonServiceStep_Rollback_Integration(t *testing.T) {
	requireRoot(t)

	paths := daemonTestPaths(t)
	cleanupDaemonService(t, paths)
	t.Cleanup(func() { cleanupDaemonService(t, paths) })

	ctx := context.Background()

	// Simulate a partial install: files placed + daemon-reload + enabled,
	// but service never started (as if RestartService had failed).
	require.NoError(t, installDaemonServiceFiles(paths.DaemonServiceSandboxPath, paths.DaemonServiceSymlinkPath, nil))
	require.NoError(t, osx.DaemonReload(ctx))
	require.NoError(t, osx.EnableService(ctx, daemonServiceName))

	// Build and run the rollback
	step, err := InstallDaemonServiceStep(paths, nil).Build()
	require.NoError(t, err)

	rollbackReport := step.Rollback(ctx)
	require.NotNil(t, rollbackReport)
	assert.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Sandbox file and symlink should be gone
	_, errSandbox := os.Stat(paths.DaemonServiceSandboxPath)
	assert.True(t, os.IsNotExist(errSandbox), "sandbox file should be removed by rollback")

	_, errSymlink := os.Lstat(paths.DaemonServiceSymlinkPath)
	assert.True(t, os.IsNotExist(errSymlink), "symlink should be removed by rollback")

	// Service should be disabled
	enabled, err := osx.IsServiceEnabled(ctx, daemonServiceName)
	require.NoError(t, err)
	assert.False(t, enabled, "service should be disabled by rollback")
}

// Test_RemoveDaemonServiceStep_Integration verifies that RemoveDaemonServiceStep
// removes the symlink and sandbox file and disables the service.
func Test_RemoveDaemonServiceStep_Integration(t *testing.T) {
	requireRoot(t)

	paths := daemonTestPaths(t)
	cleanupDaemonService(t, paths)
	t.Cleanup(func() { cleanupDaemonService(t, paths) })

	ctx := context.Background()

	// Simulate an installed (but not running) service
	require.NoError(t, installDaemonServiceFiles(paths.DaemonServiceSandboxPath, paths.DaemonServiceSymlinkPath, nil))
	require.NoError(t, osx.DaemonReload(ctx))
	require.NoError(t, osx.EnableService(ctx, daemonServiceName))

	// Run remove step
	step, err := RemoveDaemonServiceStep(paths).Build()
	require.NoError(t, err)

	report := step.Execute(ctx)
	require.NotNil(t, report)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	assert.NoError(t, report.Error)

	// Symlink gone
	_, errSymlink := os.Lstat(paths.DaemonServiceSymlinkPath)
	assert.True(t, os.IsNotExist(errSymlink), "symlink should be removed")

	// Sandbox file gone
	_, errSandbox := os.Stat(paths.DaemonServiceSandboxPath)
	assert.True(t, os.IsNotExist(errSandbox), "sandbox file should be removed")

	// Service disabled
	enabled, err := osx.IsServiceEnabled(ctx, daemonServiceName)
	require.NoError(t, err)
	assert.False(t, enabled, "service should be disabled after uninstall")
}
