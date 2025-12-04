// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/core"
	osx "golang.hedera.com/solo-weaver/pkg/os"
)

// Helper function to Reset unit states before and after tests
func resetSystemdState(t *testing.T, unitName string) {
	t.Helper()

	err := cleanupSystemdState(t, unitName)
	require.NoError(t, err)

	// Register cleanup to run after test completes
	t.Cleanup(func() {
		_ = cleanupSystemdState(t, unitName)
	})

	// Make sure unit is not enabled or running
	enabled, err := osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.False(t, enabled)

	running, err := osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.False(t, running)

	createTempService(t, unitName)

	// Make sure unit is still not enabled or running
	enabled, err = osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.False(t, enabled)

	running, err = osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.False(t, running)
}

// Helper function to Reset unit states
func cleanupSystemdState(t *testing.T, unitName string) error {
	t.Helper()

	ctx := context.Background()

	_ = osx.StopService(ctx, unitName)
	_ = osx.DisableService(ctx, unitName)

	// Remove unit file from systemd directory
	systemdDir := "/etc/systemd/system"
	systemdUnitFile := filepath.Join(systemdDir, unitName)
	_ = os.Remove(systemdUnitFile)

	// Reload daemon to apply changes
	return osx.DaemonReload(ctx)
}

func createTempService(t *testing.T, unitName string) {
	// Create a temporary test unit file
	tempDir := t.TempDir()
	unitFile := filepath.Join(tempDir, unitName)
	unitContent := `[Unit]
Description=Test Service for Integration Testing
After=network.target

[Service]
Type=oneshot
ExecStart=/bin/true
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	err := os.WriteFile(unitFile, []byte(unitContent), core.DefaultFilePerm)
	require.NoError(t, err)

	// Copy to systemd directory
	systemdDir := "/etc/systemd/system"
	systemdUnitFile := filepath.Join(systemdDir, unitName)
	err = os.WriteFile(systemdUnitFile, []byte(unitContent), core.DefaultFilePerm)
	require.NoError(t, err)
}

func toBool(s string) bool {
	b, _ := strconv.ParseBool(s)
	return b
}

func Test_StepSystemdService_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	unitName := "test-systemd-fresh.service"
	resetSystemdState(t, unitName)

	//
	// When
	//
	step, err := SetupSystemdService(unitName).Build()

	//
	// Then
	//
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	require.False(t, toBool(report.Metadata[ServiceAlreadyEnabled]))
	require.False(t, toBool(report.Metadata[ServiceAlreadyRunning]))
	require.True(t, toBool(report.Metadata[ServiceEnabledByThisStep]))
	require.True(t, toBool(report.Metadata[ServiceStartedByThisStep]))

	enabled, err := osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, enabled)

	running, err := osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, running)
}

// Test when service is already enabled and running
func Test_StepSystemdService_AlreadyEnabledRunning_Integration(t *testing.T) {
	//
	// Given
	//
	unitName := "test-systemd-already-enabled-running.service"
	resetSystemdState(t, unitName)

	step, err := SetupSystemdService(unitName).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	enabled, err := osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, enabled)

	running, err := osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, running)

	//
	// When
	//
	step, err = SetupSystemdService(unitName).Build()

	//
	// Then
	//
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	require.True(t, toBool(report.Metadata[ServiceAlreadyEnabled]))
	require.True(t, toBool(report.Metadata[ServiceAlreadyRunning]))
	require.False(t, toBool(report.Metadata[ServiceEnabledByThisStep]))
	require.False(t, toBool(report.Metadata[ServiceStartedByThisStep]))

	enabled, err = osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, enabled)

	running, err = osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, running)
}

// Test when service is already enabled but not running
func Test_StepSystemdService_AlreadyEnabledNotRunning_Integration(t *testing.T) {
	//
	// Given
	//
	unitName := "test-systemd-already-enabled-not-running.service"
	resetSystemdState(t, unitName)

	step, err := SetupSystemdService(unitName).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	enabled, err := osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, enabled)

	running, err := osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, running)

	err = osx.StopService(context.Background(), unitName)
	require.NoError(t, err)

	running, err = osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.False(t, running)

	//
	// When
	//
	step, err = SetupSystemdService(unitName).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())

	//
	// Then
	//
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	require.True(t, toBool(report.Metadata[ServiceAlreadyEnabled]))
	require.False(t, toBool(report.Metadata[ServiceAlreadyRunning]))
	require.False(t, toBool(report.Metadata[ServiceEnabledByThisStep]))
	require.True(t, toBool(report.Metadata[ServiceStartedByThisStep]))

	enabled, err = osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, enabled)
	running, err = osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, running)
}

// Test when service is not enabled but running
func Test_StepSystemdService_NotEnabledRunning_Integration(t *testing.T) {
	//
	// Given
	//
	unitName := "test-systemd-not-enabled-running.service"
	resetSystemdState(t, unitName)

	step, err := SetupSystemdService(unitName).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	enabled, err := osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, enabled)

	running, err := osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, running)

	err = osx.DisableService(context.Background(), unitName)
	require.NoError(t, err)

	running, err = osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.False(t, running)

	//
	// When
	//
	step, err = SetupSystemdService(unitName).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())

	//
	// Then
	//
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)
	require.False(t, toBool(report.Metadata[ServiceAlreadyEnabled]))
	require.True(t, toBool(report.Metadata[ServiceAlreadyRunning]))
	require.True(t, toBool(report.Metadata[ServiceEnabledByThisStep]))
	require.False(t, toBool(report.Metadata[ServiceStartedByThisStep]))

	enabled, err = osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, enabled)
	running, err = osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, running)
}

// Test full rollback
func Test_StepSystemdService_Rollback_Fresh_Integration(t *testing.T) {
	//
	// Given
	//
	unitName := "test-systemd-rollback-fresh.service"
	resetSystemdState(t, unitName)

	step, err := SetupSystemdService(unitName).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	enabled, err := osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, enabled)

	running, err := osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, running)

	//
	// When
	//
	rollbackReport := step.Rollback(context.Background())

	//
	// Then
	//
	require.NotNil(t, rollbackReport)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	enabled, err = osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.False(t, enabled)

	running, err = osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.False(t, running)
}

// Test rollback when service was already enabled and running
func Test_StepSystemdService_Rollback_AlreadyEnabledRunning_Integration(t *testing.T) {
	//
	// Given
	//
	unitName := "test-systemd-rollback-already-enabled-running.service"
	resetSystemdState(t, unitName)

	step, err := SetupSystemdService(unitName).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	enabled, err := osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, enabled)

	running, err := osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, running)

	// Call it a second time so that it detects service is already enabled and running
	step, err = SetupSystemdService(unitName).Build()
	require.NoError(t, err)
	report = step.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When
	//
	rollbackReport := step.Rollback(context.Background())

	//
	// Then
	//
	require.NotNil(t, rollbackReport)
	require.Equal(t, automa.StatusSkipped, rollbackReport.Status)

	enabled, err = osx.IsServiceEnabled(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, enabled)

	running, err = osx.IsServiceRunning(context.Background(), unitName)
	require.NoError(t, err)
	require.True(t, running)
}
