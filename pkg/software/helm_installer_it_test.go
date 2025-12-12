// SPDX-License-Identifier: Apache-2.0

//go:build integration

package software

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/stretchr/testify/require"
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

	// Verify downloaded files exist in the shared downloads folder
	files, err := os.ReadDir("/opt/solo/weaver/downloads")
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(files), 1, "There should be at least one file in the download directory")
	// Check that the downloaded helm file exists with expected name format by regex
	var foundHelm bool
	for _, file := range files {
		if regexp.MustCompile(`^helm-v[0-9]+\.[0-9]+\.[0-9]+-[^-]+-[^-]+\.tar\.gz$`).MatchString(file.Name()) {
			foundHelm = true
			break
		}
	}
	require.True(t, foundHelm, "Downloaded helm file should exist with expected pattern")

	//
	// When - Extract
	//
	err = installer.Extract()
	require.NoError(t, err, "Failed to extract helm")

	// Verify extraction directory exists and contains expected files
	extractedFiles, err := os.ReadDir("/opt/solo/weaver/tmp/helm/unpack")
	require.NoError(t, err)
	require.Greater(t, len(extractedFiles), 0, "Extraction directory should contain files")

	//
	// When - Install
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install helm")

	// Verify installation files exist in sandbox
	_, exists, err := fileManager.PathExists("/opt/solo/weaver/sandbox/bin/helm")
	require.NoError(t, err)
	require.True(t, exists, "helm binary should exist in sandbox bin directory")

	// Check binary permissions (should be executable)
	info, err := os.Stat("/opt/solo/weaver/sandbox/bin/helm")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm(), "helm binary should have 0755 permissions")

	//
	// When - Cleanup
	//
	err = installer.Cleanup()
	require.NoError(t, err, "Failed to cleanup helm installation")

	// Check download folder is cleaned up
	_, exists, err = fileManager.PathExists("/opt/solo/weaver/tmp/helm")
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
	require.Equal(t, "/opt/solo/weaver/sandbox/bin/helm", linkTarget, "symlink should point to sandbox binary")

}
