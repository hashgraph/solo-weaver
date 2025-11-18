//go:build integration

package steps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/mount"
)

func Test_SetupBindMounts_Fresh_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	testBindMounts := []mount.BindMount{
		{Source: filepath.Join(core.Paths().SandboxDir, "/etc/kubernetes"), Target: "/etc/kubernetes"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/lib/kubelet"), Target: "/var/lib/kubelet"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/run/cilium"), Target: "/var/run/cilium"},
	}

	// Cleanup before test
	cleanupBindMounts(t, testBindMounts)

	// Backup and restore fstab
	backupFstab := backupFstabFile(t)

	// Register cleanup and restore to run after test completes
	t.Cleanup(func() {
		cleanupBindMounts(t, testBindMounts)
		restoreFstabFile(t, backupFstab)
	})

	// Ensure source directories exist
	for _, bm := range testBindMounts {
		err := os.MkdirAll(bm.Source, core.DefaultDirOrExecPerm)
		require.NoError(t, err, "Failed to create source directory %s", bm.Source)
	}

	// Verify initial state - nothing should be mounted
	for _, bm := range testBindMounts {
		mounted, fstabExists, err := mount.IsBindMountedWithFstab(bm)
		require.NoError(t, err)
		require.False(t, mounted, "Mount %s should not be mounted initially", bm.Target)
		require.False(t, fstabExists, "Fstab entry for %s should not exist initially", bm.Target)
	}

	//
	// When
	//
	workflow, err := SetupBindMounts().Build()
	require.NoError(t, err)

	report := workflow.Execute(context.Background())

	//
	// Then
	//
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Verify all mounts are set up
	for _, bm := range testBindMounts {
		mounted, fstabExists, err := mount.IsBindMountedWithFstab(bm)
		require.NoError(t, err)
		require.True(t, mounted, "Mount %s should be mounted", bm.Target)
		require.True(t, fstabExists, "Fstab entry for %s should exist", bm.Target)

		// Verify target directory exists
		_, err = os.Stat(bm.Target)
		require.NoError(t, err, "Target directory %s should exist", bm.Target)
	}

	// Verify metadata in step reports
	require.Len(t, report.StepReports, 3)
	for _, stepReport := range report.StepReports {
		require.Equal(t, automa.StatusSuccess, stepReport.Status)
		require.Equal(t, "true", stepReport.Metadata[KeyModifiedByThisStep])
		require.Equal(t, "false", stepReport.Metadata[KeyAlreadyMounted])
		require.Equal(t, "false", stepReport.Metadata[KeyAlreadyInFstab])
	}
}

func Test_SetupBindMounts_AlreadySetup_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	testBindMounts := []mount.BindMount{
		{Source: filepath.Join(core.Paths().SandboxDir, "/etc/kubernetes"), Target: "/etc/kubernetes"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/lib/kubelet"), Target: "/var/lib/kubelet"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/run/cilium"), Target: "/var/run/cilium"},
	}

	// Cleanup before test
	cleanupBindMounts(t, testBindMounts)

	// Backup and restore fstab
	backupFstab := backupFstabFile(t)

	// Register cleanup and restore to run after test completes
	t.Cleanup(func() {
		cleanupBindMounts(t, testBindMounts)
		restoreFstabFile(t, backupFstab)
	})

	// Ensure source directories exist
	for _, bm := range testBindMounts {
		err := os.MkdirAll(bm.Source, core.DefaultDirOrExecPerm)
		require.NoError(t, err)
	}

	// First execution - setup bind mounts
	workflow, err := SetupBindMounts().Build()
	require.NoError(t, err)

	report := workflow.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When - Execute again (idempotency test)
	//
	workflow2, err := SetupBindMounts().Build()
	require.NoError(t, err)

	report2 := workflow2.Execute(context.Background())

	//
	// Then
	//
	require.NoError(t, report2.Error)
	require.Equal(t, automa.StatusSuccess, report2.Status)

	// Verify all mounts are still set up
	for _, bm := range testBindMounts {
		mounted, fstabExists, err := mount.IsBindMountedWithFstab(bm)
		require.NoError(t, err)
		require.True(t, mounted, "Mount %s should still be mounted", bm.Target)
		require.True(t, fstabExists, "Fstab entry for %s should still exist", bm.Target)
	}

	// Verify metadata indicates no modifications were made
	require.Len(t, report2.StepReports, 3)
	for _, stepReport := range report2.StepReports {
		require.Equal(t, automa.StatusSuccess, stepReport.Status)
		require.Equal(t, "false", stepReport.Metadata[KeyModifiedByThisStep])
		require.Equal(t, "true", stepReport.Metadata[KeyAlreadyMounted])
		require.Equal(t, "true", stepReport.Metadata[KeyAlreadyInFstab])
	}
}

