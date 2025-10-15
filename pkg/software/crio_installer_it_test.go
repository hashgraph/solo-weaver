//go:build integration

package software

import (
	"os"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

func TestCrioInstaller_Download_Success(t *testing.T) {
	setupTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewCrioInstaller()
	require.NoError(t, err, "Failed to create crio installer")

	//
	// When
	//
	err = installer.Download()

	//
	// Then
	//
	require.NoError(t, err, "Failed to download crio")
}

// Test when permission to create file is denied
func TestCrioInstaller_Download_PermissionError(t *testing.T) {
	setupTestEnvironment(t)

	// Create a regular file where the directory should be created
	// This will cause MkdirAll to fail with permission/file exists error
	conflictingFile := tmpFolder
	err := os.WriteFile(conflictingFile, []byte("blocking file"), 0644)
	require.NoError(t, err, "Failed to create blocking file")

	// Override cleanup to remove the file we created
	t.Cleanup(func() {
		_ = os.Remove(conflictingFile)
	})

	//
	// Given
	//
	installer, err := NewCrioInstaller()
	require.NoError(t, err, "Failed to create crio installer")

	//
	// When
	//
	err = installer.Download()

	//
	// Then
	//
	require.Error(t, err, "Download should fail due to permission error")
	require.True(t, errorx.IsOfType(err, DownloadError), "Error should be of type DownloadError")
}

// Test when download fails due to invalid configuration
func TestCrioInstaller_Download_Fails(t *testing.T) {
	setupTestEnvironment(t)

	//
	// Given
	// Create an installer with invalid configuration by manipulating the struct directly
	config, err := LoadSoftwareConfig()
	require.NoError(t, err, "Failed to load software config")

	item, err := config.GetSoftwareByName("cri-o")
	require.NoError(t, err, "Failed to find cri-o in config")

	fsxManager, err := fsx.NewManager()
	require.NoError(t, err, "Failed to create fsx manager")

	wrongInstaller := &crioInstaller{
		BaseInstaller: &BaseInstaller{
			downloader:           NewDownloader(),
			software:             item,
			fileManager:          fsxManager,
			versionToBeInstalled: "invalidversion",
		},
	}

	//
	// When
	//
	err = wrongInstaller.Download()

	//
	// Then
	//
	require.Error(t, err, "Download should fail")
	require.True(t, errorx.IsOfType(err, VersionNotFoundError), "Error should be of type VersionNotFoundError")
}

// Test when checksum fails (this test might need adjustment based on actual config)
func TestCrioInstaller_Download_ChecksumFails(t *testing.T) {
	setupTestEnvironment(t)

	//
	// Given
	// Create an installer with a versionToBeInstalled that has wrong checksum
	config, err := LoadSoftwareConfig()
	require.NoError(t, err, "Failed to load software config")

	item, err := config.GetSoftwareByName("cri-o")
	require.NoError(t, err, "Failed to find cri-o in config")
	item.Versions["1.33.4"] = VersionDetails{
		Binary: BinaryDetails{
			"linux": {
				"arm64": {
					Algorithm: "sha256",
					Value:     "invalidchecksum",
				},
			},
		},
	}

	fsxManager, err := fsx.NewManager()
	require.NoError(t, err, "Failed to create fsx manager")

	wrongInstaller := &crioInstaller{
		BaseInstaller: &BaseInstaller{
			downloader:           NewDownloader(),
			software:             item,
			fileManager:          fsxManager,
			versionToBeInstalled: "1.33.4", // This versionToBeInstalled might not exist or have wrong checksum
		},
	}

	//
	// When
	//
	err = wrongInstaller.Download()

	//
	// Then
	//
	require.Error(t, err, "Download should fail")
	require.True(t, errorx.IsOfType(err, ChecksumError), "Error should be of type ChecksumError")

}

// Test idempotency with existing valid file
func TestCrioInstaller_Download_Idempotency_ExistingFile(t *testing.T) {
	setupTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewCrioInstaller()
	require.NoError(t, err, "Failed to create crio installer")

	err = installer.Download()
	require.NoError(t, err, "Failed to download crio")

	//
	// When
	//

	// Trigger Download again
	err = installer.Download()

	//
	// Then
	//
	require.NoError(t, err, "Download should succeed again due to idempotency")
}

func TestCrioInstaller_Download_Idempotency_ExistingFile_WrongChecksum(t *testing.T) {
	setupTestEnvironment(t)

	//
	// Given
	//
	installerInterface, err := NewCrioInstaller()
	require.NoError(t, err, "Failed to create crio installer")

	installer := installerInterface.(*crioInstaller) // cast to crioInstaller to access private fields

	// create empty file to emulate first download with wrong checksum
	err = os.MkdirAll(installer.DownloadFolder(), core.DefaultFilePerm)
	require.NoError(t, err, "Failed to create download folder")

	destinationFile, err := installer.DestinationFile()
	require.NoError(t, err, "Failed to get destination file path")

	err = os.WriteFile(destinationFile, []byte(""), core.DefaultFilePerm)
	require.NoError(t, err, "Failed to create empty file")

	//
	// When
	//
	err = installer.Download()

	//
	// Then
	//
	require.NoError(t, err, "Download should succeed again due to idempotency")
	// Check that the file exists
	_, err = os.Stat(destinationFile)
	require.NoError(t, err, "File should exist after download")
}

func TestCrioInstaller_Extract_Success(t *testing.T) {
	setupTestEnvironment(t)

	//
	// Given
	//
	installer, err := NewCrioInstaller()
	require.NoError(t, err, "Failed to create crio installer")

	//
	// When
	//
	err = installer.Download()
	require.NoError(t, err, "Failed to download crio")

	err = installer.Extract()

	//
	// Then
	//
	require.NoError(t, err, "Failed to extract crio")
	// validate there are multiple files under extracted folder
	extractedFolder := tmpFolder + "/cri-o/unpack"
	files, err := os.ReadDir(extractedFolder)
	require.NoError(t, err, "Failed to read extracted folder")
	require.Greater(t, len(files), 0, "No files found in extracted folder")
}

func TestCrioInstaller_Extract_Error(t *testing.T) {
	setupTestEnvironment(t)

	//
	// Given
	//

	// Create a regular file where the directory should be created
	conflictingFile := tmpFolder + "/cri-o/unpack"
	err := os.MkdirAll(tmpFolder+"/cri-o", core.DefaultFilePerm)
	require.NoError(t, err, "Failed to create cri-o directory")
	err = os.WriteFile(conflictingFile, []byte("blocking file"), 0644)
	require.NoError(t, err, "Failed to create blocking file")

	// Override cleanup to remove the file we created
	t.Cleanup(func() {
		_ = os.Remove(conflictingFile)
	})

	//
	// When
	//
	installer, err := NewCrioInstaller()
	require.NoError(t, err, "Failed to create crio installer")

	err = installer.Download()
	require.NoError(t, err, "Failed to download crio")

	err = installer.Extract()

	//
	// Then
	//
	require.Error(t, err, "Extract should fail due to permission error")
	require.True(t, errorx.IsOfType(err, ExtractionError), "Error should be of type ExtractError")
}
