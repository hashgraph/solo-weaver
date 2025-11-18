//go:build integration

package os

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/core"
)

// Helper function to reset unit states before and after tests
func resetSystemdState(t *testing.T, unitName, unitFilePath string) {
	t.Helper()

	cleanupSystemdState(t, unitName, unitFilePath)

	// Register cleanup to run after test completes
	t.Cleanup(func() {
		cleanupSystemdState(t, unitName, unitFilePath)
	})
}

// Helper function to reset unit states including deleting the service file
func cleanupSystemdState(t *testing.T, unitName, unitFilePath string) error {
	t.Helper()

	ctx := context.Background()

	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Stop the unit if active
	_, err = conn.StopUnitContext(ctx, unitName, "replace", nil)
	if err != nil {
		return err
	}
	// Disable the unit if enabled
	_, err = conn.DisableUnitFilesContext(ctx, []string{unitName}, false)
	if err != nil {
		return err
	}
	// Remove the unit file
	if unitFilePath != "" {
		err = os.Remove(unitFilePath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	// Reload daemon to apply changes
	return DaemonReload(ctx)
}

// TestDaemonReloadCtx_Integration tests the actual daemon-reload operation
func Test_Systemd_DaemonReloadCtx_Integration(t *testing.T) {
	//
	// Given
	//
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	ctx := context.Background()

	//
	// When
	//
	err := DaemonReload(ctx)

	//
	// Then
	//
	require.NoError(t, err, "daemon-reload should succeed")
}

// TestDaemonReloadCtx_Timeout_Integration tests context timeout handling
func Test_Systemd_DaemonReloadCtx_Timeout_Integration(t *testing.T) {
	//
	// Given
	//
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Millisecond) // Ensure timeout

	//
	// When
	//
	err := DaemonReload(ctx)

	//
	// Then
	//
	require.Error(t, err, "daemon-reload should fail with expired context")
}

// TestEnableService_Integration tests enabling a test unit file
func Test_Systemd_EnableService_Integration(t *testing.T) {
	resetSystemdState(t, "test-systemd-integration.service", "/etc/systemd/system/test-systemd-integration.service")

	//
	// Given
	//
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Create a temporary test unit file
	tempDir := t.TempDir()
	unitFile := filepath.Join(tempDir, "test-systemd-integration.service")
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
	systemdUnitFile := filepath.Join(systemdDir, "test-systemd-integration.service")
	err = os.WriteFile(systemdUnitFile, []byte(unitContent), core.DefaultFilePerm)
	require.NoError(t, err)
	defer os.Remove(systemdUnitFile)

	ctx := context.Background()

	// Reload daemon to recognize the new unit file
	err = DaemonReload(ctx)
	require.NoError(t, err)

	// Verify unit is not enabled initially
	enabled, err := IsServiceEnabled(ctx, "test-systemd-integration.service")
	require.NoError(t, err)
	require.False(t, enabled, "unit should not be enabled initially")

	// Verify unit is not started initially
	active, err := IsServiceRunning(ctx, "test-systemd-integration.service")
	require.NoError(t, err)
	require.False(t, active, "unit should not be active/started")

	//
	// When
	//

	// Enable the unit file
	err = EnableService(ctx, "test-systemd-integration.service")

	//
	// Then
	//
	require.NoError(t, err, "enable service should succeed")

	// Verify unit is now enabled
	enabled, err = IsServiceEnabled(ctx, "test-systemd-integration.service")
	require.NoError(t, err)
	require.True(t, enabled, "unit should be enabled after EnableService")

	// Cleanup: disable the unit
	err = DisableService(ctx, "test-systemd-integration.service")
	require.NoError(t, err)

	// Verify unit is disabled again
	enabled, err = IsServiceEnabled(ctx, "test-systemd-integration.service")
	require.NoError(t, err)
	require.False(t, enabled, "unit should be disabled after cleanup")

	// Verify unit is not started initially
	active, err = IsServiceRunning(ctx, "test-systemd-integration.service")
	require.NoError(t, err)
	require.False(t, active, "unit should not be active/started")
}

// TestEnableService_WithoutSuffix_Integration tests enabling a service without .service suffix
func Test_Systemd_EnableService_WithoutSuffix_Integration(t *testing.T) {
	resetSystemdState(t, "test-systemd-no-suffix.service", "/etc/systemd/system/test-systemd-no-suffix.service")

	//
	// Given
	//
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Create a test unit file
	systemdDir := "/etc/systemd/system"
	systemdUnitFile := filepath.Join(systemdDir, "test-systemd-no-suffix.service")
	unitContent := `[Unit]
Description=Test Service for No Suffix Integration Testing
After=network.target

[Service]
Type=oneshot
ExecStart=/bin/true
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	err := os.WriteFile(systemdUnitFile, []byte(unitContent), core.DefaultFilePerm)
	require.NoError(t, err)
	defer os.Remove(systemdUnitFile)

	ctx := context.Background()

	// Reload daemon to recognize the new unit file
	err = DaemonReload(ctx)
	require.NoError(t, err)

	//
	// When
	//

	// Enable the unit file WITHOUT .service suffix
	err = EnableService(ctx, "test-systemd-no-suffix")

	//
	// Then
	//
	require.NoError(t, err, "enable service should succeed without .service suffix")

	// Verify unit is now enabled
	enabled, err := IsServiceEnabled(ctx, "test-systemd-no-suffix.service")
	require.NoError(t, err)
	require.True(t, enabled, "unit should be enabled after EnableService")

	// Cleanup: disable the unit
	err = DisableService(ctx, "test-systemd-no-suffix")
	require.NoError(t, err)
}

// TestStartStopService_Integration tests starting and stopping a unit
func Test_Systemd_StartStopService_Integration(t *testing.T) {
	resetSystemdState(t, "test-systemd-start-stop.service", "/etc/systemd/system/test-systemd-start-stop.service")

	//
	// Given
	//
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Create a test unit file
	systemdDir := "/etc/systemd/system"
	systemdUnitFile := filepath.Join(systemdDir, "test-systemd-start-stop.service")
	unitContent := `[Unit]
Description=Test Service for Start/Stop Integration Testing
After=network.target

[Service]
Type=oneshot
ExecStart=/bin/true
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	err := os.WriteFile(systemdUnitFile, []byte(unitContent), core.DefaultFilePerm)
	require.NoError(t, err)
	defer os.Remove(systemdUnitFile)

	ctx := context.Background()

	// Verify unit is not active initially
	active, err := IsServiceRunning(ctx, "test-systemd-start-stop.service")
	require.NoError(t, err)
	require.False(t, active, "unit should not be active initially")

	//
	// When
	//

	// Start the unit
	err = RestartService(ctx, "test-systemd-start-stop.service")

	//
	// Then
	//
	require.NoError(t, err, "start service should succeed")

	// Verify unit is now active
	active, err = IsServiceRunning(ctx, "test-systemd-start-stop.service")
	require.NoError(t, err)
	require.True(t, active, "unit should be active after RestartService")

	//
	// When
	//

	// Stop the unit
	err = StopService(ctx, "test-systemd-start-stop.service")

	//
	// Then
	//
	require.NoError(t, err, "stop service should succeed")

	// Verify unit is now inactive
	active, err = IsServiceRunning(ctx, "test-systemd-start-stop.service")
	require.NoError(t, err)
	require.False(t, active, "unit should be inactive after StopService")
}

// TestStartService_WithoutSuffix_Integration tests starting a service without .service suffix
func Test_Systemd_StartService_WithoutSuffix_Integration(t *testing.T) {
	resetSystemdState(t, "test-systemd-start-no-suffix.service", "/etc/systemd/system/test-systemd-start-no-suffix.service")

	//
	// Given
	//
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Create a test unit file
	systemdDir := "/etc/systemd/system"
	systemdUnitFile := filepath.Join(systemdDir, "test-systemd-start-no-suffix.service")
	unitContent := `[Unit]
Description=Test Service for Start Without Suffix Integration Testing
After=network.target

[Service]
Type=oneshot
ExecStart=/bin/true
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	err := os.WriteFile(systemdUnitFile, []byte(unitContent), core.DefaultFilePerm)
	require.NoError(t, err)
	defer os.Remove(systemdUnitFile)

	ctx := context.Background()

	// Reload daemon
	err = DaemonReload(ctx)
	require.NoError(t, err)

	//
	// When
	//

	// Start the unit WITHOUT .service suffix
	err = RestartService(ctx, "test-systemd-start-no-suffix")

	//
	// Then
	//
	require.NoError(t, err, "start service should succeed without .service suffix")

	// Verify unit is now active
	active, err := IsServiceRunning(ctx, "test-systemd-start-no-suffix.service")
	require.NoError(t, err)
	require.True(t, active, "unit should be active after RestartService")

	// Cleanup: stop the unit
	err = StopService(ctx, "test-systemd-start-no-suffix")
	require.NoError(t, err)
}

// TestServiceInUsrLib_Integration tests working with a service in /usr/lib/systemd/system
func Test_Systemd_ServiceInUsrLib_Integration(t *testing.T) {
	//
	// Given
	//
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Create a test unit file in /usr/lib/systemd/system
	usrLibDir := "/usr/lib/systemd/system"

	// Ensure the directory exists
	err := os.MkdirAll(usrLibDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	systemdUnitFile := filepath.Join(usrLibDir, "test-systemd-usrlib.service")
	unitContent := `[Unit]
Description=Test Service in /usr/lib for Integration Testing
After=network.target

[Service]
Type=oneshot
ExecStart=/bin/true
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	err = os.WriteFile(systemdUnitFile, []byte(unitContent), core.DefaultFilePerm)
	require.NoError(t, err)
	defer os.Remove(systemdUnitFile)

	ctx := context.Background()

	// Reload daemon to recognize the new unit file
	err = DaemonReload(ctx)
	require.NoError(t, err)

	//
	// When - Test with .service suffix
	//

	// Enable the unit
	err = EnableService(ctx, "test-systemd-usrlib.service")
	require.NoError(t, err, "enable service in /usr/lib should succeed")

	//
	// Then
	//

	// Verify unit is enabled
	enabled, err := IsServiceEnabled(ctx, "test-systemd-usrlib.service")
	require.NoError(t, err)
	require.True(t, enabled, "unit in /usr/lib should be enabled")

	// Start the unit
	err = RestartService(ctx, "test-systemd-usrlib.service")
	require.NoError(t, err, "start service in /usr/lib should succeed")

	// Verify unit is active
	active, err := IsServiceRunning(ctx, "test-systemd-usrlib.service")
	require.NoError(t, err)
	require.True(t, active, "unit in /usr/lib should be active")

	// Stop the unit
	err = StopService(ctx, "test-systemd-usrlib.service")
	require.NoError(t, err, "stop service in /usr/lib should succeed")

	// Verify unit is inactive
	active, err = IsServiceRunning(ctx, "test-systemd-usrlib.service")
	require.NoError(t, err)
	require.False(t, active, "unit in /usr/lib should be inactive after stop")

	// Disable the unit
	err = DisableService(ctx, "test-systemd-usrlib.service")
	require.NoError(t, err, "disable service in /usr/lib should succeed")

	// Verify unit is disabled
	enabled, err = IsServiceEnabled(ctx, "test-systemd-usrlib.service")
	require.NoError(t, err)
	require.False(t, enabled, "unit in /usr/lib should be disabled")
}

// TestServiceInUsrLib_WithoutSuffix_Integration tests working with a service in /usr/lib without .service suffix
func Test_Systemd_ServiceInUsrLib_WithoutSuffix_Integration(t *testing.T) {
	//
	// Given
	//
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Create a test unit file in /usr/lib/systemd/system
	usrLibDir := "/usr/lib/systemd/system"

	// Ensure the directory exists
	err := os.MkdirAll(usrLibDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	systemdUnitFile := filepath.Join(usrLibDir, "test-systemd-usrlib-nosuffix.service")
	unitContent := `[Unit]
Description=Test Service in /usr/lib Without Suffix for Integration Testing
After=network.target

[Service]
Type=oneshot
ExecStart=/bin/true
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	err = os.WriteFile(systemdUnitFile, []byte(unitContent), core.DefaultFilePerm)
	require.NoError(t, err)
	defer os.Remove(systemdUnitFile)

	ctx := context.Background()

	// Reload daemon to recognize the new unit file
	err = DaemonReload(ctx)
	require.NoError(t, err)

	//
	// When - Test WITHOUT .service suffix
	//

	// Enable the unit WITHOUT suffix
	err = EnableService(ctx, "test-systemd-usrlib-nosuffix")
	require.NoError(t, err, "enable service in /usr/lib without suffix should succeed")

	//
	// Then
	//

	// Verify unit is enabled
	enabled, err := IsServiceEnabled(ctx, "test-systemd-usrlib-nosuffix.service")
	require.NoError(t, err)
	require.True(t, enabled, "unit in /usr/lib should be enabled")

	// Start the unit WITHOUT suffix
	err = RestartService(ctx, "test-systemd-usrlib-nosuffix")
	require.NoError(t, err, "start service in /usr/lib without suffix should succeed")

	// Verify unit is active
	active, err := IsServiceRunning(ctx, "test-systemd-usrlib-nosuffix.service")
	require.NoError(t, err)
	require.True(t, active, "unit in /usr/lib should be active")

	// Stop the unit WITHOUT suffix
	err = StopService(ctx, "test-systemd-usrlib-nosuffix")
	require.NoError(t, err, "stop service in /usr/lib without suffix should succeed")

	// Verify unit is inactive
	active, err = IsServiceRunning(ctx, "test-systemd-usrlib-nosuffix.service")
	require.NoError(t, err)
	require.False(t, active, "unit in /usr/lib should be inactive after stop")

	// Disable the unit WITHOUT suffix
	err = DisableService(ctx, "test-systemd-usrlib-nosuffix")
	require.NoError(t, err, "disable service in /usr/lib without suffix should succeed")

	// Verify unit is disabled
	enabled, err = IsServiceEnabled(ctx, "test-systemd-usrlib-nosuffix.service")
	require.NoError(t, err)
	require.False(t, enabled, "unit in /usr/lib should be disabled")
}

// TestStartService_NonExistent_Integration tests starting a non-existent unit
func Test_Systemd_StartService_NonExistent_Integration(t *testing.T) {
	resetSystemdState(t, "nonexistent-unit-12345.service", "")

	//
	// Given
	//
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	ctx := context.Background()

	//
	// When
	//
	err := RestartService(ctx, "nonexistent-unit-12345.service")

	//
	// Then
	//
	require.Error(t, err, "starting non-existent unit should fail")
}

// TestStopService_NonExistent_Integration tests stopping a non-existent unit
func Test_Systemd_StopService_NonExistent_Integration(t *testing.T) {
	resetSystemdState(t, "nonexistent-unit-12345.service", "/etc/systemd/system/nonexistent-unit-12345.service")

	//
	// Given
	//
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	ctx := context.Background()

	//
	// When
	//
	err := StopService(ctx, "nonexistent-unit-12345.service")

	//
	// Then
	//
	require.Error(t, err, "stopping non-existent unit should fail")
}

// TestDisableService_Integration tests disabling unit files
func Test_Systemd_DisableService_Integration(t *testing.T) {
	resetSystemdState(t, "test-systemd-disable.service", "/etc/systemd/system/test-systemd-disable.service")

	//
	// Given
	//
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Create a test unit file
	systemdDir := "/etc/systemd/system"
	systemdUnitFile := filepath.Join(systemdDir, "test-systemd-disable.service")
	unitContent := `[Unit]
Description=Test Service for Disable Integration Testing
After=network.target

[Service]
Type=oneshot
ExecStart=/bin/true
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	err := os.WriteFile(systemdUnitFile, []byte(unitContent), core.DefaultFilePerm)
	require.NoError(t, err)
	defer os.Remove(systemdUnitFile)

	ctx := context.Background()

	// Reload daemon
	err = DaemonReload(ctx)
	require.NoError(t, err)

	// Enable the unit first
	err = EnableService(ctx, "test-systemd-disable.service")
	require.NoError(t, err)

	// Verify unit is enabled
	enabled, err := IsServiceEnabled(ctx, "test-systemd-disable.service")
	require.NoError(t, err)
	require.True(t, enabled, "unit should be enabled before disabling")

	//
	// When
	//

	// Disable the unit
	err = DisableService(ctx, "test-systemd-disable.service")

	//
	// Then
	//
	require.NoError(t, err, "disable service should succeed")

	// Verify unit is now disabled
	enabled, err = IsServiceEnabled(ctx, "test-systemd-disable.service")
	require.NoError(t, err)
	require.False(t, enabled, "unit should be disabled after DisableService")
}

// TestFullLifecycle_Integration tests a complete lifecycle of a systemd unit
func Test_Systemd_FullLifecycle_Integration(t *testing.T) {
	resetSystemdState(t, "test-systemd-lifecycle.service", "/etc/systemd/system/test-systemd-lifecycle.service")

	//
	// Given
	//

	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Create a test unit file
	systemdDir := "/etc/systemd/system"
	systemdUnitFile := filepath.Join(systemdDir, "test-systemd-lifecycle.service")
	unitContent := `[Unit]
Description=Test Service for Full Lifecycle Integration Testing
After=network.target

[Service]
Type=oneshot
ExecStart=/bin/true
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
`
	err := os.WriteFile(systemdUnitFile, []byte(unitContent), core.DefaultFilePerm)
	require.NoError(t, err)
	defer os.Remove(systemdUnitFile)

	ctx := context.Background()

	//
	// When
	//

	// Step 1: Reload daemon
	err = DaemonReload(ctx)
	require.NoError(t, err, "daemon reload should succeed")

	// Verify initial state
	enabled, err := IsServiceEnabled(ctx, "test-systemd-lifecycle.service")
	require.NoError(t, err)
	require.False(t, enabled, "unit should not be enabled initially")

	active, err := IsServiceRunning(ctx, "test-systemd-lifecycle.service")
	require.NoError(t, err)
	require.False(t, active, "unit should not be active initially")

	// Step 2: Enable the unit
	err = EnableService(ctx, "test-systemd-lifecycle.service")
	require.NoError(t, err, "enable should succeed")

	//
	// Then
	//

	// Verify unit is enabled
	enabled, err = IsServiceEnabled(ctx, "test-systemd-lifecycle.service")
	require.NoError(t, err)
	require.True(t, enabled, "unit should be enabled after EnableService")

	// Step 3: Start the unit
	err = RestartService(ctx, "test-systemd-lifecycle.service")
	require.NoError(t, err, "start should succeed")

	// Verify unit is active
	active, err = IsServiceRunning(ctx, "test-systemd-lifecycle.service")
	require.NoError(t, err)
	require.True(t, active, "unit should be active after RestartService")

	// Step 4: Stop the unit
	err = StopService(ctx, "test-systemd-lifecycle.service")
	require.NoError(t, err, "stop should succeed")

	// Verify unit is inactive
	active, err = IsServiceRunning(ctx, "test-systemd-lifecycle.service")
	require.NoError(t, err)
	require.False(t, active, "unit should be inactive after StopService")

	// Step 5: Disable the unit
	err = DisableService(ctx, "test-systemd-lifecycle.service")
	require.NoError(t, err, "disable should succeed")

	// Verify unit is disabled
	enabled, err = IsServiceEnabled(ctx, "test-systemd-lifecycle.service")
	require.NoError(t, err)
	require.False(t, enabled, "unit should be disabled after DisableService")

	// Step 6: Final reload
	err = DaemonReload(ctx)
	require.NoError(t, err, "final daemon reload should succeed")
}
