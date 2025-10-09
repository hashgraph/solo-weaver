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

	err := os.MkdirAll(destDir, core.DefaultFilePerm)
	require.NoError(t, err)

	srcFiles, err := ReadDir(sysctlConfigSourceDir)
	require.NoError(t, err)

	copied, err := CopyFiles(sysctlConfigSourceDir, destDir)
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

	// Patch global vars
	origDst := sysctlConfigDestinationDir
	sysctlConfigDestinationDir = dstDir
	defer func() {
		sysctlConfigDestinationDir = origDst
	}()

	srcFiles, err := ReadDir(sysctlConfigSourceDir)
	require.NoError(t, err)

	copied, err := CopyFiles(sysctlConfigSourceDir, dstDir)
	require.NoError(t, err)
	require.NotEmpty(t, copied)
	require.Equal(t, len(copied), len(srcFiles))

	// Remove files
	removed, err := RemoveSysctlConfigurationFiles()
	require.NoError(t, err)
	require.NotEmpty(t, removed)
	require.Equal(t, len(removed), len(copied)) // Ensure all files are reported as removed

	// Assert files are removed
	files, err := os.ReadDir(dstDir)
	require.NoError(t, err)
	require.Len(t, files, 0)
}
