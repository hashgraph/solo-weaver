// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"context"
	"os"
	"os/exec"
	"path"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/testutil"
	"golang.hedera.com/solo-weaver/pkg/software"
)

func Test_StepKubelet_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	//
	// When
	//
	step, err := SetupKubelet().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	require.Empty(t, report.StepReports[0].Metadata[AlreadyInstalled])
	require.Equal(t, "true", report.StepReports[0].Metadata[DownloadedByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[InstalledByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[CleanedUpByThisStep])
	require.Empty(t, report.StepReports[1].Metadata[AlreadyConfigured])
	require.Equal(t, "true", report.StepReports[1].Metadata[ConfiguredByThisStep])
}

func Test_StepKubelet_AlreadyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	step, err := SetupKubelet().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When
	//
	step, err = SetupKubelet().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	require.Equal(t, automa.StatusSkipped, report.StepReports[0].Status)
	require.Equal(t, "true", report.StepReports[0].Metadata[AlreadyInstalled])
	require.Empty(t, report.StepReports[0].Metadata[DownloadedByThisStep])
	require.Empty(t, report.StepReports[0].Metadata[InstalledByThisStep])
	require.Empty(t, report.StepReports[0].Metadata[CleanedUpByThisStep])

	require.Equal(t, automa.StatusSkipped, report.StepReports[1].Status)
	require.Equal(t, "true", report.StepReports[1].Metadata[AlreadyConfigured])
	require.Empty(t, report.StepReports[1].Metadata[ConfiguredByThisStep])
}

