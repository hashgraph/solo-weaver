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
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/software"
)

func Test_StepKubeadm_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	//
	// When
	//
	step, err := SetupKubeadm().Build()

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

func Test_StepKubeadm_AlreadyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	step, err := SetupKubeadm().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When
	//
	step, err = SetupKubeadm().Build()

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

func Test_StepKubeadm_PartiallyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	step, err := SetupKubeadm().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	err = os.RemoveAll("/usr/local/bin/kubeadm")
	require.NoError(t, err)

	//
	// When
	//
	step, err = SetupKubeadm().Build()
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

	require.Equal(t, automa.StatusSuccess, report.StepReports[1].Status)
	require.Empty(t, report.StepReports[1].Metadata[AlreadyConfigured])
	require.Equal(t, "true", report.StepReports[1].Metadata[ConfiguredByThisStep])
}

func Test_StepKubeadm_Rollback_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	//
	// When
	//
	step, err := SetupKubeadm().Build()

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

	// Verify download folder for kubeadm is removed
	_, err = os.Stat("/opt/provisioner/tmp/kubeadm")
	require.Error(t, err)

	// Verify binary files are removed
	_, err = os.Stat("/opt/provisioner/sandbox/bin/kubeadm")
	require.Error(t, err)
}

func Test_StepKubeadm_Rollback_Setup_DownloadFailed(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

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
	step, err := SetupKubeadm().Build()
	require.NoError(t, err)

	//
	// When - Execute (should fail at download step)
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

	// Verify download folder for kubeadm was not created
	_, err = os.Stat("/opt/provisioner/tmp/kubeadm")
	require.Error(t, err)

	// Confirm binary files were not created
	_, err = os.Stat("/opt/provisioner/sandbox/bin/kubeadm")
	require.Error(t, err)
}

