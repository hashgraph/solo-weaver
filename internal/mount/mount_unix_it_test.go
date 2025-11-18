//go:build integration

package mount

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/core"
)

func Test_SetupBindMountsWithFstab_CompleteWorkflow_Integration(t *testing.T) {
	//
	// Given
	//

	// This test simulates the complete workflow from the shell script
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create fstab file
	err := os.WriteFile(testFstab, []byte("# Test fstab\n"), core.DefaultFilePerm)
	require.NoError(t, err)

	// Create sandbox directory
	sourceDir := filepath.Join(tempDir, "source")
	err = os.MkdirAll(sourceDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create target parent directory
	parentTargetDir := filepath.Join(tempDir, "target")
	err = os.MkdirAll(parentTargetDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create BindMounts
	bindMounts := []BindMount{
		{Source: path.Join(sourceDir, "from_kubernetes"), Target: path.Join(parentTargetDir, "to_kubernetes")},
		{Source: path.Join(sourceDir, "from_kubelet"), Target: path.Join(parentTargetDir, "to_kubelet")},
		{Source: path.Join(sourceDir, "from_cilium"), Target: path.Join(parentTargetDir, "to_cilium")},
	}

	// Create source directories
	for _, mount := range bindMounts {
		err := os.MkdirAll(mount.Source, core.DefaultDirOrExecPerm)
		require.NoError(t, err)
	}

	// Create sample files in source directories
	err = os.WriteFile(path.Join(bindMounts[0].Source, "kube_file.txt"), []byte("kubernetes data"), core.DefaultFilePerm)
	require.NoError(t, err)
	err = os.WriteFile(path.Join(bindMounts[1].Source, "kubelet_file.txt"), []byte("kubelet data"), core.DefaultFilePerm)
	require.NoError(t, err)
	err = os.WriteFile(path.Join(bindMounts[2].Source, "cilium_file.txt"), []byte("cilium data"), core.DefaultFilePerm)
	require.NoError(t, err)

	// Cleanup: unmount after test
	t.Cleanup(func() {
		for _, mount := range bindMounts {
			targetMountPath := path.Join(mount.Target)
			_ = exec.Command("umount", targetMountPath).Run()
		}
	})

	//
	// When
	//

	// Call SetupBindMountsWithFstab for each mount (updated to accept single BindMount)
	for _, mount := range bindMounts {
		err = SetupBindMountsWithFstab(mount)
		require.NoError(t, err)
	}

	//
	// Then
	//

	// Verify fstab entries were created
	entries, lines, err := readFstab(testFstab)
	require.NoError(t, err)
	require.NotNil(t, entries)
	require.NotNil(t, lines)
	require.Len(t, entries, len(bindMounts))

	// Check that lines include all original lines
	require.Len(t, lines, len(bindMounts)+1) // +1 for the initial comment line

	//Verify specific entries
	for i, mount := range bindMounts {
		require.Equal(t, mount.Source, entries[i].source)
		require.Equal(t, mount.Target, entries[i].target)
		require.Equal(t, "none", entries[i].fsType)
		require.Equal(t, "bind,nofail", entries[i].options)
		require.Equal(t, "0", entries[i].dump)
		require.Equal(t, "0", entries[i].pass)
	}

	// Verify mounts were created
	for _, mount := range bindMounts {
		// Check if directory exists
		_, err := os.Stat(mount.Target)
		require.NoError(t, err)
	}

	// Verify files in mounted directories
	data, err := os.ReadFile(path.Join(bindMounts[0].Target, "kube_file.txt"))
	require.NoError(t, err)
	require.Equal(t, "kubernetes data", string(data))

	data, err = os.ReadFile(path.Join(bindMounts[1].Target, "kubelet_file.txt"))
	require.NoError(t, err)
	require.Equal(t, "kubelet data", string(data))

	data, err = os.ReadFile(path.Join(bindMounts[2].Target, "cilium_file.txt"))
	require.NoError(t, err)
	require.Equal(t, "cilium data", string(data))

	// Run again - should be idempotent
	for _, mount := range bindMounts {
		err = SetupBindMountsWithFstab(mount)
		require.NoError(t, err)
	}

	// Verify mounts still exist
	for _, mount := range bindMounts {
		// Check if directory exists
		_, err := os.Stat(mount.Target)
		require.NoError(t, err)
	}

	// Verify files in mounted directories again
	data, err = os.ReadFile(path.Join(bindMounts[0].Target, "kube_file.txt"))
	require.NoError(t, err)
	require.Equal(t, "kubernetes data", string(data))

	data, err = os.ReadFile(path.Join(bindMounts[1].Target, "kubelet_file.txt"))
	require.NoError(t, err)
	require.Equal(t, "kubelet data", string(data))

	data, err = os.ReadFile(path.Join(bindMounts[2].Target, "cilium_file.txt"))
	require.NoError(t, err)
	require.Equal(t, "cilium data", string(data))

	// Verify no duplicates in fstab
	entries, lines, err = readFstab(testFstab)
	require.NoError(t, err)
	require.NotNil(t, entries)
	require.NotNil(t, lines)
	require.Len(t, entries, len(bindMounts))
}

func Test_RollbackBindMountsWithFstab_CompleteWorkflow_Integration(t *testing.T) {
	//
	// Given
	//

	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create fstab file
	err := os.WriteFile(testFstab, []byte("# Test fstab\n"), core.DefaultFilePerm)
	require.NoError(t, err)

	// Create source and target directories
	sourceDir := filepath.Join(tempDir, "source")
	targetDir := filepath.Join(tempDir, "target")

	err = os.MkdirAll(sourceDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create a sample file in source
	testFile := filepath.Join(sourceDir, "test_file.txt")
	err = os.WriteFile(testFile, []byte("test data"), core.DefaultFilePerm)
	require.NoError(t, err)

	mount := BindMount{
		Source: sourceDir,
		Target: targetDir,
	}

	// Setup the bind mount
	err = SetupBindMountsWithFstab(mount)
	require.NoError(t, err)

	// Verify mount was created
	mounted, err := isBindMounted(mount)
	require.NoError(t, err)
	require.True(t, mounted)

	// Verify fstab entry exists
	entries, _, err := readFstab(testFstab)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, targetDir, entries[0].target)

	// Verify file is accessible through mount
	data, err := os.ReadFile(filepath.Join(targetDir, "test_file.txt"))
	require.NoError(t, err)
	require.Equal(t, "test data", string(data))

	//
	// When
	//

	// Call RemoveBindMountsWithFstab
	err = RemoveBindMountsWithFstab(mount)

	//
	// Then
	//

	require.NoError(t, err)

	// Verify mount was unmounted
	mounted, err = isBindMounted(mount)
	require.NoError(t, err)
	require.False(t, mounted)

	// Verify fstab entry was removed
	entries, _, err = readFstab(testFstab)
	require.NoError(t, err)
	require.Len(t, entries, 0)

	// Verify file is no longer accessible through target (since unmounted)
	_, err = os.ReadFile(filepath.Join(targetDir, "test_file.txt"))
	require.Error(t, err) // Should error because mount is gone
}

func Test_RollbackBindMountsWithFstab_Idempotent_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create fstab file
	err := os.WriteFile(testFstab, []byte("# Test fstab\n"), core.DefaultFilePerm)
	require.NoError(t, err)

	// Create source and target directories
	sourceDir := filepath.Join(tempDir, "source")
	targetDir := filepath.Join(tempDir, "target")

	err = os.MkdirAll(sourceDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	mount := BindMount{
		Source: sourceDir,
		Target: targetDir,
	}

	// Setup and rollback
	err = SetupBindMountsWithFstab(mount)
	require.NoError(t, err)

	err = RemoveBindMountsWithFstab(mount)
	require.NoError(t, err)

	// Call rollback again - should be idempotent
	err = RemoveBindMountsWithFstab(mount)
	require.NoError(t, err)

	// Verify still unmounted
	mounted, err := isBindMounted(mount)
	require.NoError(t, err)
	require.False(t, mounted)

	// Verify fstab still clean
	entries, _, err := readFstab(testFstab)
	require.NoError(t, err)
	require.Len(t, entries, 0)
}

func Test_SetupAndRollback_MultipleMounts_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create fstab file
	err := os.WriteFile(testFstab, []byte("# Test fstab\n"), core.DefaultFilePerm)
	require.NoError(t, err)

	// Create multiple bind mounts
	mounts := []BindMount{
		{Source: filepath.Join(tempDir, "source1"), Target: filepath.Join(tempDir, "target1")},
		{Source: filepath.Join(tempDir, "source2"), Target: filepath.Join(tempDir, "target2")},
		{Source: filepath.Join(tempDir, "source3"), Target: filepath.Join(tempDir, "target3")},
	}

	// Create source directories
	for _, mount := range mounts {
		err = os.MkdirAll(mount.Source, core.DefaultDirOrExecPerm)
		require.NoError(t, err)
	}

	// Setup all mounts
	for _, mount := range mounts {
		err = SetupBindMountsWithFstab(mount)
		require.NoError(t, err)
	}

	// Verify all are mounted
	for _, mount := range mounts {
		mounted, err := isBindMounted(mount)
		require.NoError(t, err)
		require.True(t, mounted)
	}

	// Verify fstab has all entries
	entries, _, err := readFstab(testFstab)
	require.NoError(t, err)
	require.Len(t, entries, 3)

	// Rollback the middle mount
	err = RemoveBindMountsWithFstab(mounts[1])
	require.NoError(t, err)

	// Verify middle mount is unmounted
	mounted, err := isBindMounted(mounts[1])
	require.NoError(t, err)
	require.False(t, mounted)

	// Verify other mounts are still mounted
	mounted, err = isBindMounted(mounts[0])
	require.NoError(t, err)
	require.True(t, mounted)

	mounted, err = isBindMounted(mounts[2])
	require.NoError(t, err)
	require.True(t, mounted)

	// Verify fstab has 2 entries
	entries, _, err = readFstab(testFstab)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, mounts[0].Target, entries[0].target)
	require.Equal(t, mounts[2].Target, entries[1].target)

	// Cleanup remaining mounts
	t.Cleanup(func() {
		_ = RemoveBindMountsWithFstab(mounts[0])
		_ = RemoveBindMountsWithFstab(mounts[2])
	})
}

