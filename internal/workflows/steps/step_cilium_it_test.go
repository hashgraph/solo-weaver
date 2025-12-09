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
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

func Test_StepCilium_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.Reset(t)

	//
	// When
	//
	step, err := SetupCilium().Build()

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
	require.Equal(t, "true", report.StepReports[0].Metadata[ExtractedByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[InstalledByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[CleanedUpByThisStep])
	require.Empty(t, report.StepReports[1].Metadata[AlreadyConfigured])
	require.Equal(t, "true", report.StepReports[1].Metadata[ConfiguredByThisStep])
}

func Test_StepCilium_AlreadyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.Reset(t)

	step, err := SetupCilium().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When
	//
	step, err = SetupCilium().Build()

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
	require.Empty(t, report.StepReports[0].Metadata[ExtractedByThisStep])
	require.Empty(t, report.StepReports[0].Metadata[InstalledByThisStep])
	require.Empty(t, report.StepReports[0].Metadata[CleanedUpByThisStep])

	require.Equal(t, automa.StatusSkipped, report.StepReports[1].Status)
	require.Equal(t, "true", report.StepReports[1].Metadata[AlreadyConfigured])
	require.Empty(t, report.StepReports[1].Metadata[ConfiguredByThisStep])
}

func Test_StepCilium_Rollback_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.Reset(t)

	//
	// When
	//
	step, err := SetupCilium().Build()

	require.NoError(t, err)
	report := step.Execute(context.Background())

	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	require.Equal(t, "true", report.StepReports[0].Metadata[DownloadedByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[ExtractedByThisStep])
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

	// Verify download folder for cilium is removed
	_, err = os.Stat("/opt/solo/weaver/tmp/cilium")
	require.Error(t, err)

	// Verify binary files are removed
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/cilium")
	require.Error(t, err)
}

func Test_StepCilium_Rollback_Setup_DownloadFailed(t *testing.T) {
	//
	// Given
	//
	testutil.Reset(t)

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
	step, err := SetupCilium().Build()
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

	// Verify download folder for cilium was not created
	_, err = os.Stat("/opt/solo/weaver/tmp/cilium")
	require.Error(t, err)

	// Confirm binary files were not created
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/cilium")
	require.Error(t, err)
}