func Test_SetupBindMounts_PartiallySetup_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	testBindMounts := []mount.BindMount{
		{Source: filepath.Join(core.Paths().SandboxDir, "/etc/kubernetes"), Target: "/etc/kubernetes"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/lib/kubelet"), Target: "/var/lib/kubelet"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/run/cilium"), Target: "/var/run/cilium"},
	}

	// Cleanup before test
	cleanupBindMounts(t, testBindMounts)

	// Backup and restore fstab
	backupFstab := backupFstabFile(t)

	// Register cleanup and restore to run after test completes
	t.Cleanup(func() {
		cleanupBindMounts(t, testBindMounts)
		restoreFstabFile(t, backupFstab)
	})

	// Ensure source directories exist
	for _, bm := range testBindMounts {
		err := os.MkdirAll(bm.Source, core.DefaultDirOrExecPerm)
		require.NoError(t, err)
	}

	// Setup only the first bind mount manually
	err := mount.SetupBindMountsWithFstab(testBindMounts[0])
	require.NoError(t, err)

	// Verify partial state
	mounted, fstabExists, err := mount.IsBindMountedWithFstab(testBindMounts[0])
	require.NoError(t, err)
	require.True(t, mounted)
	require.True(t, fstabExists)

	//
	// When - Execute workflow
	//
	workflow, err := SetupBindMounts().Build()
	require.NoError(t, err)

	report := workflow.Execute(context.Background())

	//
	// Then
	//
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Verify all mounts are set up
	for _, bm := range testBindMounts {
		mounted, fstabExists, err := mount.IsBindMountedWithFstab(bm)
		require.NoError(t, err)
		require.True(t, mounted, "Mount %s should be mounted", bm.Target)
		require.True(t, fstabExists, "Fstab entry for %s should exist", bm.Target)
	}

	// First step should show no modifications (already setup)
	require.Equal(t, "false", report.StepReports[0].Metadata[KeyModifiedByThisStep])
	require.Equal(t, "true", report.StepReports[0].Metadata[KeyAlreadyMounted])

	// Other steps should show modifications
	require.Equal(t, "true", report.StepReports[1].Metadata[KeyModifiedByThisStep])
	require.Equal(t, "true", report.StepReports[2].Metadata[KeyModifiedByThisStep])
}

func Test_SetupBindMounts_Rollback_Fresh_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	testBindMounts := []mount.BindMount{
		{Source: filepath.Join(core.Paths().SandboxDir, "/etc/kubernetes"), Target: "/etc/kubernetes"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/lib/kubelet"), Target: "/var/lib/kubelet"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/run/cilium"), Target: "/var/run/cilium"},
	}

	// Cleanup before test
	cleanupBindMounts(t, testBindMounts)

	// Backup and restore fstab
	backupFstab := backupFstabFile(t)

	// Register cleanup and restore to run after test completes
	t.Cleanup(func() {
		cleanupBindMounts(t, testBindMounts)
		restoreFstabFile(t, backupFstab)
	})

	// Ensure source directories exist
	for _, bm := range testBindMounts {
		err := os.MkdirAll(bm.Source, core.DefaultDirOrExecPerm)
		require.NoError(t, err)
	}

	// Execute workflow to setup bind mounts
	workflow, err := SetupBindMounts().Build()
	require.NoError(t, err)

	report := workflow.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	// Verify mounts are set up
	for _, bm := range testBindMounts {
		mounted, fstabExists, err := mount.IsBindMountedWithFstab(bm)
		require.NoError(t, err)
		require.True(t, mounted)
		require.True(t, fstabExists)
	}

	//
	// When - Rollback
	//
	rollbackReport := workflow.Rollback(context.Background())

	//
	// Then
	//
	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Verify all mounts are removed
	for _, bm := range testBindMounts {
		mounted, fstabExists, err := mount.IsBindMountedWithFstab(bm)
		require.NoError(t, err)
		require.False(t, mounted, "Mount %s should be unmounted after rollback", bm.Target)
		require.False(t, fstabExists, "Fstab entry for %s should be removed after rollback", bm.Target)
	}
}

