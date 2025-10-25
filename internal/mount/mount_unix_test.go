//go:build linux

package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
)

func Test_FstabEntry_String(t *testing.T) {
	entry := fstabEntry{
		source:  "/source/path",
		target:  "/target/path",
		fsType:  "none",
		options: "bind,nofail",
		dump:    "0",
		pass:    "0",
	}

	expected := "/source/path /target/path none bind,nofail 0 0"
	require.Equal(t, expected, entry.String())
}

func Test_parseFstabEntry(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected *fstabEntry
		wantNil  bool
	}{
		{
			name: "valid bind mount entry",
			line: "/source/path /target/path none bind,nofail 0 0",
			expected: &fstabEntry{
				source:  "/source/path",
				target:  "/target/path",
				fsType:  "none",
				options: "bind,nofail",
				dump:    "0",
				pass:    "0",
			},
		},
		{
			name: "valid entry without dump and pass",
			line: "/dev/sda1 /mnt ext4 defaults",
			expected: &fstabEntry{
				source:  "/dev/sda1",
				target:  "/mnt",
				fsType:  "ext4",
				options: "defaults",
				dump:    "0",
				pass:    "0",
			},
		},
		{
			name:    "comment line",
			line:    "# This is a comment",
			wantNil: true,
		},
		{
			name:    "empty line",
			line:    "",
			wantNil: true,
		},
		{
			name:    "whitespace only",
			line:    "   ",
			wantNil: true,
		},
		{
			name: "line with inline comment",
			line: "/source /target none bind 0 0 # comment",
			expected: &fstabEntry{
				source:  "/source",
				target:  "/target",
				fsType:  "none",
				options: "bind",
				dump:    "0",
				pass:    "0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := parseFstabEntry(tt.line)
			require.NoError(t, err)

			if tt.wantNil {
				require.Nil(t, entry)
			} else {
				require.NotNil(t, entry)
				require.Equal(t, tt.expected.source, entry.source)
				require.Equal(t, tt.expected.target, entry.target)
				require.Equal(t, tt.expected.fsType, entry.fsType)
				require.Equal(t, tt.expected.options, entry.options)
				require.Equal(t, tt.expected.dump, entry.dump)
				require.Equal(t, tt.expected.pass, entry.pass)
			}
		})
	}
}

func Test_readFstab(t *testing.T) {
	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")

	content := `# /etc/fstab: static file system information
#
# <file system> <mount point> <type> <options> <dump> <pass>
UUID=1234 / ext4 defaults 0 1
/swap.img none swap sw 0 0
/source/kubernetes /etc/kubernetes none bind,nofail 0 0
# Comment line
/source/kubelet /var/lib/kubelet none bind,nofail 0 0
`

	err := os.WriteFile(testFstab, []byte(content), core.DefaultFilePerm)
	require.NoError(t, err)

	entries, lines, err := readFstab(testFstab)
	require.NoError(t, err)
	require.NotNil(t, entries)
	require.NotNil(t, lines)

	// Should have parsed 4 valid entries (3 regular + 2 bind mounts)
	require.Len(t, entries, 4)

	// Check that lines include all original lines
	require.Len(t, lines, 8) // including comments and empty lines

	// Verify specific entries
	require.Equal(t, "/etc/kubernetes", entries[2].target)
	require.Equal(t, "bind,nofail", entries[2].options)

	require.Equal(t, "/var/lib/kubelet", entries[3].target)
	require.Equal(t, "bind,nofail", entries[3].options)
}

func Test_readFstab_NonExistent(t *testing.T) {
	tempDir := t.TempDir()
	nonExistentFile := filepath.Join(tempDir, "nonexistent")

	entries, lines, err := readFstab(nonExistentFile)
	require.NoError(t, err)
	require.Nil(t, entries)
	require.Nil(t, lines)
}

func Test_writeFstab(t *testing.T) {
	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")

	lines := []string{
		"# Test fstab",
		"/source /target none bind,nofail 0 0",
		"/source2 /target2 none bind,nofail 0 0",
	}

	err := writeFstab(testFstab, lines)
	require.NoError(t, err)

	// Read it back
	content, err := os.ReadFile(testFstab)
	require.NoError(t, err)

	expected := strings.Join(lines, "\n") + "\n"
	require.Equal(t, expected, string(content))

	// Check permissions
	info, err := os.Stat(testFstab)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(core.DefaultFilePerm), info.Mode().Perm())
}

