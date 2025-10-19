package software

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

func Test_BaseInstaller_replaceAllInFile(t *testing.T) {
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	ki := kubeadmInstaller{
		baseInstaller: &baseInstaller{
			fileManager: fsxManager,
		},
	}

	// Create a temp dir and file
	tmpDir := t.TempDir()
	origPath := filepath.Join(tmpDir, "10-kubeadm.conf")
	origContent := "ExecStart=/usr/bin/kubelet $KUBELET_KUBEADM_ARGS\n"
	if err := os.WriteFile(origPath, []byte(origContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	newKubeletPath := "/custom/bin/kubelet"
	if err := ki.replaceAllInFile(origPath, "/usr/bin/kubelet", newKubeletPath); err != nil {
		t.Fatalf("replaceKubeletPath failed: %v", err)
	}

	// Read back and check
	updated, err := os.ReadFile(origPath)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}
	if !strings.Contains(string(updated), newKubeletPath) {
		t.Errorf("expected file to contain new kubelet path %q, got %q", newKubeletPath, string(updated))
	}
	if strings.Contains(string(updated), "/usr/bin/kubelet") {
		t.Errorf("old kubelet path still present in file")
	}
}

func newTestInstaller(t *testing.T) *baseInstaller {
	t.Helper()

	// TempDir auto-cleans at the end of the test
	tempDir := t.TempDir()

	// Build the tar.gz used by the server
	tarGzPath := filepath.Join(tempDir, "test-artifact.tar.gz")
	checksum, err := createTestTarGz(tarGzPath, nil)
	require.NoError(t, err, "Failed to create test tar.gz")

	fileContents, err := os.ReadFile(tarGzPath)
	require.NoError(t, err, "Failed to read test tar.gz")

	// Keep server alive for the duration of the test
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fileContents)
	}))
	t.Cleanup(server.Close)

	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)
	item := SoftwareMetadata{
		Name: "test-artifact",
		Versions: map[Version]VersionDetails{
			"1.0.0": {
				Binary: BinaryDetails{
					"test-os": {
						"test-arch": {
							Algorithm: "sha256",
							Value:     checksum,
						},
					},
				},
			},
		},
		URL:      server.URL + "/test-artifact.tar.gz",
		Filename: "test-artifact.tar.gz",
	}

	return &baseInstaller{
		downloader:           NewDownloader(),
		software:             item.withPlatform("test-os", "test-arch"),
		fileManager:          fsxManager,
		versionToBeInstalled: "1.0.0",
	}
}

// Test successful download
func Test_BaseInstaller_Download_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.NoError(t, err, "Failed to download test-artifact")
}

// Test when permission to create file is denied
func Test_BaseInstaller_Download_PermissionError(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)

	// Create a regular file where the directory should be created
	// This will cause MkdirAll to fail with permission/file exists error
	conflictingFile := tmpFolder
	err := os.MkdirAll("/opt/provisioner", core.DefaultFilePerm)
	require.NoError(t, err, "Failed to create /opt/provisioner directory")
	err = os.WriteFile(conflictingFile, []byte("blocking file"), 0644)
	require.NoError(t, err, "Failed to create blocking file")

	// Override cleanup to remove the file we created
	t.Cleanup(func() {
		_ = os.Remove(conflictingFile)
	})

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
func Test_BaseInstaller_Download_Fails(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)
	installer.versionToBeInstalled = "invalidversion" // Set to a version that doesn't exist

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.Error(t, err, "Download should fail")
	require.True(t, errorx.IsOfType(err, VersionNotFoundError), "Error should be of type VersionNotFoundError")
}

// Test when checksum fails (this test might need adjustment based on actual config)
func Test_BaseInstaller_Download_ChecksumFails(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	installer := newTestInstaller(t)

	// Corrupt the checksum (read-modify-write because map index returns non-addressable values)
	vd := installer.software.Versions["1.0.0"]
	bd := vd.Binary["test-os"]
	entry := bd["test-arch"]
	entry.Value = "invalidchecksum"
	bd["test-arch"] = entry
	vd.Binary["test-os"] = bd
	installer.software.Versions["1.0.0"] = vd

	//
	// When
	//
	err := installer.Download()

	//
	// Then
	//
	require.Error(t, err, "Download should fail")
	require.True(t, errorx.IsOfType(err, ChecksumError), "Error should be of type ChecksumError")

}

// Test idempotency with existing valid file
func Test_BaseInstaller_Download_Idempotency_ExistingFile(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)

	err := installer.Download()
	require.NoError(t, err, "Failed to download test-artifact")

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

func Test_BaseInstaller_Download_Idempotency_ExistingFile_WrongChecksum(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)

	// create empty file to emulate first download with wrong checksum
	err := os.MkdirAll(installer.downloadFolder(), core.DefaultFilePerm)
	require.NoError(t, err, "Failed to create download folder")

	destinationFile, err := installer.destinationFilePath()
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

func Test_BaseInstaller_Extract_Success(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//
	installer := newTestInstaller(t)

	//
	// When
	//
	err := installer.Download()
	require.NoError(t, err, "Failed to download test-artifact")

	err = installer.Extract()

	//
	// Then
	//
	require.NoError(t, err, "Failed to extract test-artifact")
	// validate there are multiple files under extracted folder
	extractedFolder := tmpFolder + "/test-artifact/unpack"
	files, err := os.ReadDir(extractedFolder)
	require.NoError(t, err, "Failed to read extracted folder")
	require.Greater(t, len(files), 0, "No files found in extracted folder")
}

func Test_BaseInstaller_Extract_Error(t *testing.T) {
	resetTestEnvironment(t)

	//
	// Given
	//

	// Create a regular file where the directory should be created
	conflictingFile := tmpFolder + "/test-artifact/unpack"
	err := os.MkdirAll(tmpFolder+"/test-artifact", core.DefaultFilePerm)
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
	installer := newTestInstaller(t)

	err = installer.Download()
	require.NoError(t, err, "Failed to download test-artifact")

	err = installer.Extract()

	//
	// Then
	//
	require.Error(t, err, "Extract should fail due to permission error")
	require.True(t, errorx.IsOfType(err, ExtractionError), "Error should be of type ExtractError")
}
