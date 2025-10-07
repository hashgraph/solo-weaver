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
	downloader := NewFileDownloader()
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

	downloader := NewFileDownloader()
	err = downloader.Download(server.URL, tmpFile.Name())
	if err == nil {
		t.Fatal("Expected download to fail with HTTP 404, but it succeeded")
	}

	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("Expected error to contain 'HTTP 404', got: %v", err)
	}
}

func TestFileDownloader_VerifyMD5(t *testing.T) {
	// Create temporary file with known content
	testContent := "Hello, World!"
	tmpFile, err := os.CreateTemp("", "test_md5_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(testContent)
	if err != nil {
		t.Fatalf("Failed to write test content: %v", err)
	}
	tmpFile.Close()

	// Calculate expected MD5
	hash := md5.New()
	hash.Write([]byte(testContent))
	expectedMD5 := fmt.Sprintf("%x", hash.Sum(nil))

	// Test verification
	downloader := NewFileDownloader()
	err = downloader.VerifyMD5(tmpFile.Name(), expectedMD5)
	if err != nil {
		t.Fatalf("MD5 verification failed: %v", err)
	}

	// Test with wrong MD5
	err = downloader.VerifyMD5(tmpFile.Name(), "wrongmd5hash")
	if err == nil {
		t.Fatal("Expected MD5 verification to fail with wrong hash, but it succeeded")
	}
}

func TestFileDownloader_VerifySHA256(t *testing.T) {
	// Create temporary file with known content
	testContent := "Hello, World!"
	tmpFile, err := os.CreateTemp("", "test_sha256_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(testContent)
	if err != nil {
		t.Fatalf("Failed to write test content: %v", err)
	}
	tmpFile.Close()

	// Calculate expected SHA256
	hash := sha256.New()
	hash.Write([]byte(testContent))
	expectedSHA256 := fmt.Sprintf("%x", hash.Sum(nil))

	// Test verification
	downloader := NewFileDownloader()
	err = downloader.VerifySHA256(tmpFile.Name(), expectedSHA256)
	if err != nil {
		t.Fatalf("SHA256 verification failed: %v", err)
	}

	// Test with wrong SHA256
	err = downloader.VerifySHA256(tmpFile.Name(), "wrongsha256hash")
	if err == nil {
		t.Fatal("Expected SHA256 verification to fail with wrong hash, but it succeeded")
	}
}

func TestFileDownloader_VerifySHA512(t *testing.T) {
	// Create temporary file with known content
	testContent := "Hello, World!"
	tmpFile, err := os.CreateTemp("", "test_sha512_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(testContent)
	if err != nil {
		t.Fatalf("Failed to write test content: %v", err)
	}
	tmpFile.Close()

	// Calculate expected SHA512
	hash := sha512.New()
	hash.Write([]byte(testContent))
	expectedSHA512 := fmt.Sprintf("%x", hash.Sum(nil))

	// Test verification
	downloader := NewFileDownloader()
	err = downloader.VerifySHA512(tmpFile.Name(), expectedSHA512)
	if err != nil {
		t.Fatalf("SHA512 verification failed: %v", err)
	}

	// Test with wrong SHA512
	err = downloader.VerifySHA512(tmpFile.Name(), "wrongsha512hash")
	if err == nil {
		t.Fatal("Expected SHA512 verification to fail with wrong hash, but it succeeded")
	}
}

func TestNewFileDownloaderWithTimeout(t *testing.T) {
	timeout := 5 * time.Second
	downloader := NewFileDownloaderWithTimeout(timeout)

	if downloader.timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, downloader.timeout)
	}

	if downloader.client.Timeout != timeout {
		t.Errorf("Expected client timeout %v, got %v", timeout, downloader.client.Timeout)
	}
}

func TestFileDownloader_VerifyNonExistentFile(t *testing.T) {
	downloader := NewFileDownloader()

	err := downloader.VerifyMD5("/non/existent/file", "somehash")
	if err == nil {
		t.Fatal("Expected error when verifying non-existent file")
	}

	err = downloader.VerifySHA256("/non/existent/file", "somehash")
	if err == nil {
		t.Fatal("Expected error when verifying non-existent file")
	}

	err = downloader.VerifySHA512("/non/existent/file", "somehash")
	if err == nil {
		t.Fatal("Expected error when verifying non-existent file")
	}
}