func Test_IsBindMountedWithFstab_NotMountedNoFstab_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create empty fstab
	err := os.WriteFile(testFstab, []byte("# Test fstab\n"), core.DefaultFilePerm)
	require.NoError(t, err)

	// Create source and target directories
	sourceDir := filepath.Join(tempDir, "source")
	targetDir := filepath.Join(tempDir, "target")

	err = os.MkdirAll(sourceDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)
	err = os.MkdirAll(targetDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	mount := BindMount{
		Source: sourceDir,
		Target: targetDir,
	}

	// Check status - should be neither mounted nor in fstab
	alreadyMounted, fstabEntryExists, err := IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.False(t, alreadyMounted)
	require.False(t, fstabEntryExists)
}

func Test_IsBindMountedWithFstab_MountedWithFstab_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create empty fstab
	err := os.WriteFile(testFstab, []byte("# Test fstab\n"), core.DefaultFilePerm)
	require.NoError(t, err)

	// Create source and target directories
	sourceDir := filepath.Join(tempDir, "source")
	targetDir := filepath.Join(tempDir, "target")

	err = os.MkdirAll(sourceDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	mount := BindMount{
		Source: sourceDir,
		Target: targetDir,
	}

	// Setup the bind mount (which creates mount AND fstab entry)
	err = SetupBindMountsWithFstab(mount)
	require.NoError(t, err)

	// Cleanup: unmount after test
	t.Cleanup(func() {
		_ = exec.Command("umount", targetDir).Run()
	})

	// Check status - should be BOTH mounted AND in fstab
	alreadyMounted, fstabEntryExists, err := IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.True(t, alreadyMounted)
	require.True(t, fstabEntryExists)
}

