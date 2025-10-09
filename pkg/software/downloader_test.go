package software

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

func TestFileDownloader_Download(t *testing.T) {
	// Create a test server
	testContent := "This is test content for download"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testContent))
	}))
	defer server.Close()

	// Create temporary file for destination
	tmpFile, err := os.CreateTemp("", "test_download_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Test download
	downloader := NewDownloader()
	err = downloader.Download(server.URL, tmpFile.Name())
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify content
	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Downloaded content mismatch: got %s, want %s", string(content), testContent)
	}
}

func TestFileDownloader_Download_HTTPError(t *testing.T) {
	// Create a test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "test_download_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	downloader := NewDownloader()
	err = downloader.Download(server.URL, tmpFile.Name())
	if err == nil {
		t.Fatal("Expected download to fail with HTTP 404, but it succeeded")
	}

	require.True(t, errorx.IsOfType(err, DownloadError), "Error should be of type DownloadError")
}

func TestNewFileDownloaderWithTimeout(t *testing.T) {
	timeout := 5 * time.Second
	downloader := NewDownloaderWithTimeout(timeout)

	if downloader.timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, downloader.timeout)
	}

	if downloader.client.Timeout != timeout {
		t.Errorf("Expected client timeout %v, got %v", timeout, downloader.client.Timeout)
	}
}

func TestFileDownloader_Timeout(t *testing.T) {
	// Create a test server that sleeps longer than the timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("This should timeout"))
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "test_timeout_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	downloader := NewDownloaderWithTimeout(1 * time.Second)
	err = downloader.Download(server.URL, tmpFile.Name())
	if err == nil {
		t.Fatal("Expected download to fail due to timeout, but it succeeded")
	}

	if !strings.Contains(err.Error(), "Client.Timeout") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
	require.True(t, errorx.IsOfType(err, DownloadError), "Error should be of type DownloadError")
}

func TestFileDownloader_VerifyNonExistentFile(t *testing.T) {
	downloader := NewDownloader()

	err := downloader.VerifyChecksum("/non/existent/file", "somehash", "md5")
	if err == nil {
		t.Fatal("Expected error when verifying non-existent file")
	}
	require.True(t, errorx.IsOfType(err, FileNotFoundError), "Error should be of type FileNotFoundError")

	err = downloader.VerifyChecksum("/non/existent/file", "somehash", "sha256")
	if err == nil {
		t.Fatal("Expected error when verifying non-existent file")
	}
	require.True(t, errorx.IsOfType(err, FileNotFoundError), "Error should be of type FileNotFoundError")

	err = downloader.VerifyChecksum("/non/existent/file", "somehash", "sha512")
	if err == nil {
		t.Fatal("Expected error when verifying non-existent file")
	}
	require.True(t, errorx.IsOfType(err, FileNotFoundError), "Error should be of type FileNotFoundError")
}

func TestFileDownloader_VerifyChecksum(t *testing.T) {
	// Create temporary file with known content
	testContent := "Hello, World!"
	tmpFile, err := os.CreateTemp("", "test_checksum_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(testContent)
	require.NoError(t, err, "Failed to write test content to temp file")
	tmpFile.Close()

	downloader := NewDownloader()

	// Test MD5 algorithm
	hash := md5.New()
	hash.Write([]byte(testContent))
	expectedMD5 := fmt.Sprintf("%x", hash.Sum(nil))

	err = downloader.VerifyChecksum(tmpFile.Name(), expectedMD5, "md5")
	require.NoError(t, err, "MD5 verification through VerifyChecksum failed")

	// Test SHA256 algorithm
	hash256 := sha256.New()
	hash256.Write([]byte(testContent))
	expectedSHA256 := fmt.Sprintf("%x", hash256.Sum(nil))

	err = downloader.VerifyChecksum(tmpFile.Name(), expectedSHA256, "sha256")
	require.NoError(t, err, "SHA256 verification through VerifyChecksum failed")

	// Test SHA512 algorithm
	hash512 := sha512.New()
	hash512.Write([]byte(testContent))
	expectedSHA512 := fmt.Sprintf("%x", hash512.Sum(nil))

	err = downloader.VerifyChecksum(tmpFile.Name(), expectedSHA512, "sha512")
	require.NoError(t, err, "SHA512 verification through VerifyChecksum failed")

	// Test with wrong checksum
	err = downloader.VerifyChecksum(tmpFile.Name(), "wronghash", "sha256")
	require.Error(t, err, "VerifyChecksum should fail with wrong checksum")
	require.True(t, errorx.IsOfType(err, ChecksumError), "Error should be of type ChecksumError")

	// Test with unsupported algorithm
	err = downloader.VerifyChecksum(tmpFile.Name(), expectedSHA256, "sha1")
	require.Error(t, err, "VerifyChecksum should fail with unsupported algorithm")
	require.True(t, errorx.IsOfType(err, ChecksumError), "Error should be of type ChecksumError")

	// Test with non-existent file
	err = downloader.VerifyChecksum("/non/existent/file", expectedSHA256, "sha256")
	require.Error(t, err, "VerifyChecksum should fail with non-existent file")

	require.True(t, errorx.IsOfType(err, FileNotFoundError), "Error should be of type FileNotFoundError")
}
