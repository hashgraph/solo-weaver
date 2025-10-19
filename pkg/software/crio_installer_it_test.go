//go:build integration

package software

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_CrioInstaller_FullWorkflow_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewCrioInstaller()
	require.NoError(t, err, "Failed to create cri-o installer")

	//
	// When - Download
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download cri-o and/or its configuration")

	// Verify downloaded files exist
	files, err := os.ReadDir("/opt/provisioner/tmp/cri-o")
	require.NoError(t, err)

	require.Equal(t, 1, len(files), "There should be exactly one file in the download directory")
	// Check that the downloaded file has the expected name format by regex
	require.Regexp(t,
		regexp.MustCompile(`^cri-o\.[^-]+\.v[0-9]+\.[0-9]+\.[0-9]+\.tar\.gz$`),
		files[0].Name(),
		"Downloaded file name should match expected pattern",
	)

	//
	// When - Extract
	//
	err = installer.Extract()
	require.NoError(t, err, "Failed to extract cri-o")

	// Verify extraction directory exists and contains expected files
	extractedFiles, err := os.ReadDir("/opt/provisioner/tmp/cri-o/unpack")
	require.NoError(t, err)
	require.Greater(t, len(extractedFiles), 0, "Extraction directory should contain files")

	//
	// When - Install
	//
	// TODO - Install and Configure still needs to be implemented based on the install script provided by cri-o

}