func Test_StepCilium_Rollback_Setup_InstallFailed(t *testing.T) {
	//
	// Given
	//
	testutil.Reset(t)

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
	step, err := SetupCilium().Build()
	require.NoError(t, err)

	//
	// When - Execute (should fail at install step)
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
	_, err = os.Stat("/opt/solo/weaver/tmp/cilium")
	require.NoError(t, err)

	// Check there are downloaded files in the cilium directory
	files, err := os.ReadDir(path.Join(core.Paths().TempDir, "cilium"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1, "Expected at least 1 file in the cilium directory")

	// Verify binary files were not installed
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/cilium")
	require.Error(t, err)
}

func Test_StepCilium_Rollback_Setup_CleanupFailed(t *testing.T) {
	//
	// Given
	//
	testutil.Reset(t)

	// Create an unremovable directory under download folder
	unremovableDir := path.Join(core.Paths().TempDir, "cilium", "unremovable")

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
	step, err := SetupCilium().Build()
	require.NoError(t, err)

	//
	// When - Execute (should fail at cleanup step)
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
	_, err = os.Stat("/opt/solo/weaver/tmp/cilium")
	require.NoError(t, err)

	// Check there are files in the tmp/cilium directory
	files, err := os.ReadDir(path.Join(core.Paths().TempDir, "cilium"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1, "Expected at least 1 file in the tmp/cilium directory")

	// Verify binary files were removed
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/cilium")
	require.Error(t, err)
}

func Test_StepCilium_Rollback_ConfigurationFailed(t *testing.T) {
	//
	// Given
	//
	testutil.Reset(t)

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
	step, err := SetupCilium().Build()
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
	require.Equal(t, "true", report.StepReports[0].Metadata[ExtractedByThisStep])
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
	_, err = os.Stat("/opt/solo/weaver/tmp/cilium")
	require.Error(t, err)

	// Verify binary files were removed from sandbox
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/cilium")
	require.Error(t, err)

	// Verify configuration was not applied - symlinks should not exist
	_, err = os.Stat("/usr/local/bin/cilium")
	require.Error(t, err)
}

func Test_StartCilium_Fresh_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	testutil.Reset(t)
	SetupPrerequisitesToLevel(t, SetupKubeadmLevel)

	// Setup Cilium CLI first
	step, err := SetupCilium().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup Cilium CLI")

	//
	// When
	//
	step, err = StartCilium().Build()
	require.NoError(t, err)

	report = step.Execute(context.Background())

	//
	// Then
	//
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Verify the install-cilium-cni step succeeded and installed Cilium
	require.Equal(t, automa.StatusSuccess, report.StepReports[0].Status)
	require.Empty(t, report.StepReports[0].Metadata[AlreadyInstalled])
	require.Equal(t, "true", report.StepReports[0].Metadata[InstalledByThisStep])

	// Verify Cilium CNI is installed in the cluster
	cmd := testutil.Sudo(exec.Command("/usr/local/bin/kubectl", "get", "pods", "-n", "kube-system", "-l", "k8s-app=cilium"))
	output, err := cmd.Output()
	require.NoError(t, err, "kubectl should be able to get cilium pods")
	require.Contains(t, string(output), "cilium", "Cilium pods should be present")
}

func Test_StartCilium_AlreadyInstalled_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	testutil.Reset(t)
	SetupPrerequisitesToLevel(t, SetupKubeadmLevel)

	// Setup Cilium CLI first
	step, err := SetupCilium().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup Cilium CLI")

	// Start Cilium once
	step, err = StartCilium().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to start Cilium initially")

	//
	// When - Start again
	//
	step, err = StartCilium().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())

	//
	// Then
	//
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Verify the install-cilium-cni step was skipped due to already being installed
	require.Equal(t, automa.StatusSkipped, report.StepReports[0].Status)
	require.Equal(t, "true", report.StepReports[0].Metadata[AlreadyInstalled])
	require.Empty(t, report.StepReports[0].Metadata[InstalledByThisStep])

	// Verify Cilium CNI is still running
	cmd := testutil.Sudo(exec.Command("/usr/local/bin/kubectl", "get", "pods", "-n", "kube-system", "-l", "k8s-app=cilium"))
	output, err := cmd.Output()
	require.NoError(t, err, "kubectl should be able to get cilium pods")
	require.Contains(t, string(output), "cilium", "Cilium pods should still be present")
}

func Test_StartCilium_Rollback_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	testutil.Reset(t)
	SetupPrerequisitesToLevel(t, SetupKubeadmLevel)

	// Setup Cilium CLI first
	step, err := SetupCilium().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to setup Cilium CLI")

	// Start Cilium
	step, err = StartCilium().Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to start Cilium")

	//
	// When - Rollback
	//
	rollbackReport := step.Rollback(context.Background())

	//
	// Then
	//
	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Verify Cilium CNI is uninstalled from the cluster
	cmd := testutil.Sudo(exec.Command("/usr/local/bin/kubectl", "get", "pods", "-n", "kube-system", "-l", "k8s-app=cilium"))
	output, err := cmd.Output()
	// Should either error (no resources found) or show no cilium pods
	if err == nil {
		// If command succeeds, output should not contain cilium pods
		require.NotContains(t, string(output), "Running", "No cilium pods should be running after rollback")
	}
}

func Test_StartCilium_WithoutPrerequisites_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given - Only Reset, no prerequisites setup
	//
	testutil.Reset(t)

	//
	// When
	//
	step, err := StartCilium().Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())

	//
	// Then - Should fail due to missing prerequisites
	//
	require.NotNil(t, report)
	require.Error(t, report.Error)
	require.Equal(t, automa.StatusFailed, report.Status)
}
