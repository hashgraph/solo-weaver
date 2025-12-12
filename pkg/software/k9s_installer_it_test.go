// SPDX-License-Identifier: Apache-2.0

//go:build integration

package software

import (
	"os"
	"regexp"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/stretchr/testify/require"
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
	files, err := os.ReadDir("/opt/solo/weaver/downloads")
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(files), 1, "There should be at least one file in the download directory")
	// Check that the downloaded k9s file exists with expected name format by regex
	var foundK9s bool
	for _, file := range files {
		if regexp.MustCompile(`^k9s_[^-]+_[^-]+\.tar\.gz$`).MatchString(file.Name()) {
			foundK9s = true
			break
		}
	}
	require.True(t, foundK9s, "Downloaded k9s file should exist with expected pattern")

	//
	// When - Extract
	//
	err = installer.Extract()
	require.NoError(t, err, "Failed to extract k9s")

	// Verify extraction directory exists and contains expected files
	extractedFiles, err := os.ReadDir("/opt/solo/weaver/tmp/k9s/unpack")
	require.NoError(t, err)
	require.Greater(t, len(extractedFiles), 0, "Extraction directory should contain files")

	//
	// When - Install
	//
	err = installer.Install()
	require.NoError(t, err, "Failed to install k9s")

	// Verify installation files exist in sandbox
	_, exists, err := fileManager.PathExists("/opt/solo/weaver/sandbox/bin/k9s")
	require.NoError(t, err)
	require.True(t, exists, "k9s binary should exist in sandbox bin directory")

	// Check binary permissions (should be executable)
	info, err := os.Stat("/opt/solo/weaver/sandbox/bin/k9s")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(core.DefaultDirOrExecPerm), info.Mode().Perm(), "k9s binary should have 0755 permissions")

	//
	// When - Cleanup
	//
	err = installer.Cleanup()
	require.NoError(t, err, "Failed to cleanup k9s installation")

	// Check download folder is cleaned up
	_, exists, err = fileManager.PathExists("/opt/solo/weaver/tmp/k9s")
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
	require.Equal(t, "/opt/solo/weaver/sandbox/bin/k9s", linkTarget, "symlink should point to sandbox binary")

}
