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
	"golang.hedera.com/solo-weaver/pkg/software"
)

func Test_StepCrio_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	reset(t)

	//
	// When
	//
	step, err := SetupCrio().Build()

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

func Test_StepCrio_AlreadyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	reset(t)

	step, err := SetupCrio().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When
	//
	step, err = SetupCrio().Build()

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

func Test_StepCrio_PartiallyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	reset(t)

	step, err := SetupCrio().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	err = os.RemoveAll(core.Paths().TempDir)
	require.NoError(t, err)

	//
	// When
	//
	step, err = SetupCrio().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())

	//
	// Then
	//
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

func Test_StepCrio_Rollback_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	reset(t)

	//
	// When
	//
	step, err := SetupCrio().Build()

	require.NoError(t, err)
	report := step.Execute(context.Background())

	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	require.Equal(t, "true", report.StepReports[0].Metadata[DownloadedByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[InstalledByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[CleanedUpByThisStep])

	//
	// When - Rollback
	//
	rollbackReport := step.Rollback(context.Background())

	//
	// Then
	//
	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Verify download folder for crio is removed
	_, err = os.Stat("/opt/weaver/tmp/crio")
	require.Error(t, err)

	// Verify binary files are removed (from Uninstall method)
	_, err = os.Stat("/opt/weaver/sandbox/usr/local/bin/crio")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/local/bin/pinns")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/local/bin/crictl")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/libexec/crio/conmon")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/libexec/crio/conmonrs")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/libexec/crio/crun")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/libexec/crio/runc")
	require.Error(t, err)

	// Verify CNI plugins directory is removed
	_, err = os.Stat("/opt/weaver/sandbox/opt/cni/bin")
	require.Error(t, err)

	// Verify configuration files are removed (from Uninstall method)
	_, err = os.Stat("/opt/weaver/sandbox/etc/cni/net.d/10-crio-bridge.conflist.disabled")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/etc/crictl.yaml")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/share/oci-umount/oci-umount.d/crio-umount.conf")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/etc/crio/policy.json")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/etc/crio/crio.conf.d/10-crio.conf")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/local/share/man/man5/crio.conf.5")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/local/share/man/man5/crio.conf.d.5")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/local/share/man/man8/crio.8")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/local/share/bash-completion/completions/crio")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/local/share/fish/completions/crio.fish")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/local/share/zsh/site-functions/_crio")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/usr/lib/systemd/system/crio.service")
	require.Error(t, err)
	_, err = os.Stat("/opt/weaver/sandbox/etc/containers/registries.conf.d/registries.conf")
	require.Error(t, err)

	// Verify symlinks and configuration files removed (from RemoveConfiguration method)
	_, err = os.Stat("/usr/lib/systemd/system/crio.service")
	require.Error(t, err)
	_, err = os.Stat("/etc/containers")
	require.Error(t, err)

	// Check that .crio-install file is removed
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)
	_, err = os.Stat(path.Join(homeDir, ".crio-install"))
	require.Error(t, err)
}

func Test_StepCrio_Rollback_Setup_DownloadFailed(t *testing.T) {
	//
	// Given
	//
	reset(t)

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
	step, err := SetupCrio().Build()
	require.NoError(t, err)

	//
	// When - Execute (should fail at download)
	//

	report := step.Execute(context.Background())

	require.NotNil(t, report)

	// Check errorx error type
	require.Error(t, report.Error)

	// Confirm errorx error type is DownloadError
	require.True(t, errorx.IsOfType(errorx.Cast(report.Error).Cause(), software.DownloadError))
	require.Equal(t, automa.StatusFailed, report.Status)

	//
	// Then
	//
	rollbackReport := report.StepReports[0].Rollback

	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSkipped, rollbackReport.Status)

	// Verify download folder for crio was not created
	_, err = os.Stat("/opt/weaver/tmp/crio")
	require.Error(t, err)

	// Confirm binary files were not created
	_, err = os.Stat("/opt/weaver/sandbox/bin/crio")
	require.Error(t, err)
}

