//go:build integration

package software

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

func Test_HelmInstaller_FullWorkflow_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewHelmInstaller()
	require.NoError(t, err, "Failed to create helm installer")

	fileManager, err := fsx.NewManager()
	require.NoError(t, err)

	//
	// When - Download
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download helm and/or its configuration")

	// Verify downloaded files exist
	files, err := os.ReadDir("/opt/provisioner/tmp/helm")
	require.NoError(t, err)

	require.Equal(t, 1, len(files), "There should be exactly one file in the download directory")
	// Check that the downloaded file has the expected name format by regex
	require.Regexp(t,
		regexp.MustCompile(`^helm-v[0-9]+\.[0-9]+\.[0-9]+-[^-]+-[^-]+\.tar\.gz$`),
		files[0].Name(),
		"Downloaded file name should match expected pattern",
	)

	//
	// When - Extract
	//
	err = installer.Extract()
	require.NoError(t, err, "Failed to extract helm")

	// Verify extraction directory exists and contains expected files
	extractedFiles, err := os.ReadDir("/opt/provisioner/tmp/helm/unpack")
	require.NoError(t, err)
	require.Greater(t, len(extractedFiles), 0, "Extraction directory should contain files")

	//
	// When - Install
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install helm")

	// Verify installation files exist in sandbox
	_, exists, err := fileManager.PathExists("/opt/provisioner/sandbox/bin/helm")
	require.NoError(t, err)
	require.True(t, exists, "helm binary should exist in sandbox bin directory")

	// Check binary permissions (should be executable)
	info, err := os.Stat("/opt/provisioner/sandbox/bin/helm")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm(), "helm binary should have 0755 permissions")

	//
	// When - Cleanup
	//
	err = installer.Cleanup()
	require.NoError(t, err, "Failed to cleanup helm installation")

	// Check download folder is cleaned up
	_, exists, err = fileManager.PathExists("/opt/provisioner/tmp/helm")
	require.NoError(t, err)
	require.False(t, exists, "helm download temp folder should be cleaned up after installation")

	//
	// When - Configure
	//
	err = installer.Configure()
	require.NoError(t, err, "Failed to configure helm")

	// Verify system-wide symlink exists
	_, exists, err = fileManager.PathExists("/usr/local/bin/helm")
	require.NoError(t, err)
	require.True(t, exists, "helm symlink should exist in /usr/local/bin")

	// Verify it's actually a symlink pointing to the sandbox binary
	linkTarget, err := os.Readlink("/usr/local/bin/helm")
	require.NoError(t, err)
	require.Equal(t, "/opt/provisioner/sandbox/bin/helm", linkTarget, "symlink should point to sandbox binary")

}
