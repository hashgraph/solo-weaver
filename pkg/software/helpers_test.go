// SPDX-License-Identifier: Apache-2.0

package software

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	tmpFolder = "/opt/solo/weaver/tmp"
)

// resetTestEnvironment creates a clean test environment and registers cleanup
func resetTestEnvironment(t *testing.T) {
	t.Helper()

	// Clean up any existing test artifacts
	_ = os.RemoveAll("/opt/solo/weaver")

	// Clean up any leftover symbolic links in SystemBinDir from previous test runs
	// This is critical because IsInstalled() and IsConfigured() check these directories
	cleanupSystemBinDir(t)

	// Register cleanup to run after test completes
	t.Cleanup(func() {
		_ = os.RemoveAll("/opt/solo/weaver")
		cleanupSystemBinDir(t)
	})
}

// cleanupSystemBinDir removes any symbolic links in /usr/local/bin that point to /opt/solo/weaver
// This prevents test pollution between runs
func cleanupSystemBinDir(t *testing.T) {
	t.Helper()

	systemBinDir := "/usr/local/bin"
	weaverPrefix := "/opt/solo/weaver"

	// Read the system bin directory
	entries, err := os.ReadDir(systemBinDir)
	if err != nil {
		// Directory might not exist or not be readable, that's OK for tests
		return
	}

	// Check each entry to see if it's a symlink pointing to weaver directories
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink == 0 {
			continue // Not a symlink, skip
		}

		linkPath := filepath.Join(systemBinDir, entry.Name())
		target, err := os.Readlink(linkPath)
		if err != nil {
			continue // Can't read symlink, skip
		}

		// If the symlink points to a weaver directory, remove it
		if strings.HasPrefix(target, weaverPrefix) {
			_ = os.Remove(linkPath)
		}
	}
}

// createTestTarGz creates a .tar.gz file at outputPath containing the specified files.
// If files is nil, it creates a default set of files.
// It returns the SHA256 checksum of the created archive and a map of individual file checksums.
func createTestTarGz(outputPath string, files map[string]string) (checksum string, fileChecksums map[string]string, err error) {
	// Use default files if none provided
	if files == nil {
		files = map[string]string{
			"bin/test-binary": "#!/bin/bash\necho 'test binary'\n",
			"config.conf":     "config_option=value\n",
		}
	}

	fileChecksums = make(map[string]string)

	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return "", nil, err
	}
	defer outFile.Close()

	// Create gzip writer
	gw := gzip.NewWriter(outFile)
	defer gw.Close()

	// Create tar writer
	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Write each file into the tar archive
	for name, content := range files {
		data := []byte(content)

		// Calculate SHA256 for this file
		hash := sha256.New()
		hash.Write(data)
		fileChecksums[name] = hex.EncodeToString(hash.Sum(nil))

		hdr := &tar.Header{
			Name:    name,
			Mode:    0o644,
			Size:    int64(len(data)),
			ModTime: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return "", nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return "", nil, err
		}
	}

	// Flush all writers
	if err := tw.Close(); err != nil {
		return "", nil, err
	}
	if err := gw.Close(); err != nil {
		return "", nil, err
	}
	if err := outFile.Close(); err != nil {
		return "", nil, err
	}

	// Reopen to calculate checksum
	f, err := os.Open(outputPath)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()

	archiveHash := sha256.New()
	if _, err := io.Copy(archiveHash, f); err != nil {
		return "", nil, err
	}

	return hex.EncodeToString(archiveHash.Sum(nil)), fileChecksums, nil
}