func Test_StepCrio_Rollback_Setup_ExtractFailed(t *testing.T) {
	//
	// Given
	//
	reset(t)

	// First, let the download succeed, then make the extraction fail
	// by making the unpack directory read-only after download
	step, err := SetupCrio().Build()
	require.NoError(t, err)

	// Download first to create the download folder
	installer, err := software.NewCrioInstaller()
	require.NoError(t, err)

	err = installer.Download()
	require.NoError(t, err)

	// Now make the download folder read-only to prevent extraction
	downloadDir := path.Join(core.Paths().TempDir, "cri-o")
	cmd := exec.Command("chattr", "+i", downloadDir)
	err = cmd.Run()
	require.NoError(t, err, "Failed to make download directory read-only")

	// Restore permissions after test
	t.Cleanup(func() {
		_ = exec.Command("chattr", "-i", downloadDir).Run()
	})

	//
	// When - Execute (should fail at extraction)
	//
	report := step.Execute(context.Background())

	require.NotNil(t, report)

	// Check errorx error type
	require.Error(t, report.Error)

	// Confirm errorx error type is ExtractionError
	require.True(t, errorx.IsOfType(errorx.Cast(report.Error).Cause(), software.ExtractionError))
	require.Equal(t, automa.StatusFailed, report.Status)

	//
	// Then
	//
	rollbackReport := report.StepReports[0].Rollback

	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSkipped, rollbackReport.Status)

	// Verify download folder is still around when there is an extraction error
	_, err = os.Stat("/opt/weaver/tmp/cri-o")
	require.NoError(t, err)

	// Check there are downloaded files in the crio directory
	files, err := os.ReadDir(path.Join(core.Paths().TempDir, "cri-o"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1, "Expected at least 1 file in the crio directory")

	// Verify binary files were not installed (since extraction failed)
	_, err = os.Stat("/opt/weaver/sandbox/usr/local/bin/crio")
	require.Error(t, err)
}

