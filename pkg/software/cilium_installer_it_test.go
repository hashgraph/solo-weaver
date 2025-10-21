//go:build integration

package software

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

func Test_CiliumInstaller_FullWorkflow_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewCiliumInstaller()
	require.NoError(t, err, "Failed to create cilium installer")

	fileManager, err := fsx.NewManager()
	require.NoError(t, err)

	//
	// When - Download
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download cilium and/or its configuration")

	// Verify downloaded files exist
	files, err := os.ReadDir("/opt/provisioner/tmp/cilium")
	require.NoError(t, err)

	require.Equal(t, 1, len(files), "There should be exactly one file in the download directory")
	// Check that the downloaded file has the expected name format by regex
	require.Regexp(t,
		regexp.MustCompile(`^cilium-[^-]+-[^-]+\.tar\.gz$`),
		files[0].Name(),
		"Downloaded file name should match expected pattern",
	)

	//
	// When - Extract
	//
	err = installer.Extract()
	require.NoError(t, err, "Failed to extract cilium")

	// Verify extraction directory exists and contains expected files
	extractedFiles, err := os.ReadDir("/opt/provisioner/tmp/cilium/unpack")
	require.NoError(t, err)
	require.Greater(t, len(extractedFiles), 0, "Extraction directory should contain files")

	//
	// When - Install
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install cilium")

	// Verify installation files exist in sandbox
	_, exists, err := fileManager.PathExists("/opt/provisioner/sandbox/bin/cilium")
	require.NoError(t, err)
	require.True(t, exists, "cilium binary should exist in sandbox bin directory")

	// Check binary permissions (should be executable)
	info, err := os.Stat("/opt/provisioner/sandbox/bin/cilium")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm(), "cilium binary should have 0755 permissions")

	//
	// When - Cleanup
	//
	err = installer.Cleanup()
	require.NoError(t, err, "Failed to cleanup cilium installation")

	// Check download folder is cleaned up
	_, exists, err = fileManager.PathExists("/opt/provisioner/tmp/cilium")
	require.NoError(t, err)
	require.False(t, exists, "cilium download temp folder should be cleaned up after installation")

	//
	// When - Configure
	//
	err = installer.Configure()
	require.NoError(t, err, "Failed to configure cilium")

	// Verify system-wide symlink exists
	_, exists, err = fileManager.PathExists("/usr/local/bin/cilium")
	require.NoError(t, err)
	require.True(t, exists, "cilium symlink should exist in /usr/local/bin")

	// Verify it's actually a symlink pointing to the sandbox binary
	linkTarget, err := os.Readlink("/usr/local/bin/cilium")
	require.NoError(t, err)
	require.Equal(t, "/opt/provisioner/sandbox/bin/cilium", linkTarget, "symlink should point to sandbox binary")

}