func Test_IsBindMountedWithFstab_MountedWithoutFstab_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create empty fstab
	err := os.WriteFile(testFstab, []byte("# Test fstab\n"), core.DefaultFilePerm)
	require.NoError(t, err)

	// Create source and target directories
	sourceDir := filepath.Join(tempDir, "source")
	targetDir := filepath.Join(tempDir, "target")

	err = os.MkdirAll(sourceDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)
	err = os.MkdirAll(targetDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	mount := BindMount{
		Source: sourceDir,
		Target: targetDir,
	}

	// Manually mount without updating fstab (using setupBindMount directly)
	err = setupBindMount(mount)
	require.NoError(t, err)

	// Cleanup: unmount after test
	t.Cleanup(func() {
		_ = exec.Command("umount", targetDir).Run()
	})

	// Check status - should be mounted but NOT in fstab
	alreadyMounted, fstabEntryExists, err := IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.True(t, alreadyMounted)
	require.False(t, fstabEntryExists)
}

func Test_IsBindMountedWithFstab_NotMountedButInFstab_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create source and target directories
	sourceDir := filepath.Join(tempDir, "source")
	targetDir := filepath.Join(tempDir, "target")

	err := os.MkdirAll(sourceDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)
	err = os.MkdirAll(targetDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	mount := BindMount{
		Source: sourceDir,
		Target: targetDir,
	}

	// Add fstab entry manually without mounting
	entry := fstabEntry{
		source:  sourceDir,
		target:  targetDir,
		fsType:  "none",
		options: "bind,nofail",
		dump:    "0",
		pass:    "0",
	}
	err = os.WriteFile(testFstab, []byte("# Test fstab\n"), core.DefaultFilePerm)
	require.NoError(t, err)
	err = addFstabEntry(entry)
	require.NoError(t, err)

	// Check status - should be in fstab but NOT mounted
	alreadyMounted, fstabEntryExists, err := IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.False(t, alreadyMounted)
	require.True(t, fstabEntryExists)
}