func Test_StepKubeadm_Rollback_Setup_InstallFailed(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

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
	step, err := SetupKubeadm().Build()
	require.NoError(t, err)

	//
	// When - Execute (should fail at install step)
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
	_, err = os.Stat("/opt/provisioner/tmp/kubeadm")
	require.NoError(t, err)

	// Check there are downloaded files in the kubeadm directory
	files, err := os.ReadDir(path.Join(core.Paths().TempDir, "kubeadm"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1, "Expected at least 1 file in the kubeadm directory")

	// Verify binary files were not installed
	_, err = os.Stat("/opt/provisioner/sandbox/bin/kubeadm")
	require.Error(t, err)
}

func Test_StepKubeadm_Rollback_Setup_CleanupFailed(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	// Create an unremovable directory under download folder
	unremovableDir := path.Join(core.Paths().TempDir, "kubeadm", "unremovable")

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
	step, err := SetupKubeadm().Build()
	require.NoError(t, err)

	//
	// When - Execute (should fail at cleanup step)
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
	_, err = os.Stat("/opt/provisioner/tmp/kubeadm")
	require.NoError(t, err)

	// Check there are files in the tmp/kubeadm directory
	files, err := os.ReadDir(path.Join(core.Paths().TempDir, "kubeadm"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1, "Expected at least 1 file in the tmp/kubeadm directory")

	// Verify binary files were removed
	_, err = os.Stat("/opt/provisioner/sandbox/bin/kubeadm")
	require.Error(t, err)
}

func Test_StepKubeadm_Rollback_ConfigurationFailed(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

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
	step, err := SetupKubeadm().Build()
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
	_, err = os.Stat("/opt/provisioner/tmp/kubeadm")
	require.Error(t, err)

	// Verify binary files were removed from sandbox
	_, err = os.Stat("/opt/provisioner/sandbox/bin/kubeadm")
	require.Error(t, err)

	// Verify configuration was not applied - symlinks should not exist
	_, err = os.Stat("/usr/local/bin/kubeadm")
	require.Error(t, err)
}

func Test_StepKubeadm_ServiceConfiguration_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	//
	// When
	//
	step, err := SetupKubeadm().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())

	//
	// Then
	//
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Verify kubelet service directory configuration was installed in sandbox
	_, err = os.Stat("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err, "10-kubeadm.conf should exist in sandbox")

	// Verify .latest 10-kubeadm.conf file was created with updated paths
	_, err = os.Stat("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf.latest")
	require.NoError(t, err, "10-kubeadm.conf.latest should exist in sandbox")

	// Verify systemd symlink was created for the 10-kubeadm.conf file
	_, err = os.Stat("/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err, "10-kubeadm.conf symlink should exist in system directory")

	// Verify it's actually a symlink pointing to the .latest file
	linkTarget, err := os.Readlink("/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err, "10-kubeadm.conf should be a symlink")
	require.Equal(t, "/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf.latest", linkTarget, "symlink should point to .latest file")

	// Verify kubeadm-init.yaml was created
	_, err = os.Stat("/opt/provisioner/sandbox/etc/provisioner/kubeadm-init.yaml")
	require.NoError(t, err, "kubeadm-init.yaml should exist")

	// Verify 10-kubeadm.conf.latest contains sandbox binary path
	content, err := os.ReadFile("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf.latest")
	require.NoError(t, err, "should be able to read 10-kubeadm.conf.latest file")
	contentStr := string(content)
	require.Contains(t, contentStr, "/opt/provisioner/sandbox/bin/kubelet", "10-kubeadm.conf.latest should contain sandbox kubelet path")
	require.NotContains(t, contentStr, "/usr/bin/kubelet", "10-kubeadm.conf.latest should not contain original kubelet path")
}

func Test_StepKubeadm_ServiceConfiguration_AlreadyConfigured_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	// First run to configure kubeadm
	step, err := SetupKubeadm().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	//
	// When - Run again
	//
	step, err = SetupKubeadm().Build()
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
	_, err = os.Stat("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err, "10-kubeadm.conf should still exist")

	_, err = os.Stat("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf.latest")
	require.NoError(t, err, "10-kubeadm.conf.latest should still exist")

	linkTarget, err := os.Readlink("/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err, "10-kubeadm.conf symlink should still exist")
	require.Equal(t, "/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf.latest", linkTarget)

	_, err = os.Stat("/opt/provisioner/sandbox/etc/provisioner/kubeadm-init.yaml")
	require.NoError(t, err, "kubeadm-init.yaml should still exist")
}

func Test_StepKubeadm_ServiceConfiguration_PartiallyConfigured_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	// First run to install and configure kubeadm
	step, err := SetupKubeadm().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	// Remove the 10-kubeadm.conf symlink but keep the configuration files
	err = os.RemoveAll("/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err)

	//
	// When - Run again
	//
	step, err = SetupKubeadm().Build()
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

	// Verify 10-kubeadm.conf symlink was recreated
	linkTarget, err := os.Readlink("/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err, "10-kubeadm.conf symlink should be recreated")
	require.Equal(t, "/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf.latest", linkTarget)

	// Verify .latest file was recreated
	_, err = os.Stat("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf.latest")
	require.NoError(t, err, "10-kubeadm.conf.latest should be recreated")
}

func Test_StepKubeadm_ServiceConfiguration_CorruptedConfFile_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	// First run to install and configure kubeadm
	step, err := SetupKubeadm().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	// Corrupt the 10-kubeadm.conf.latest file by writing incorrect content
	corruptedContent := "This is corrupted content"
	err = os.WriteFile("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf.latest", []byte(corruptedContent), core.DefaultFilePerm)
	require.NoError(t, err)

	//
	// When - Run again
	//
	step, err = SetupKubeadm().Build()
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

	// Configuration should run again (corrupted conf file detected)
	require.Equal(t, automa.StatusSuccess, report.StepReports[1].Status)
	require.Empty(t, report.StepReports[1].Metadata[AlreadyConfigured])
	require.Equal(t, "true", report.StepReports[1].Metadata[ConfiguredByThisStep])

	// Verify 10-kubeadm.conf.latest file was fixed
	content, err := os.ReadFile("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf.latest")
	require.NoError(t, err)
	contentStr := string(content)
	require.Contains(t, contentStr, "/opt/provisioner/sandbox/bin/kubelet", "10-kubeadm.conf.latest should contain correct sandbox path")
	require.NotEqual(t, corruptedContent, contentStr, "10-kubeadm.conf.latest should be fixed")
}

func Test_StepKubeadm_ServiceConfiguration_RestoreConfiguration_Integration(t *testing.T) {
	//
	// Given
	//
	cleanUpTempDir(t)

	// Install and configure kubeadm
	step, err := SetupKubeadm().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	// Verify configuration is in place
	_, err = os.Stat("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err, "10-kubeadm.conf should exist before restoration")

	_, err = os.Stat("/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.NoError(t, err, "10-kubeadm.conf symlink should exist before restoration")

	_, err = os.Stat("/opt/provisioner/sandbox/etc/provisioner/kubeadm-init.yaml")
	require.NoError(t, err, "kubeadm-init.yaml should exist before restoration")

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
	_, err = os.Stat("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.Error(t, err, "10-kubeadm.conf should be removed after rollback")

	_, err = os.Stat("/opt/provisioner/sandbox/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf.latest")
	require.Error(t, err, "10-kubeadm.conf.latest should be removed after rollback")

	_, err = os.Stat("/usr/lib/systemd/system/kubelet.service.d/10-kubeadm.conf")
	require.Error(t, err, "10-kubeadm.conf symlink should be removed after rollback")

	_, err = os.Stat("/opt/provisioner/sandbox/etc/provisioner/kubeadm-init.yaml")
	require.Error(t, err, "kubeadm-init.yaml should be removed after rollback")

	// Verify installation was also rolled back
	_, err = os.Stat("/opt/provisioner/sandbox/bin/kubeadm")
	require.Error(t, err, "kubeadm binary should be removed after rollback")

	_, err = os.Stat("/opt/provisioner/tmp/kubeadm")
	require.Error(t, err, "kubeadm temp directory should be removed after rollback")
}

func Test_InitializeCluster_Fresh_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	reset(t)
	setupPrerequisites(t)

	//
	// When
	//
	step, err := InitializeCluster().Build()

	//
	// Then
	//
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Verify cluster is initialized by checking for kubeconfig
	_, err = os.Stat("/etc/kubernetes/admin.conf")
	require.NoError(t, err, "admin.conf should exist after cluster initialization")

	// Verify kubectl can connect to the cluster
	cmd := sudo(exec.Command("/usr/local/bin/kubectl", "get", "nodes"))
	output, err := cmd.Output()
	require.NoError(t, err, "kubectl should be able to get nodes")
	require.Contains(t, string(output), "Ready", "node should be in Ready state")
}

func Test_InitializeCluster_AlreadyInitialized_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	reset(t)
	setupPrerequisites(t)

	// Initialize cluster first time
	step, err := InitializeCluster().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error)

	//
	// When - Try to initialize again
	//
	step, err = InitializeCluster().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())

	//
	// Then
	//
	require.NotNil(t, report)
	// This may succeed or fail depending on implementation - kubeadm init typically fails if already initialized
	// The important thing is that it handles the case gracefully
	if report.Error != nil {
		// If it fails, it should be a known error about cluster already being initialized
		require.Contains(t, report.Error.Error(), "already exists")
	}
}

func Test_InitializeCluster_Rollback_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	reset(t)
	setupPrerequisites(t)

	//
	// When
	//
	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Verify cluster is initialized
	_, err = os.Stat("/etc/kubernetes/admin.conf")
	require.NoError(t, err, "admin.conf should exist after cluster initialization")

	//
	// When - Rollback
	//
	rollbackReport := step.Rollback(context.Background())

	//
	// Then
	//
	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Verify cluster is reset - admin.conf should be removed
	_, err = os.Stat("/etc/kubernetes/admin.conf")
	require.Error(t, err, "admin.conf should be removed after rollback")

	// Verify kubectl cannot connect to the cluster
	cmd := sudo(exec.Command("/usr/local/bin/kubectl", "get", "nodes"))
	_, err = cmd.Output()
	require.Error(t, err, "kubectl should not be able to get nodes after rollback")
}

func Test_InitializeCluster_WithoutPrerequisites_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given - Only reset, no prerequisites setup
	//
	reset(t)

	//
	// When
	//
	step, err := InitializeCluster().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())

	//
	// Then - Should fail due to missing prerequisites
	//
	require.NotNil(t, report)
	require.Error(t, report.Error)
	require.Equal(t, automa.StatusFailed, report.Status)
}