func Test_StepKubelet_Rollback_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	//
	// When
	//
	step, err := SetupKubelet().Build()

	require.NoError(t, err)
	report := step.Execute(context.Background())

	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	require.Equal(t, "true", report.StepReports[0].Metadata[DownloadedByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[InstalledByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[CleanedUpByThisStep])
	require.Equal(t, "true", report.StepReports[1].Metadata[ConfiguredByThisStep])

	//
	// When - Rollback
	//
	rollbackReport := step.Rollback(context.Background())

	//
	// Then
	//
	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Verify download folder for kubelet is removed
	_, err = os.Stat("/opt/solo/weaver/tmp/kubelet")
	require.Error(t, err)

	// Verify binary files are removed
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubelet")
	require.Error(t, err)
}

func Test_StepKubelet_Rollback_Setup_DownloadFailed(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	// Make the download directory read-only
	err := os.MkdirAll(core.Paths().TempDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create download directory")
	cmd := exec.Command("chattr", "+i", core.Paths().TempDir)
	err = cmd.Run()
	require.NoError(t, err, "Failed to make download directory read-only")

	// Restore permissions after test
	t.Cleanup(func() {
		_ = exec.Command("chattr", "-i", core.Paths().TempDir).Run()
	})

	//
	// When
	//
	step, err := SetupKubelet().Build()
	require.NoError(t, err)

	//
	// When - Execute (should fail at download step)
	//

	report := step.Execute(context.Background())

	require.NotNil(t, report)

	// Check errorx error type
	require.Error(t, report.Error)

	// Confirm errorx error type is DownloadError
	require.True(t, errorx.IsOfType(errorx.Cast(report.StepReports[0].Error), software.DownloadError))
	require.Equal(t, automa.StatusFailed, report.Status)

	//
	// Then
	//
	rollbackReport := report.StepReports[0].Rollback

	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSkipped, rollbackReport.Status)

	// Verify download folder for kubelet was not created
	_, err = os.Stat("/opt/solo/weaver/tmp/kubelet")
	require.Error(t, err)

	// Confirm binary files were not created
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubelet")
	require.Error(t, err)
}

func Test_StepKubelet_Rollback_Setup_InstallFailed(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	// Make the sandbox directory read-only
	sandboxDir := path.Join(core.Paths().SandboxDir, "bin")

	err := os.MkdirAll(sandboxDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create sandbox bin directory")
	cmd := exec.Command("chattr", "+i", sandboxDir)
	err = cmd.Run()
	require.NoError(t, err, "Failed to make sandbox binary directory read-only")

	// Restore permissions after test
	t.Cleanup(func() {
		_ = exec.Command("chattr", "-i", sandboxDir).Run()
	})

	//
	// When
	//
	step, err := SetupKubelet().Build()
	require.NoError(t, err)

	//
	// When - Rollback
	//

	report := step.Execute(context.Background())

	require.NotNil(t, report)

	// Check errorx error type
	require.Error(t, report.Error)

	// Confirm errorx error type is InstallationError
	require.True(t, errorx.IsOfType(errorx.Cast(report.StepReports[0].Error), software.InstallationError))
	require.Equal(t, automa.StatusFailed, report.Status)

	//
	// Then
	//
	rollbackReport := report.StepReports[0].Rollback

	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSkipped, rollbackReport.Status)

	// Verify download folder is still around when there is an installation error
	_, err = os.Stat("/opt/solo/weaver/tmp/kubelet")
	require.NoError(t, err)

	// Check there are downloaded files in the kubelet directory
	files, err := os.ReadDir(path.Join(core.Paths().TempDir, "kubelet"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1, "Expected at least 1 file in the kubelet directory")

	// Verify binary files were not installed
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubelet")
	require.Error(t, err)
}

func Test_StepKubelet_Rollback_Setup_CleanupFailed(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	// Create an unremovable directory under download folder
	unremovableDir := path.Join(core.Paths().TempDir, "kubelet", "unremovable")

	err := os.MkdirAll(unremovableDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create unremovable directory")
	cmd := exec.Command("chattr", "+i", unremovableDir)
	err = cmd.Run()
	require.NoError(t, err, "Failed to make unremovable directory read-only")

	// Restore permissions after test
	t.Cleanup(func() {
		_ = exec.Command("chattr", "-i", unremovableDir).Run()
	})

	//
	// When
	//
	step, err := SetupKubelet().Build()
	require.NoError(t, err)

	//
	// When - Rollback
	//

	report := step.Execute(context.Background())

	require.NotNil(t, report)

	// Check errorx error type
	require.Error(t, report.Error)

	// Confirm errorx error type is CleanupError
	require.True(t, errorx.IsOfType(errorx.Cast(report.StepReports[0].Error), software.CleanupError))
	require.Equal(t, automa.StatusFailed, report.Status)

	//
	// Then
	//
	rollbackReport := report.StepReports[0].Rollback

	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Verify download folder is still around when there is a cleanup error
	_, err = os.Stat("/opt/solo/weaver/tmp/kubelet")
	require.NoError(t, err)

	// Check there are files in the tmp/kubelet directory
	files, err := os.ReadDir(path.Join(core.Paths().TempDir, "kubelet"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1, "Expected at least 1 file in the tmp/kubelet directory")

	// Verify binary files were removed
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubelet")
	require.Error(t, err)
}

func Test_StepKubelet_Rollback_ConfigurationFailed(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	// Make the /usr/local/bin directory read-only to prevent configuration
	usrLocalBinDir := "/usr/local/bin"
	err := os.MkdirAll(usrLocalBinDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create /usr/local/bin directory")
	cmd := exec.Command("chattr", "+i", usrLocalBinDir)
	err = cmd.Run()
	require.NoError(t, err, "Failed to make /usr/local/bin directory read-only")

	// Restore permissions after test
	t.Cleanup(func() {
		_ = exec.Command("chattr", "-i", usrLocalBinDir).Run()
	})

	//
	// When
	//
	step, err := SetupKubelet().Build()
	require.NoError(t, err)

	//
	// When - Execute (should fail at configuration step)
	//

	report := step.Execute(context.Background())

	require.NotNil(t, report)

	// Check that it failed
	require.Error(t, report.Error)
	require.Equal(t, automa.StatusFailed, report.Status)

	// First step (install) should succeed
	require.Equal(t, automa.StatusSuccess, report.StepReports[0].Status)
	require.Equal(t, "true", report.StepReports[0].Metadata[DownloadedByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[InstalledByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[CleanedUpByThisStep])

	// Second step (configure) should fail
	require.Equal(t, automa.StatusFailed, report.StepReports[1].Status)

	//
	// Then - Verify rollback
	//
	installRollbackReport := report.StepReports[0].Rollback
	configRollbackReport := report.StepReports[1].Rollback

	require.NoError(t, installRollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, installRollbackReport.Status)

	require.NoError(t, configRollbackReport.Error)
	require.Equal(t, automa.StatusSkipped, configRollbackReport.Status)

	// Verify installation was rolled back - download folder should be removed
	_, err = os.Stat("/opt/solo/weaver/tmp/kubelet")
	require.Error(t, err)

	// Verify binary files were removed from sandbox
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubelet")
	require.Error(t, err)

	// Verify configuration was not applied - symlinks should not exist
	_, err = os.Stat("/usr/local/bin/kubelet")
	require.Error(t, err)
}

func Test_StepKubelet_ServiceConfiguration_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	//
	// When
	//
	step, err := SetupKubelet().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())

	//
	// Then
	//
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Verify kubelet.service was installed in sandbox
	_, err = os.Stat("/opt/solo/weaver/sandbox/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err, "kubelet.service should exist in sandbox")

	// Verify file in sandbox directory was created with modified content
	_, err = os.Stat("/opt/solo/weaver/sandbox/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err, "kubelet.service should exist in sandbox directory")

	// Verify systemd symlink was created
	linkTarget, err := os.Readlink("/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err, "kubelet.service symlink should exist")
	require.Equal(t, "/opt/solo/weaver/sandbox/usr/lib/systemd/system/kubelet.service", linkTarget, "symlink should point to file in sandbox directory")

	// Verify file in sandbox directory contains sandbox binary path
	content, err := os.ReadFile("/opt/solo/weaver/sandbox/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err, "should be able to read file in sandbox directory")
	contentStr := string(content)
	require.Contains(t, contentStr, "/opt/solo/weaver/sandbox/bin/kubelet", "file in sandbox directory should contain sandbox kubelet path")
	require.NotContains(t, contentStr, "/usr/bin/kubelet", "file in sandbox directory should not contain original kubelet path")
}

func Test_StepKubelet_ServiceConfiguration_AlreadyConfigured_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	// First run to configure kubelet
	step, err := SetupKubelet().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	//
	// When - Run again
	//
	step, err = SetupKubelet().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())

	//
	// Then
	//
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Configuration step should be skipped
	require.Equal(t, automa.StatusSkipped, report.StepReports[1].Status)
	require.Equal(t, "true", report.StepReports[1].Metadata[AlreadyConfigured])
	require.Empty(t, report.StepReports[1].Metadata[ConfiguredByThisStep])

	// Verify service configuration still exists and is valid
	_, err = os.Stat("/opt/solo/weaver/sandbox/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err, "kubelet.service should still exist in sandbox directory")

	linkTarget, err := os.Readlink("/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err, "kubelet.service symlink should still exist")
	require.Equal(t, "/opt/solo/weaver/sandbox/usr/lib/systemd/system/kubelet.service", linkTarget)
}

func Test_StepKubelet_ServiceConfiguration_RestoreConfiguration_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	// Install and configure kubelet
	step, err := SetupKubelet().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	// Verify configuration is in place
	_, err = os.Stat("/opt/solo/weaver/sandbox/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err, "kubelet.service should exist in sandbox directory before restoration")

	_, err = os.Stat("/usr/lib/systemd/system/kubelet.service")
	require.NoError(t, err, "kubelet.service symlink should exist before restoration")

	//
	// When - Rollback
	//
	rollbackReport := step.Rollback(context.Background())

	//
	// Then
	//
	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Verify configuration was restored (removed)
	_, err = os.Stat("/opt/solo/weaver/sandbox/usr/lib/systemd/system/kubelet.service")
	require.Error(t, err, "latest kubelet.service file should be removed after rollback")

	_, err = os.Stat("/usr/lib/systemd/system/kubelet.service")
	require.Error(t, err, "kubelet.service symlink should be removed after rollback")

	// Verify installation was also rolled back
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubelet")
	require.Error(t, err, "kubelet binary should be removed after rollback")

	_, err = os.Stat("/opt/solo/weaver/tmp/kubelet")
	require.Error(t, err, "kubelet temp directory should be removed after rollback")
}