func Test_SetupBindMounts_Rollback_PreExisting_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	testBindMounts := []mount.BindMount{
		{Source: filepath.Join(core.Paths().SandboxDir, "/etc/kubernetes"), Target: "/etc/kubernetes"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/lib/kubelet"), Target: "/var/lib/kubelet"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/run/cilium"), Target: "/var/run/cilium"},
	}

	// Cleanup before test
	cleanupBindMounts(t, testBindMounts)

	// Backup and restore fstab
	backupFstab := backupFstabFile(t)

	// Register cleanup and restore to run after test completes
	t.Cleanup(func() {
		cleanupBindMounts(t, testBindMounts)
		restoreFstabFile(t, backupFstab)
	})

	// Ensure source directories exist
	for _, bm := range testBindMounts {
		err := os.MkdirAll(bm.Source, core.DefaultDirOrExecPerm)
		require.NoError(t, err)
	}

	// Setup all bind mounts before the workflow (simulating pre-existing state)
	for _, bm := range testBindMounts {
		err := mount.SetupBindMountsWithFstab(bm)
		require.NoError(t, err)
	}

	// Verify pre-existing state
	for _, bm := range testBindMounts {
		mounted, fstabExists, err := mount.IsBindMountedWithFstab(bm)
		require.NoError(t, err)
		require.True(t, mounted)
		require.True(t, fstabExists)
	}

	// Execute workflow (should be no-op since already setup)
	workflow, err := SetupBindMounts().Build()
	require.NoError(t, err)

	report := workflow.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When - Rollback
	//
	rollbackReport := workflow.Rollback(context.Background())

	//
	// Then
	//
	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Verify mounts are NOT removed (they were pre-existing)
	for _, bm := range testBindMounts {
		mounted, fstabExists, err := mount.IsBindMountedWithFstab(bm)
		require.NoError(t, err)
		require.True(t, mounted, "Pre-existing mount %s should remain mounted after rollback", bm.Target)
		require.True(t, fstabExists, "Pre-existing fstab entry for %s should remain after rollback", bm.Target)
	}

	// Verify all rollback steps were skipped
	require.Len(t, rollbackReport.StepReports, 3)
	for _, stepReport := range rollbackReport.StepReports {
		require.Equal(t, automa.StatusSkipped, stepReport.Status)
	}
}

func Test_SetupBindMounts_Rollback_Mixed_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	testBindMounts := []mount.BindMount{
		{Source: filepath.Join(core.Paths().SandboxDir, "/etc/kubernetes"), Target: "/etc/kubernetes"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/lib/kubelet"), Target: "/var/lib/kubelet"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/run/cilium"), Target: "/var/run/cilium"},
	}

	// Cleanup before test
	cleanupBindMounts(t, testBindMounts)

	// Backup and restore fstab
	backupFstab := backupFstabFile(t)

	// Register cleanup and restore to run after test completes
	t.Cleanup(func() {
		cleanupBindMounts(t, testBindMounts)
		restoreFstabFile(t, backupFstab)
	})

	// Ensure source directories exist
	for _, bm := range testBindMounts {
		err := os.MkdirAll(bm.Source, core.DefaultDirOrExecPerm)
		require.NoError(t, err)
	}

	// Setup only the first bind mount before the workflow (pre-existing)
	err := mount.SetupBindMountsWithFstab(testBindMounts[0])
	require.NoError(t, err)

	// Execute workflow (should setup the remaining mounts)
	workflow, err := SetupBindMounts().Build()
	require.NoError(t, err)

	report := workflow.Execute(context.Background())
	require.NoError(t, report.Error)
	require.Equal(t, automa.StatusSuccess, report.Status)

	//
	// When - Rollback
	//
	rollbackReport := workflow.Rollback(context.Background())

	//
	// Then
	//
	require.NoError(t, rollbackReport.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport.Status)

	// Verify first mount (pre-existing) is NOT removed
	mounted, fstabExists, err := mount.IsBindMountedWithFstab(testBindMounts[0])
	require.NoError(t, err)
	require.True(t, mounted, "Pre-existing mount should remain mounted after rollback")
	require.True(t, fstabExists, "Pre-existing fstab entry should remain after rollback")

	// Verify other mounts (created by workflow) ARE removed
	for i := 1; i < len(testBindMounts); i++ {
		mounted, fstabExists, err := mount.IsBindMountedWithFstab(testBindMounts[i])
		require.NoError(t, err)
		require.False(t, mounted, "Newly created mount %s should be unmounted after rollback", testBindMounts[i].Target)
		require.False(t, fstabExists, "Newly created fstab entry for %s should be removed after rollback", testBindMounts[i].Target)
	}

	// Verify rollback report
	require.Len(t, rollbackReport.StepReports, 3)
	require.Equal(t, automa.StatusSkipped, rollbackReport.StepReports[0].Status, "First step should be skipped (pre-existing)")
	require.Equal(t, automa.StatusSuccess, rollbackReport.StepReports[1].Status, "Second step should succeed")
	require.Equal(t, automa.StatusSuccess, rollbackReport.StepReports[2].Status, "Third step should succeed")
}