func Test_StepCrio_Rollback_Setup_InstallFailed(t *testing.T) {
	//
	// Given
	//
	reset(t)

	// Make subfolder in sandbox directory read-only
	sandboxSubFolder := path.Join(core.Paths().SandboxDir, "opt")

	err := os.MkdirAll(sandboxSubFolder, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create sandbox bin directory")
	cmd := exec.Command("chattr", "+i", sandboxSubFolder)
	err = cmd.Run()
	require.NoError(t, err, "Failed to make sandbox binary directory read-only")

	// Restore permissions after test
	t.Cleanup(func() {
		_ = exec.Command("chattr", "-i", sandboxSubFolder).Run()
	})

	//
	// When
	//
	step, err := SetupCrio().Build()
	require.NoError(t, err)

	//
	// When - Execute (should fail at install)
	//

	report := step.Execute(context.Background())

	require.NotNil(t, report)

	// Check errorx error type
	require.Error(t, report.Error)

	// Confirm errorx error type is InstallationError
	require.True(t, errorx.IsOfType(errorx.Cast(report.Error).Cause(), software.InstallationError))
	require.Equal(t, automa.StatusFailed, report.Status)

	//
	// Then
	//
	rollbackReport := report.StepReports[0].Rollback

	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSkipped, rollbackReport.Status)

	// Verify download folder is still around when there is an installation error
	_, err = os.Stat("/opt/weaver/tmp/cri-o")
	require.NoError(t, err)

	// Check there are downloaded files in the crio directory
	files, err := os.ReadDir(path.Join(core.Paths().TempDir, "cri-o"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1, "Expected at least 1 file in the crio directory")

	// Verify binary files were not installed
	_, err = os.Stat("/opt/weaver/sandbox/bin/crio")
	require.Error(t, err)
}

func Test_StepCrio_Rollback_Setup_CleanupFailed(t *testing.T) {
	//
	// Given
	//
	reset(t)

	// Create an unremovable directory under download folder
	unremovableDir := path.Join(core.Paths().TempDir, "cri-o", "unremovable")

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
	step, err := SetupCrio().Build()
	require.NoError(t, err)

	//
	// When - Execute (should fail at cleanup)
	//

	report := step.Execute(context.Background())

	require.NotNil(t, report)

	// Check errorx error type
	require.Error(t, report.Error)

	// Confirm errorx error type is CleanupError
	require.True(t, errorx.IsOfType(errorx.Cast(report.Error).Cause(), software.CleanupError))
	require.Equal(t, automa.StatusFailed, report.Status)

	//
	// Then
	//
	rollbackReport := report.StepReports[0].Rollback

	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Verify download folder is still around when there is a cleanup error
	_, err = os.Stat("/opt/weaver/tmp/cri-o")
	require.NoError(t, err)

	// Check there are files in the tmp/crio directory
	files, err := os.ReadDir(path.Join(core.Paths().TempDir, "cri-o"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1, "Expected at least 1 file in the tmp/crio directory")

	// Verify binary files were removed
	_, err = os.Stat("/opt/weaver/sandbox/bin/crio")
	require.Error(t, err)
}

func Test_StepCrio_Rollback_ConfigurationFailed(t *testing.T) {
	//
	// Given
	//
	reset(t)

	// Make the /etc/containers directory read-only to prevent configuration
	etcCrioDir := "/etc/containers"
	err := os.MkdirAll(etcCrioDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create /etc/containers directory")
	cmd := exec.Command("chattr", "+i", etcCrioDir)
	err = cmd.Run()
	require.NoError(t, err, "Failed to make /etc/containers directory read-only")

	// Restore permissions after test
	t.Cleanup(func() {
		_ = exec.Command("chattr", "-i", etcCrioDir).Run()
	})

	//
	// When
	//
	step, err := SetupCrio().Build()
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
	_, err = os.Stat("/opt/weaver/tmp/cri-o")
	require.Error(t, err)

	// Verify binary files were removed from sandbox
	_, err = os.Stat("/opt/weaver/sandbox/bin/crio")
	require.Error(t, err)

	// Verify configuration was not applied
	_, err = os.Stat("/usr/lib/systemd/system/crio.service")
	require.Error(t, err)
}

func Test_StepCrio_ServiceConfiguration_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	reset(t)

	//
	// When
	//
	step, err := SetupCrio().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())

	//
	// Then
	//
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Verify crio.service was installed in sandbox
	_, err = os.Stat("/opt/weaver/sandbox/usr/lib/systemd/system/crio.service")
	require.NoError(t, err, "crio.service should exist in sandbox")

	// Verify .latest file was created with modified content
	_, err = os.Stat("/opt/weaver/sandbox/usr/lib/systemd/system/crio.service.latest")
	require.NoError(t, err, "crio.service.latest should exist")

	// Verify systemd symlink was created
	linkTarget, err := os.Readlink("/usr/lib/systemd/system/crio.service")
	require.NoError(t, err, "crio.service symlink should exist")
	require.Equal(t, "/opt/weaver/sandbox/usr/lib/systemd/system/crio.service.latest", linkTarget, "symlink should point to .latest file")

	// Verify .latest file contains sandbox binary path
	content, err := os.ReadFile("/opt/weaver/sandbox/usr/lib/systemd/system/crio.service.latest")
	require.NoError(t, err, "should be able to read .latest file")
	contentStr := string(content)
	require.Contains(t, contentStr, "/opt/weaver/sandbox/usr/local/bin/crio", ".latest file should contain sandbox crio path")
}

func Test_StepCrio_ServiceConfiguration_AlreadyConfigured_Integration(t *testing.T) {
	//
	// Given
	//
	reset(t)

	// First run to configure crio
	step, err := SetupCrio().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	//
	// When - Run again
	//
	step, err = SetupCrio().Build()
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
	_, err = os.Stat("/opt/weaver/sandbox/usr/lib/systemd/system/crio.service.latest")
	require.NoError(t, err, "crio.service.latest should still exist")

	linkTarget, err := os.Readlink("/usr/lib/systemd/system/crio.service")
	require.NoError(t, err, "crio.service symlink should still exist")
	require.Equal(t, "/opt/weaver/sandbox/usr/lib/systemd/system/crio.service.latest", linkTarget)
}

func Test_StepCrio_ServiceConfiguration_PartiallyConfigured_Integration(t *testing.T) {
	//
	// Given
	//
	reset(t)

	// First run to install and configure crio
	step, err := SetupCrio().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	// Remove the systemd symlink but keep the .latest file
	err = os.RemoveAll("/usr/lib/systemd/system/crio.service")
	require.NoError(t, err)

	//
	// When - Run again
	//
	step, err = SetupCrio().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())

	//
	// Then
	//
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Installation should be skipped (already installed)
	require.Equal(t, automa.StatusSkipped, report.StepReports[0].Status)
	require.Equal(t, "true", report.StepReports[0].Metadata[AlreadyInstalled])

	// Configuration should run again (partial configuration)
	require.Equal(t, automa.StatusSuccess, report.StepReports[1].Status)
	require.Empty(t, report.StepReports[1].Metadata[AlreadyConfigured])
	require.Equal(t, "true", report.StepReports[1].Metadata[ConfiguredByThisStep])

	// Verify systemd symlink was recreated
	linkTarget, err := os.Readlink("/usr/lib/systemd/system/crio.service")
	require.NoError(t, err, "crio.service symlink should be recreated")
	require.Equal(t, "/opt/weaver/sandbox/usr/lib/systemd/system/crio.service.latest", linkTarget)
}

