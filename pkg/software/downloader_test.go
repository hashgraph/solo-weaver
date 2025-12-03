// SPDX-License-Identifier: Apache-2.0

package software

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/core"
)

func Test_Downloader_Download(t *testing.T) {
	// Create a test server
	testContent := "This is test content for download"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testContent))
	}))
	defer server.Close()

	// Create temporary file for destination
	tmpFile, err := os.CreateTemp("", "test_download_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	// Test download
	downloader := NewDownloader()
	err = downloader.Download(server.URL, tmpFile.Name())
	require.NoError(t, err, "Download failed")

	// Verify content
	content, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err, "Failed to read downloaded file")

	require.Equal(t, testContent, string(content), "Downloaded content mismatch")
}

func Test_Downloader_Download_HTTPError(t *testing.T) {
	// Create a test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "test_download_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	downloader := NewDownloader()
	err = downloader.Download(server.URL, tmpFile.Name())
	require.Error(t, err, "Download should fail with HTTP 404")

	require.True(t, errorx.IsOfType(err, DownloadError), "Error should be of type DownloadError")
}

func Test_Downloader_Timeout(t *testing.T) {
	// Create a test server that sleeps longer than the timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("This should timeout"))
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "test_timeout_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	downloader := NewDownloaderWithTimeout(1 * time.Second)
	err = downloader.Download(server.URL, tmpFile.Name())
	require.Error(t, err, "Download should fail with timeout")

	require.True(t, errorx.IsOfType(err, DownloadError), "Error should be of type DownloadError")
}

func Test_Downloader_Extract(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "test_extract_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test tar.gz file
	tarGzPath := filepath.Join(tempDir, "test.tar.gz")
	testFiles := map[string]string{
		"file1.txt": "Content of file 1",
		"file2.txt": "Content of file 2",
	}

	_, _, err = createTestTarGz(tarGzPath, testFiles)
	require.NoError(t, err, "Failed to create test tar.gz")

	// Create extraction destination
	extractDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create extraction directory")

	// Test extraction
	downloader := NewDownloader()
	err = downloader.Extract(tarGzPath, extractDir)
	require.NoError(t, err, "Extract failed")

	// Verify extracted files
	for filePath, expectedContent := range testFiles {
		extractedPath := filepath.Join(extractDir, filePath)
		content, err := os.ReadFile(extractedPath)
		require.NoError(t, err, "Failed to read extracted file: %s", filePath)
		require.Equal(t, expectedContent, string(content), "Content mismatch for file: %s", filePath)
	}
}

func Test_Downloader_Extract_FileNotFound(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_extract_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	downloader := NewDownloader()
	err = downloader.Extract("nonexistent.tar.gz", tempDir)
	require.Error(t, err, "Extract should fail with file not found")
	require.True(t, errorx.IsOfType(err, FileNotFoundError), "Error should be of type FileNotFoundError")
}

func Test_Downloader_Extract_Timeout(t *testing.T) {
	// Create a large tar.gz file that will take time to extract
	tempDir, err := os.MkdirTemp("", "test_extract_timeout_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	tarGzPath := filepath.Join(tempDir, "large.tar.gz")
	// Create many files to simulate a long extraction
	testFiles := make(map[string]string)
	for i := 0; i < 1000; i++ {
		testFiles[fmt.Sprintf("file%d.txt", i)] = fmt.Sprintf("Content %d", i)
	}

	_, _, err = createTestTarGz(tarGzPath, testFiles)
	require.NoError(t, err, "Failed to create test tar.gz")

	extractDir := filepath.Join(tempDir, "extracted")
	downloader := NewDownloaderWithTimeout(1 * time.Millisecond) // Very short timeout

	err = downloader.Extract(tarGzPath, extractDir)
	require.Error(t, err, "Extract should fail with timeout")
	require.True(t, errorx.IsOfType(err, ExtractionError), "Error should be of type ExtractionError")
}
