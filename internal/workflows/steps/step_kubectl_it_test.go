// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"context"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

func Test_StepKubectl_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	//
	// When
	//
	step, err := SetupKubectl().Build()

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

	// Verify downloaded file exists
	found := testutil.FileWithPrefixExists(t, core.Paths().DownloadsDir, "kubectl")
	require.True(t, found, "expected a file prefixed with kubectl in the downloads directory")

	// Verify temporary folder for kubectl is cleaned up
	_, err = os.Stat("/opt/solo/weaver/tmp/kubectl")
	require.Error(t, err)

	// Verify binary files are there
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubectl")
	require.NoError(t, err)
}

func Test_StepKubectl_AlreadyInstalled_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	step, err := SetupKubectl().Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When
	//
	step, err = SetupKubectl().Build()

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

func Test_StepKubectl_Rollback_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	//
	// When
	//
	step, err := SetupKubectl().Build()

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

	// Verify download folder is still there
	found := testutil.FileWithPrefixExists(t, core.Paths().DownloadsDir, "kubectl")
	require.True(t, found, "expected a file prefixed with kubectl in the downloads directory")

	// Verify temporary folder for kubectl is removed
	_, err = os.Stat("/opt/solo/weaver/tmp/kubectl")
	require.Error(t, err)

	// Verify binary files are removed
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubectl")
	require.Error(t, err)
}

func Test_StepKubectl_Rollback_Setup_DownloadFailed(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	// Remove any existing kubectl files from downloads folder to ensure download will be attempted
	files, err := os.ReadDir(core.Paths().DownloadsDir)
	if err == nil {
		for _, file := range files {
			if strings.HasPrefix(file.Name(), "kubectl") {
				_ = os.Remove(path.Join(core.Paths().DownloadsDir, file.Name()))
			}
		}
	}

	// Make the downloads directory read-only
	err = os.MkdirAll(core.Paths().DownloadsDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create downloads directory")
	cmd := exec.Command("chattr", "+i", core.Paths().DownloadsDir)
	err = cmd.Run()
	require.NoError(t, err, "Failed to make downloads directory read-only")

	// Restore permissions after test
	t.Cleanup(func() {
		_ = exec.Command("chattr", "-i", core.Paths().DownloadsDir).Run()
	})

	//
	// When
	//
	step, err := SetupKubectl().Build()
	require.NoError(t, err)

	//
	// When - Rollback
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

	// Verify downloaded file is not there
	found := testutil.FileWithPrefixExists(t, core.Paths().DownloadsDir, "kubectl")
	require.False(t, found, "did not expect a file prefixed with kubectl in the downloads directory")

	// Confirm binary files were not created
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubectl")
	require.Error(t, err)
}

func Test_StepKubectl_Rollback_Setup_InstallFailed(t *testing.T) {
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
	step, err := SetupKubectl().Build()
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

	// Verify download folder is still around when there is an extraction error
	found := testutil.FileWithPrefixExists(t, core.Paths().DownloadsDir, "kubectl")
	require.True(t, found, "expected a file prefixed with kubectl in the downloads directory")

	// Verify binary files were not installed
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubectl")
	require.Error(t, err)
}

func Test_StepKubectl_Rollback_Setup_CleanupFailed(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	// Create an unremovable directory under download folder
	unremovableDir := path.Join(core.Paths().TempDir, "kubectl", "unremovable")

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
	step, err := SetupKubectl().Build()
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
	found := testutil.FileWithPrefixExists(t, core.Paths().DownloadsDir, "kubectl")
	require.True(t, found, "expected a file prefixed with kubectl in the downloads directory")

	// Check there are files in the tmp/kubectl directory
	files, err := os.ReadDir(path.Join(core.Paths().TempDir, "kubectl"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1, "Expected at least 1 file in the tmp/kubectl directory")

	// Verify binary files were removed
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubectl")
	require.Error(t, err)
}

func Test_StepKubectl_Rollback_ConfigurationFailed(t *testing.T) {
	//
	// Given
	//
	testutil.CleanUpTempDir(t)

	// Create an unremovable directory under download folder
	unremovableDir := path.Join(core.Paths().TempDir, "kubectl", "unremovable")

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
	step, err := SetupKubectl().Build()
	require.NoError(t, err)

	//
	// When - Rollback
	//

	report := step.Execute(context.Background())

	require.NotNil(t, report)

	// Check errorx error type
	require.Error(t, report.Error)

	// Confirm errorx error type is DownloadError

	require.True(t, errorx.IsOfType(errorx.Cast(report.StepReports[0].Error), software.CleanupError))
	require.Equal(t, automa.StatusFailed, report.Status)

	//
	// Then
	//
	rollbackReport := report.StepReports[0].Rollback

	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Verify download folder is still around when there is a configuration error
	found := testutil.FileWithPrefixExists(t, core.Paths().DownloadsDir, "kubectl")
	require.True(t, found, "expected a file prefixed with kubectl in the downloads directory")

	// Verify binary files were removed
	_, err = os.Stat("/opt/solo/weaver/sandbox/bin/kubectl")
	require.Error(t, err)
}
