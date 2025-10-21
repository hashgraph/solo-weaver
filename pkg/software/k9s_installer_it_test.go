//go:build integration

package software

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

func Test_K9sInstaller_FullWorkflow_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewK9sInstaller()
	require.NoError(t, err, "Failed to create k9s installer")

	fileManager, err := fsx.NewManager()
	require.NoError(t, err)

	//
	// When - Download
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download k9s and/or its configuration")

	// Verify downloaded files exist
	files, err := os.ReadDir("/opt/provisioner/tmp/k9s")
	require.NoError(t, err)

	require.Equal(t, 1, len(files), "There should be exactly one file in the download directory")
	// Check that the downloaded file has the expected name format by regex
	require.Regexp(t,
		regexp.MustCompile(`^k9s_[^-]+_[^-]+\.tar\.gz$`),
		files[0].Name(),
		"Downloaded file name should match expected pattern",
	)

	//
	// When - Extract
	//
	err = installer.Extract()
	require.NoError(t, err, "Failed to extract k9s")

	// Verify extraction directory exists and contains expected files
	extractedFiles, err := os.ReadDir("/opt/provisioner/tmp/k9s/unpack")
	require.NoError(t, err)
	require.Greater(t, len(extractedFiles), 0, "Extraction directory should contain files")

	//
	// When - Install
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install k9s")

	// Verify installation files exist in sandbox
	_, exists, err := fileManager.PathExists("/opt/provisioner/sandbox/bin/k9s")
	require.NoError(t, err)
	require.True(t, exists, "k9s binary should exist in sandbox bin directory")

	// Check binary permissions (should be executable)
	info, err := os.Stat("/opt/provisioner/sandbox/bin/k9s")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm(), "k9s binary should have 0755 permissions")

	//
	// When - Cleanup
	//
	err = installer.Cleanup()
	require.NoError(t, err, "Failed to cleanup k9s installation")

	// Check download folder is cleaned up
	_, exists, err = fileManager.PathExists("/opt/provisioner/tmp/k9s")
	require.NoError(t, err)
	require.False(t, exists, "k9s download temp folder should be cleaned up after installation")

	//
	// When - Configure
	//
	err = installer.Configure()
	require.NoError(t, err, "Failed to configure k9s")

	// Verify system-wide symlink exists
	_, exists, err = fileManager.PathExists("/usr/local/bin/k9s")
	require.NoError(t, err)
	require.True(t, exists, "k9s symlink should exist in /usr/local/bin")

	// Verify it's actually a symlink pointing to the sandbox binary
	linkTarget, err := os.Readlink("/usr/local/bin/k9s")
	require.NoError(t, err)
	require.Equal(t, "/opt/provisioner/sandbox/bin/k9s", linkTarget, "symlink should point to sandbox binary")

}