func Test_addFstabEntry(t *testing.T) {
	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create initial fstab with one entry
	initialContent := `# Test fstab
/existing/source /existing/target none bind,nofail 0 0
`
	err := os.WriteFile(testFstab, []byte(initialContent), core.DefaultFilePerm)
	require.NoError(t, err)

	// Add a new entry
	newEntry := fstabEntry{
		source:  "/new/source",
		target:  "/new/target",
		fsType:  "none",
		options: "bind,nofail",
		dump:    "0",
		pass:    "0",
	}

	err = addFstabEntry(newEntry)
	require.NoError(t, err)

	// Verify the entry was added
	entries, _, err := readFstab(testFstab)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "/new/target", entries[1].target)

	// Try adding the same entry again - should not duplicate
	err = addFstabEntry(newEntry)
	require.NoError(t, err)

	entries, _, err = readFstab(testFstab)
	require.NoError(t, err)
	require.Len(t, entries, 2) // Should still be 2, not 3
}

func Test_addFstabEntry_NewFile(t *testing.T) {
	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab_new")
	fstabFile = testFstab

	// Add entry to non-existent file
	entry := fstabEntry{
		source:  "/source",
		target:  "/target",
		fsType:  "none",
		options: "bind,nofail",
		dump:    "0",
		pass:    "0",
	}

	err := addFstabEntry(entry)
	require.NoError(t, err)

	// Verify the entry was added
	entries, _, err := readFstab(testFstab)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "/target", entries[0].target)
}

func Test_addFstabEntry_PreventsDuplicates(t *testing.T) {
	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create initial fstab
	initialContent := `# Test fstab
/sandbox/etc/kubernetes /etc/kubernetes none bind,nofail 0 0
`
	err := os.WriteFile(testFstab, []byte(initialContent), core.DefaultFilePerm)
	require.NoError(t, err)

	// Try to add entry with same target but different source
	entry := fstabEntry{
		source:  "/different/source/etc/kubernetes",
		target:  "/etc/kubernetes",
		fsType:  "none",
		options: "bind,nofail",
		dump:    "0",
		pass:    "0",
	}

	err = addFstabEntry(entry)
	require.NoError(t, err)

	// Verify no duplicate was added
	entries, _, err := readFstab(testFstab)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	// Should keep the original source
	require.Equal(t, "/sandbox/etc/kubernetes", entries[0].source)
}

func Test_removeFstabEntry(t *testing.T) {
	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create initial fstab with multiple entries
	initialContent := `# Test fstab
/source1 /target1 none bind,nofail 0 0
/source2 /target2 none bind,nofail 0 0
/source3 /target3 none bind,nofail 0 0
`
	err := os.WriteFile(testFstab, []byte(initialContent), core.DefaultFilePerm)
	require.NoError(t, err)

	// Remove middle entry
	err = removeFstabEntry("/target2")
	require.NoError(t, err)

	// Verify entry was removed
	entries, lines, err := readFstab(testFstab)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "/target1", entries[0].target)
	require.Equal(t, "/target3", entries[1].target)

	// Verify comment line is preserved
	require.Contains(t, lines[0], "# Test fstab")
}

func Test_removeFstabEntry_NonExistent(t *testing.T) {
	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create initial fstab
	initialContent := `# Test fstab
/source1 /target1 none bind,nofail 0 0
`
	err := os.WriteFile(testFstab, []byte(initialContent), core.DefaultFilePerm)
	require.NoError(t, err)

	// Try to remove non-existent entry - should not error
	err = removeFstabEntry("/nonexistent")
	require.NoError(t, err)

	// Verify nothing was removed
	entries, _, err := readFstab(testFstab)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "/target1", entries[0].target)
}

func Test_removeFstabEntry_EmptyFile(t *testing.T) {
	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab_empty")
	fstabFile = testFstab

	// Try to remove from non-existent file - should not error
	err := removeFstabEntry("/target")
	require.NoError(t, err)
}

func Test_IsBindMountedWithFstab_BothExist(t *testing.T) {
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

	// Create fstab with entry
	initialContent := fmt.Sprintf(`# Test fstab
%s %s none bind,nofail 0 0
`, sourceDir, targetDir)
	err = os.WriteFile(testFstab, []byte(initialContent), core.DefaultFilePerm)
	require.NoError(t, err)

	mount := BindMount{
		Source: sourceDir,
		Target: targetDir,
	}

	// Test when directories exist but not mounted (on non-root systems)
	// This will return false for mounted since we can't actually mount without root
	alreadyMounted, fstabEntryExists, err := IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.False(t, alreadyMounted)  // Not actually mounted without root
	require.True(t, fstabEntryExists) // But fstab entry exists
}

