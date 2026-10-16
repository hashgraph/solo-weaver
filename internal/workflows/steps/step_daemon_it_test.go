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

// cleanupDaemonService removes any left-over unit file and daemon-reloads.
// Errors are intentionally ignored — this is cleanup only.
func cleanupDaemonService(t *testing.T, paths models.WeaverPaths) {
	t.Helper()
	ctx := context.Background()
	_ = osx.StopService(ctx, daemonServiceName)
	_ = osx.DisableService(ctx, daemonServiceName)
	removeDaemonServiceFiles(paths.DaemonServiceUnitPath, paths.DaemonServiceLegacyUnitPath)
	_ = osx.DaemonReload(ctx)
}

// Test_DaemonService_FilePlacement_Integration verifies that
// installDaemonServiceFiles writes a regular unit file under
// /usr/lib/systemd/system, and that removeDaemonServiceFiles tears it down.
func Test_DaemonService_FilePlacement_Integration(t *testing.T) {
	requireRoot(t)

	paths := daemonTestPaths(t)
	cleanupDaemonService(t, paths)
	t.Cleanup(func() { cleanupDaemonService(t, paths) })

	// Install files
	err := installDaemonServiceFiles(paths.DaemonServiceUnitPath, paths.DaemonServiceLegacyUnitPath, nil)
	require.NoError(t, err)

	fi, err := os.Lstat(paths.DaemonServiceUnitPath)
	require.NoError(t, err, "unit file should exist")
	assert.Greater(t, fi.Size(), int64(0))
	assert.Zero(t, fi.Mode()&os.ModeSymlink, "unit must be a regular file, not a symlink into the sandbox")

	removeDaemonServiceFiles(paths.DaemonServiceUnitPath, paths.DaemonServiceLegacyUnitPath)

	_, err = os.Lstat(paths.DaemonServiceUnitPath)
	assert.True(t, os.IsNotExist(err), "unit file should be gone after removal")
}

// Test_DaemonService_ReplacesLegacySandboxUnit_Integration covers the upgrade
// path: a host provisioned earlier carries a symlink into the sandbox, whose
// target a later cluster install destroys. Writing through that dangling link
// would recreate the file in the sandbox instead of installing the unit.
func Test_DaemonService_ReplacesLegacySandboxUnit_Integration(t *testing.T) {
	requireRoot(t)

	paths := daemonTestPaths(t)
	cleanupDaemonService(t, paths)
	t.Cleanup(func() { cleanupDaemonService(t, paths) })

	require.NoError(t, os.MkdirAll(filepath.Dir(paths.DaemonServiceLegacyUnitPath), 0o755))
	require.NoError(t, os.WriteFile(paths.DaemonServiceLegacyUnitPath, []byte("[Unit]\n"), 0o644))
	_ = os.Remove(paths.DaemonServiceUnitPath)
	require.NoError(t, os.Symlink(paths.DaemonServiceLegacyUnitPath, paths.DaemonServiceUnitPath))

	// The sandbox rebuild that orphaned the unit.
	require.NoError(t, os.Remove(paths.DaemonServiceLegacyUnitPath))
	_, statErr := os.Stat(paths.DaemonServiceUnitPath)
	require.True(t, os.IsNotExist(statErr), "precondition: the symlink must be dangling")

	require.NoError(t, installDaemonServiceFiles(paths.DaemonServiceUnitPath, paths.DaemonServiceLegacyUnitPath, nil))

	fi, err := os.Lstat(paths.DaemonServiceUnitPath)
	require.NoError(t, err)
	assert.Zero(t, fi.Mode()&os.ModeSymlink, "the dangling symlink must be replaced by a regular file")
	assert.Greater(t, fi.Size(), int64(0))

	_, err = os.Lstat(paths.DaemonServiceLegacyUnitPath)
	assert.True(t, os.IsNotExist(err), "the legacy sandbox copy must be cleared")
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
	require.NoError(t, installDaemonServiceFiles(paths.DaemonServiceUnitPath, paths.DaemonServiceLegacyUnitPath, nil))
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
	require.NoError(t, installDaemonServiceFiles(paths.DaemonServiceUnitPath, paths.DaemonServiceLegacyUnitPath, nil))
	require.NoError(t, osx.DaemonReload(ctx))
	require.NoError(t, osx.EnableService(ctx, daemonServiceName))

	// Build and run the rollback
	step, err := InstallDaemonServiceStep(paths, nil).Build()
	require.NoError(t, err)

	rollbackReport := step.Rollback(ctx)
	require.NotNil(t, rollbackReport)
	assert.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	_, errUnit := os.Lstat(paths.DaemonServiceUnitPath)
	assert.True(t, os.IsNotExist(errUnit), "unit file should be removed by rollback")

	// Service should be disabled
	enabled, err := osx.IsServiceEnabled(ctx, daemonServiceName)
	require.NoError(t, err)
	assert.False(t, enabled, "service should be disabled by rollback")
}

// Test_RemoveDaemonServiceStep_Integration verifies that RemoveDaemonServiceStep
// removes the unit file and disables the service.
func Test_RemoveDaemonServiceStep_Integration(t *testing.T) {
	requireRoot(t)

	paths := daemonTestPaths(t)
	cleanupDaemonService(t, paths)
	t.Cleanup(func() { cleanupDaemonService(t, paths) })

	ctx := context.Background()

	// Simulate an installed (but not running) service
	require.NoError(t, installDaemonServiceFiles(paths.DaemonServiceUnitPath, paths.DaemonServiceLegacyUnitPath, nil))
	require.NoError(t, osx.DaemonReload(ctx))
	require.NoError(t, osx.EnableService(ctx, daemonServiceName))

	// Run remove step
	step, err := RemoveDaemonServiceStep(paths).Build()
	require.NoError(t, err)

	report := step.Execute(ctx)
	require.NotNil(t, report)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	assert.NoError(t, report.Error)

	_, errUnit := os.Lstat(paths.DaemonServiceUnitPath)
	assert.True(t, os.IsNotExist(errUnit), "unit file should be removed")

	// Service disabled
	enabled, err := osx.IsServiceEnabled(ctx, daemonServiceName)
	require.NoError(t, err)
	assert.False(t, enabled, "service should be disabled after uninstall")
}
