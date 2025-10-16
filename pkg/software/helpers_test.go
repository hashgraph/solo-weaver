package software

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"testing"
	"time"
)

const (
	tmpFolder = "/opt/provisioner/tmp"
)

// resetTestEnvironment creates a clean test environment and registers cleanup
func resetTestEnvironment(t *testing.T) {
	t.Helper()

	// Clean up any existing test artifacts
	_ = os.RemoveAll("/opt/provisioner")

	// Register cleanup to run after test completes
	t.Cleanup(func() {
		_ = os.RemoveAll("/opt/provisioner")
	})
}

// createTestTarGz creates a .tar.gz file at outputPath containing the specified files.
// If files is nil, it creates a default set of files.
// It returns the SHA256 checksum of the created archive.
func createTestTarGz(outputPath string, files map[string]string) (checksum string, err error) {
	// Use default files if none provided
	if files == nil {
		files = map[string]string{
			"test-binary": "#!/bin/bash\necho 'test binary'\n",
			"config.conf": "config_option=value\n",
		}
	}

	// Create output file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return "", err
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
		hdr := &tar.Header{
			Name:    name,
			Mode:    0o644,
			Size:    int64(len(data)),
			ModTime: time.Now(),
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return "", err
		}
		if _, err := tw.Write(data); err != nil {
			return "", err
		}
	}

	// Flush all writers
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gw.Close(); err != nil {
		return "", err
	}
	if err := outFile.Close(); err != nil {
		return "", err
	}

	// Reopen to calculate checksum
	f, err := os.Open(outputPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
