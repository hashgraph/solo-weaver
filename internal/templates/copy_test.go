package templates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
)

func TestCopyConfigurationFiles(t *testing.T) {
	tmpDir := t.TempDir()
	destDir := filepath.Join(tmpDir, "sysctl.d")
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	err := os.MkdirAll(destDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err)

	srcDir := "files/sysctl" // Ensure this points to the correct embedded files location
	srcFiles, err := ReadDir(srcDir)
	require.NoError(t, err)

	copied, err := CopyTemplateFiles(srcDir, destDir)
	require.NoError(t, err)
	require.NotEmpty(t, copied)
	require.Equal(t, len(copied), len(srcFiles))

	// Check that at least one file was copied.
	files, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("failed to read dest dir: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no files copied to dest dir")
	}

	// Optionally, verify file contents match the template.
	for _, f := range files {
		destPath := filepath.Join(destDir, f.Name())
		info, err := os.Stat(destPath)
		if err != nil {
			t.Errorf("file %s not found: %v", destPath, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("file %s is empty", destPath)
		}
	}
}

func TestRemoveSysctlConfigurationFiles(t *testing.T) {
	// Setup: use temp dirs for source and destination
	tmpDir := t.TempDir()
	dstDir := filepath.Join(tmpDir, "dst")
	require.NoError(t, os.MkdirAll(dstDir, 0o755))

	srcDir := "files/sysctl" // Ensure this points to the correct embedded files location
	srcFiles, err := ReadDir(srcDir)
	require.NoError(t, err)

	copied, err := CopyTemplateFiles(srcDir, dstDir)
	require.NoError(t, err)
	require.NotEmpty(t, copied)
	require.Equal(t, len(copied), len(srcFiles))

	// Remove files
	removed, err := RemoveTemplateFiles(srcDir, dstDir)
	require.NoError(t, err)
	require.NotEmpty(t, removed)
	require.Equal(t, len(removed), len(copied)) // Ensure all files are reported as removed

	// Assert files are removed
	files, err := os.ReadDir(dstDir)
	require.NoError(t, err)
	require.Len(t, files, 0)
}