func Test_StepCrio_ServiceConfiguration_CorruptedLatestFile_Integration(t *testing.T) {
	//
	// Given
	//
	reset(t)

	// First run to install and configure crio
	step, err := SetupCrio().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	// Corrupt the .latest file by writing incorrect content
	corruptedContent := "This is corrupted content"
	err = os.WriteFile("/opt/weaver/sandbox/usr/lib/systemd/system/crio.service.latest", []byte(corruptedContent), core.DefaultFilePerm)
	require.NoError(t, err)

	//
	// When - Run again
	//
	step, err = SetupCrio().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())

	//
	// Then
	//
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Installation should be skipped (already installed)
	require.Equal(t, automa.StatusSkipped, report.StepReports[0].Status)

	// Configuration should run again (corrupted .latest file detected)
	require.Equal(t, automa.StatusSuccess, report.StepReports[1].Status)
	require.Empty(t, report.StepReports[1].Metadata[AlreadyConfigured])
	require.Equal(t, "true", report.StepReports[1].Metadata[ConfiguredByThisStep])

	// Verify .latest file was fixed
	content, err := os.ReadFile("/opt/weaver/sandbox/usr/lib/systemd/system/crio.service.latest")
	require.NoError(t, err)
	contentStr := string(content)
	require.Contains(t, contentStr, "/opt/weaver/sandbox/usr/local/bin/crio", ".latest file should contain correct sandbox path")
	require.NotEqual(t, corruptedContent, contentStr, ".latest file should be fixed")
}

func Test_StepCrio_ServiceConfiguration_RestoreConfiguration_Integration(t *testing.T) {
	//
	// Given
	//
	reset(t)

	// Install and configure crio
	step, err := SetupCrio().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	// Verify configuration is in place
	_, err = os.Stat("/opt/weaver/sandbox/usr/lib/systemd/system/crio.service.latest")
	require.NoError(t, err, "crio.service.latest should exist before restoration")

	_, err = os.Stat("/usr/lib/systemd/system/crio.service")
	require.NoError(t, err, "crio.service symlink should exist before restoration")

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
	_, err = os.Stat("/opt/weaver/sandbox/usr/lib/systemd/system/crio.service.latest")
	require.Error(t, err, "crio.service.latest should be removed after rollback")

	_, err = os.Stat("/usr/lib/systemd/system/crio.service")
	require.Error(t, err, "crio.service symlink should be removed after rollback")

	// Verify installation was also rolled back
	_, err = os.Stat("/opt/weaver/sandbox/bin/crio")
	require.Error(t, err, "crio binary should be removed after rollback")

	_, err = os.Stat("/opt/weaver/tmp/crio")
	require.Error(t, err, "crio temp directory should be removed after rollback")
}