func Test_SetupBindMounts_RollbackOnFailure_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	testBindMounts := []mount.BindMount{
		{Source: filepath.Join(core.Paths().SandboxDir, "/etc/kubernetes"), Target: "/etc/kubernetes"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/lib/kubelet"), Target: "/var/lib/kubelet"},
	}

	// Cleanup before test
	cleanupBindMounts(t, testBindMounts)

	// Backup and restore fstab
	backupFstab := backupFstabFile(t)

	// Register cleanup and restore to run after test completes
	t.Cleanup(func() {
		cleanupBindMounts(t, testBindMounts)
		restoreFstabFile(t, backupFstab)
	})

	// Ensure source directories exist for valid mounts
	for _, bm := range testBindMounts {
		err := os.MkdirAll(bm.Source, core.DefaultDirOrExecPerm)
		require.NoError(t, err)
	}

	// Create workflow with valid mounts plus an invalid one
	invalidBindMount := mount.BindMount{
		Source: "/nonexistent/source/path",
		Target: "/nonexistent/target/path",
	}

	workflow, err := automa.NewWorkflowBuilder().WithId("test-rollback-on-failure").
		Steps(
			setupBindMount("kubernetes", "/etc/kubernetes"),
			setupBindMount("kubelet", "/var/lib/kubelet"),
			setupBindMount("invalid", invalidBindMount.Target), // This should fail
		).Build()
	require.NoError(t, err)

	//
	// When - Execute (should fail)
	//
	report := workflow.Execute(context.Background())

	//
	// Then
	//
	require.Error(t, report.Error)
	require.Equal(t, automa.StatusFailed, report.Status)

	// Verify that successful mounts were rolled back automatically
	for _, bm := range testBindMounts {
		mounted, fstabExists, err := mount.IsBindMountedWithFstab(bm)
		require.NoError(t, err)
		require.False(t, mounted, "Mount %s should be unmounted after automatic rollback", bm.Target)
		require.False(t, fstabExists, "Fstab entry for %s should be removed after automatic rollback", bm.Target)
	}
}

func Test_SetupBindMounts_RollbackIdempotent_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	//
	// Given
	//
	testBindMounts := []mount.BindMount{
		{Source: filepath.Join(core.Paths().SandboxDir, "/etc/kubernetes"), Target: "/etc/kubernetes"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/lib/kubelet"), Target: "/var/lib/kubelet"},
		{Source: filepath.Join(core.Paths().SandboxDir, "/var/run/cilium"), Target: "/var/run/cilium"},
	}

	// Cleanup before test
	cleanupBindMounts(t, testBindMounts)

	// Backup and restore fstab
	backupFstab := backupFstabFile(t)

	// Register cleanup and restore to run after test completes
	t.Cleanup(func() {
		cleanupBindMounts(t, testBindMounts)
		restoreFstabFile(t, backupFstab)
	})

	// Ensure source directories exist
	for _, bm := range testBindMounts {
		err := os.MkdirAll(bm.Source, core.DefaultDirOrExecPerm)
		require.NoError(t, err)
	}

	// Execute workflow
	workflow, err := SetupBindMounts().Build()
	require.NoError(t, err)

	report := workflow.Execute(context.Background())
	require.NoError(t, report.Error)

	// First rollback
	rollbackReport := workflow.Rollback(context.Background())
	require.NoError(t, rollbackReport.Error)

	//
	// When - Rollback again (idempotency test)
	//
	rollbackReport2 := workflow.Rollback(context.Background())

	//
	// Then
	//
	require.NoError(t, rollbackReport2.Error)
	require.Equal(t, automa.StatusSuccess, rollbackReport2.Status)

	// Verify all mounts are still removed
	for _, bm := range testBindMounts {
		mounted, fstabExists, err := mount.IsBindMountedWithFstab(bm)
		require.NoError(t, err)
		require.False(t, mounted)
		require.False(t, fstabExists)
	}
}

// Helper functions

func backupFstabFile(t *testing.T) string {
	t.Helper()

	backupPath := filepath.Join(t.TempDir(), "fstab.backup")
	cmd := exec.Command("cp", mount.DefaultFstabFile, backupPath)
	out, err := sudo(cmd).CombinedOutput()
	require.NoError(t, err, "Failed to backup fstab: %s", out)

	return backupPath
}

func restoreFstabFile(t *testing.T, backupPath string) {
	t.Helper()

	cmd := exec.Command("cp", backupPath, mount.DefaultFstabFile)
	out, err := sudo(cmd).CombinedOutput()
	assert.NoError(t, err, "Failed to restore fstab: %s", out)
}

func cleanupBindMounts(t *testing.T, mounts []mount.BindMount) {
	t.Helper()

	for _, bm := range mounts {
		// Try to unmount (ignore errors if not mounted)
		cmd := exec.Command("umount", "-l", bm.Target)
		_ = sudo(cmd)
		_ = mount.RemoveBindMountsWithFstab(bm)

		// Remove target directory if it exists
		_ = os.RemoveAll(bm.Target)
	}
}