// reset performs a complete cleanup of the Kubernetes environment
func reset(t *testing.T) {
	t.Helper()

	// Reset kubeadm with custom CRI socket
	_ = sudo(exec.Command("kubeadm", "reset",
		"--cri-socket", "unix:///opt/provisioner/sandbox/var/run/crio/crio.sock",
		"--force")).Run()

	// Stop CRI-O service
	_ = sudo(exec.Command("systemctl", "stop", "crio")).Run()

	// Unmount kubernetes directories
	_ = sudo(exec.Command("umount", "/etc/kubernetes")).Run()
	_ = sudo(exec.Command("umount", "/var/lib/kubelet")).Run()
	_ = sudo(exec.Command("umount", "-R", "/var/run/cilium")).Run()

	// Remove provisioner directory
	_ = sudo(exec.Command("rm", "-rf", "/opt/provisioner")).Run()

	// Remove /usr/lib/systemd/system
	_ = sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/crio.service")).Run()
	_ = sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/kubelet.service.d")).Run()
	_ = sudo(exec.Command("rm", "-rf", "/usr/lib/systemd/system/kubelet.service")).Run()

	// Clean up temp directory (from existing tests)
	cleanUpTempDir(t)
}

// setupPrerequisites sets up all the required components before cluster initialization
func setupPrerequisites(t *testing.T) {
	t.Helper()

	// preflight & basic setup
	step, err := SetupHomeDirectoryStructure(core.Paths()).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup home directory structure")

	step, err = RefreshSystemPackageIndex().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to refresh system package index")

	step, err = InstallSystemPackage("iptables", software.NewIptables).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install iptables")

	step, err = InstallSystemPackage("gpg", software.NewGpg).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install gpg")

	step, err = InstallSystemPackage("conntrack", software.NewConntrack).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install conntrack")

	step, err = InstallSystemPackage("ebtables", software.NewEbtables).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install ebtables")

	step, err = InstallSystemPackage("socat", software.NewSocat).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install socat")

	step, err = InstallSystemPackage("nftables", software.NewNftables).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install nftables")

	step, err = SetupSystemdService("nftables").Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup nftables service")

	step, err = InstallKernelModule("overlay").Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install overlay kernel module")

	step, err = InstallKernelModule("br_netfilter").Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to install br_netfilter kernel module")

	step, err = AutoRemoveOrphanedPackages().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to auto-remove orphaned packages")

	// Disable swap
	step, err = DisableSwap().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to disable swap")

	// Configure sysctl for Kubernetes
	step, err = ConfigureSysctlForKubernetes().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to configure sysctl")

	// Setup bind mounts
	step, err = SetupBindMounts().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup bind mounts")

	// Setup kubelet
	step, err = SetupKubelet().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup kubelet")

	// Setup kubelet systemd service
	step, err = SetupSystemdService(software.KubeletServiceName).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup kubelet service")

	// Setup CRI-O
	step, err = SetupCrio().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup CRI-O")

	// Setup CRI-O systemd service
	step, err = SetupSystemdService(software.CrioServiceName).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup CRI-O service")

	step, err = SetupKubeadm().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup kubeadm")

	// Setup kubectl
	step, err = SetupKubectl().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup kubectl")
}