func Test_IsBindMountedWithFstab_AfterUnmount_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create empty fstab
	err := os.WriteFile(testFstab, []byte("# Test fstab\n"), core.DefaultFilePerm)
	require.NoError(t, err)

	// Create source and target directories
	sourceDir := filepath.Join(tempDir, "source")
	targetDir := filepath.Join(tempDir, "target")

	err = os.MkdirAll(sourceDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	mount := BindMount{
		Source: sourceDir,
		Target: targetDir,
	}

	// Setup the bind mount
	err = SetupBindMountsWithFstab(mount)
	require.NoError(t, err)

	// Cleanup: ensure unmount after test
	t.Cleanup(func() {
		_ = exec.Command("umount", targetDir).Run()
	})

	// Verify it's mounted and in fstab
	alreadyMounted, fstabEntryExists, err := IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.True(t, alreadyMounted)
	require.True(t, fstabEntryExists)

	// Now unmount it (but leave fstab entry)
	err = unmountBindMount(mount)
	require.NoError(t, err)

	// Check status again - should NOT be mounted but still in fstab
	alreadyMounted, fstabEntryExists, err = IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.False(t, alreadyMounted)
	require.True(t, fstabEntryExists)
}

func Test_IsBindMountedWithFstab_MultipleBindMounts_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create empty fstab
	err := os.WriteFile(testFstab, []byte("# Test fstab\n"), core.DefaultFilePerm)
	require.NoError(t, err)

	// Create multiple bind mounts
	mount1 := BindMount{
		Source: filepath.Join(tempDir, "source1"),
		Target: filepath.Join(tempDir, "target1"),
	}
	mount2 := BindMount{
		Source: filepath.Join(tempDir, "source2"),
		Target: filepath.Join(tempDir, "target2"),
	}
	mount3 := BindMount{
		Source: filepath.Join(tempDir, "source3"),
		Target: filepath.Join(tempDir, "target3"),
	}

	// Create all source directories
	for _, m := range []BindMount{mount1, mount2, mount3} {
		err := os.MkdirAll(m.Source, core.DefaultDirOrExecPerm)
		require.NoError(t, err)
	}

	// Setup first two mounts completely
	err = SetupBindMountsWithFstab(mount1)
	require.NoError(t, err)
	err = SetupBindMountsWithFstab(mount2)
	require.NoError(t, err)

	// Setup third mount only in fstab (no actual mount)
	err = os.MkdirAll(mount3.Target, core.DefaultDirOrExecPerm)
	require.NoError(t, err)
	entry3 := fstabEntry{
		source:  mount3.Source,
		target:  mount3.Target,
		fsType:  "none",
		options: "bind,nofail",
		dump:    "0",
		pass:    "0",
	}
	err = addFstabEntry(entry3)
	require.NoError(t, err)

	// Cleanup: unmount after test
	t.Cleanup(func() {
		_ = exec.Command("umount", mount1.Target).Run()
		_ = exec.Command("umount", mount2.Target).Run()
		_ = exec.Command("umount", mount3.Target).Run()
	})

	// Check mount1 - should be both mounted and in fstab
	alreadyMounted1, fstabEntryExists1, err := IsBindMountedWithFstab(mount1)
	require.NoError(t, err)
	require.True(t, alreadyMounted1)
	require.True(t, fstabEntryExists1)

	// Check mount2 - should be both mounted and in fstab
	alreadyMounted2, fstabEntryExists2, err := IsBindMountedWithFstab(mount2)
	require.NoError(t, err)
	require.True(t, alreadyMounted2)
	require.True(t, fstabEntryExists2)

	// Check mount3 - should be in fstab but NOT mounted
	alreadyMounted3, fstabEntryExists3, err := IsBindMountedWithFstab(mount3)
	require.NoError(t, err)
	require.False(t, alreadyMounted3)
	require.True(t, fstabEntryExists3)
}