func Test_IsBindMountedWithFstab_OnlyFstabEntryExists(t *testing.T) {
	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create only source directory (target doesn't exist)
	sourceDir := filepath.Join(tempDir, "source")
	targetDir := filepath.Join(tempDir, "target")

	err := os.MkdirAll(sourceDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create fstab with entry
	initialContent := fmt.Sprintf(`# Test fstab
%s %s none bind,nofail 0 0
`, sourceDir, targetDir)
	err = os.WriteFile(testFstab, []byte(initialContent), core.DefaultFilePerm)
	require.NoError(t, err)

	mount := BindMount{
		Source: sourceDir,
		Target: targetDir,
	}

	alreadyMounted, fstabEntryExists, err := IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.False(t, alreadyMounted) // Target doesn't exist, so not mounted
	require.True(t, fstabEntryExists)
}

func Test_IsBindMountedWithFstab_NeitherExists(t *testing.T) {
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

	mount := BindMount{
		Source: filepath.Join(tempDir, "nonexistent_source"),
		Target: filepath.Join(tempDir, "nonexistent_target"),
	}

	alreadyMounted, fstabEntryExists, err := IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.False(t, alreadyMounted)
	require.False(t, fstabEntryExists)
}

func Test_IsBindMountedWithFstab_DifferentSourceInFstab(t *testing.T) {
	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create directories
	sourceDir1 := filepath.Join(tempDir, "source1")
	sourceDir2 := filepath.Join(tempDir, "source2")
	targetDir := filepath.Join(tempDir, "target")

	err := os.MkdirAll(sourceDir1, core.DefaultDirOrExecPerm)
	require.NoError(t, err)
	err = os.MkdirAll(sourceDir2, core.DefaultDirOrExecPerm)
	require.NoError(t, err)
	err = os.MkdirAll(targetDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Create fstab with sourceDir1 -> targetDir
	initialContent := fmt.Sprintf(`# Test fstab
%s %s none bind,nofail 0 0
`, sourceDir1, targetDir)
	err = os.WriteFile(testFstab, []byte(initialContent), core.DefaultFilePerm)
	require.NoError(t, err)

	// Check with different source (sourceDir2)
	mount := BindMount{
		Source: sourceDir2,
		Target: targetDir,
	}

	alreadyMounted, fstabEntryExists, err := IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.False(t, alreadyMounted)
	require.False(t, fstabEntryExists) // Different source, so no match
}

func Test_IsBindMountedWithFstab_EmptyFstab(t *testing.T) {
	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create directories but no fstab file
	sourceDir := filepath.Join(tempDir, "source")
	targetDir := filepath.Join(tempDir, "target")

	err := os.MkdirAll(sourceDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)
	err = os.MkdirAll(targetDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	// Don't create fstab file at all

	mount := BindMount{
		Source: sourceDir,
		Target: targetDir,
	}

	alreadyMounted, fstabEntryExists, err := IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.False(t, alreadyMounted)
	require.False(t, fstabEntryExists)
}

func Test_IsBindMountedWithFstab_MultipleFstabEntries(t *testing.T) {
	// Save original fstabFile and restore after test
	originalFstabFile := fstabFile
	t.Cleanup(func() {
		fstabFile = originalFstabFile
	})

	tempDir := t.TempDir()
	testFstab := filepath.Join(tempDir, "fstab")
	fstabFile = testFstab

	// Create multiple source and target directories
	source1 := filepath.Join(tempDir, "source1")
	source2 := filepath.Join(tempDir, "source2")
	source3 := filepath.Join(tempDir, "source3")
	target1 := filepath.Join(tempDir, "target1")
	target2 := filepath.Join(tempDir, "target2")
	target3 := filepath.Join(tempDir, "target3")

	for _, dir := range []string{source1, source2, source3, target1, target2, target3} {
		err := os.MkdirAll(dir, core.DefaultDirOrExecPerm)
		require.NoError(t, err)
	}

	// Create fstab with multiple entries
	initialContent := fmt.Sprintf(`# Test fstab
%s %s none bind,nofail 0 0
%s %s none bind,nofail 0 0
%s %s none bind,nofail 0 0
`, source1, target1, source2, target2, source3, target3)
	err := os.WriteFile(testFstab, []byte(initialContent), core.DefaultFilePerm)
	require.NoError(t, err)

	// Check for the second mount
	mount := BindMount{
		Source: source2,
		Target: target2,
	}

	alreadyMounted, fstabEntryExists, err := IsBindMountedWithFstab(mount)
	require.NoError(t, err)
	require.False(t, alreadyMounted) // Not actually mounted without root
	require.True(t, fstabEntryExists)
}
