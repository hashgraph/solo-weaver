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

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

func Test_Downloader_Download(t *testing.T) {
	// Create a TLS test server (uses HTTPS)
	testContent := "This is test content for download"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testContent))
	}))
	defer server.Close()

	// Create temporary file for destination
	tmpFile, err := os.CreateTemp("", "test_download_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	// Test download with a custom downloader that accepts the test server's certificate
	testClient := server.Client()
	downloader := NewDownloader(
		WithHTTPClient(testClient),
		WithBasePath(filepath.Dir(tmpFile.Name())),
		WithAllowedDomains([]string{"localhost", "127.0.0.1"}),
	)

	err = downloader.Download(server.URL, tmpFile.Name())
	require.NoError(t, err, "Download failed")

	// Verify content
	content, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err, "Failed to read downloaded file")

	require.Equal(t, testContent, string(content), "Downloaded content mismatch")
}

func Test_Downloader_Download_HTTPError(t *testing.T) {
	// Create a test server that returns 404
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "test_download_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testClient := server.Client()
	testClient.Timeout = 30 * time.Minute
	downloader := NewDownloader(
		WithHTTPClient(testClient),
		WithBasePath(filepath.Dir(tmpFile.Name())),
		WithAllowedDomains([]string{"localhost", "127.0.0.1"}),
	)

	err = downloader.Download(server.URL, tmpFile.Name())
	require.Error(t, err, "Download should fail with HTTP 404")

	require.True(t, errorx.IsOfType(err, DownloadError), "Error should be of type DownloadError")
}

func Test_Downloader_Timeout(t *testing.T) {
	// Create a test server that sleeps longer than the timeout
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("This should timeout"))
	}))
	defer server.Close()

	tmpFile, err := os.CreateTemp("", "test_timeout_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testClient := server.Client()
	downloader := NewDownloader(
		WithHTTPClient(testClient),
		WithTimeout(1*time.Second),
		WithBasePath(filepath.Dir(tmpFile.Name())),
		WithAllowedDomains([]string{"localhost", "127.0.0.1"}),
	)

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

	_, _, err = testutil.CreateTestTarGz(tarGzPath, testFiles)
	require.NoError(t, err, "Failed to create test tar.gz")

	// Create extraction destination
	extractDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create extraction directory")

	// Test extraction
	downloader := NewDownloader(
		WithBasePath(filepath.Dir(tempDir)),
	)
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

	downloader := NewDownloader(
		WithBasePath(filepath.Dir(tempDir)),
	)
	err = downloader.Extract(filepath.Join(tempDir, "nonexistent.tar.gz"), tempDir)
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

	_, _, err = testutil.CreateTestTarGz(tarGzPath, testFiles)
	require.NoError(t, err, "Failed to create test tar.gz")

	extractDir := filepath.Join(tempDir, "extracted")
	downloader := NewDownloader(
		WithTimeout(1*time.Millisecond),
		WithBasePath(filepath.Dir(tempDir)),
	)

	err = downloader.Extract(tarGzPath, extractDir)
	require.Error(t, err, "Extract should fail with timeout")
	require.True(t, errorx.IsOfType(err, ExtractionError), "Error should be of type ExtractionError")
}

func Test_Downloader_Redirect_ToAllowedDomain(t *testing.T) {
	testContent := "Redirect successful"

	// Create the redirect target server
	targetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testContent))
	}))
	defer targetServer.Close()

	// Create the initial server that redirects to the target
	redirectServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL, http.StatusFound)
	}))
	defer redirectServer.Close()

	// Create temporary file for destination
	tmpFile, err := os.CreateTemp("", "test_redirect_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testClient := redirectServer.Client()
	testClient.Timeout = 30 * time.Minute
	downloader := NewDownloader(
		WithHTTPClient(testClient),
		WithBasePath(filepath.Dir(tmpFile.Name())),
		WithAllowedDomains([]string{"localhost", "127.0.0.1"}),
	)

	err = downloader.Download(redirectServer.URL, tmpFile.Name())
	require.NoError(t, err, "Download with redirect to allowed domain should succeed")

	// Verify content
	content, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err, "Failed to read downloaded file")
	require.Equal(t, testContent, string(content), "Downloaded content mismatch")
}

func Test_Downloader_Redirect_ToUntrustedDomain(t *testing.T) {
	// Create a target server that would be blocked
	targetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Should not reach here"))
	}))
	defer targetServer.Close()

	// Create the initial server that tries to redirect to untrusted domain
	// We'll redirect to "https://untrusted.example.com" which is not in the allowlist
	redirectServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://untrusted.example.com/malicious", http.StatusFound)
	}))
	defer redirectServer.Close()

	tmpFile, err := os.CreateTemp("", "test_redirect_blocked_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testClient := redirectServer.Client()
	testClient.Timeout = 30 * time.Minute
	downloader := NewDownloader(
		WithHTTPClient(testClient),
		WithBasePath(filepath.Dir(tmpFile.Name())),
		WithAllowedDomains([]string{"localhost", "127.0.0.1"}),
	)

	err = downloader.Download(redirectServer.URL, tmpFile.Name())
	require.Error(t, err, "Download with redirect to untrusted domain should fail")
	require.Contains(t, err.Error(), "redirect to untrusted domain", "Error should mention redirect to untrusted domain")
}

func Test_Downloader_Redirect_TooManyRedirects(t *testing.T) {
	redirectCount := 0
	maxRedirects := 10

	// Create a server that keeps redirecting to itself
	var redirectServer *httptest.Server
	redirectServer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		if redirectCount > maxRedirects+5 { // Safety limit
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Too many redirects"))
			return
		}
		// Keep redirecting to self with a different path to avoid browser caching
		http.Redirect(w, r, fmt.Sprintf("%s/redirect%d", redirectServer.URL, redirectCount), http.StatusFound)
	}))
	defer redirectServer.Close()

	tmpFile, err := os.CreateTemp("", "test_redirect_limit_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testClient := redirectServer.Client()
	testClient.Timeout = 30 * time.Minute
	downloader := NewDownloader(
		WithHTTPClient(testClient),
		WithBasePath(filepath.Dir(tmpFile.Name())),
		WithAllowedDomains([]string{"localhost", "127.0.0.1"}),
	)

	err = downloader.Download(redirectServer.URL, tmpFile.Name())
	require.Error(t, err, "Download with too many redirects should fail")
	require.Contains(t, err.Error(), "stopped after 10 redirects", "Error should mention redirect limit")
}

func Test_Downloader_Redirect_ErrorMessage(t *testing.T) {
	// Create server that redirects to an untrusted domain
	redirectServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://malicious.example.com/exploit", http.StatusFound)
	}))
	defer redirectServer.Close()

	tmpFile, err := os.CreateTemp("", "test_redirect_error_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testClient := redirectServer.Client()
	testClient.Timeout = 30 * time.Minute
	downloader := NewDownloader(
		WithHTTPClient(testClient),
		WithBasePath(filepath.Dir(tmpFile.Name())),
		WithAllowedDomains([]string{"localhost", "127.0.0.1"}),
	)

	err = downloader.Download(redirectServer.URL, tmpFile.Name())
	require.Error(t, err, "Download should fail")

	// Verify the error message is clear and informative
	errMsg := err.Error()
	require.Contains(t, errMsg, "redirect to untrusted domain", "Error should mention redirect blocking")
	require.Contains(t, errMsg, "not in the allowed domain list", "Error should mention allowlist")
}

func Test_Downloader_Redirect_ChainOfRedirects(t *testing.T) {
	testContent := "Multiple redirects successful"

	// Create the final target server
	targetServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testContent))
	}))
	defer targetServer.Close()

	// Create intermediate redirect server
	redirect2 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetServer.URL, http.StatusFound)
	}))
	defer redirect2.Close()

	// Create first redirect server
	redirect1 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirect2.URL, http.StatusFound)
	}))
	defer redirect1.Close()

	tmpFile, err := os.CreateTemp("", "test_redirect_chain_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testClient := redirect1.Client()
	testClient.Timeout = 30 * time.Minute
	downloader := NewDownloader(
		WithHTTPClient(testClient),
		WithBasePath(filepath.Dir(tmpFile.Name())),
		WithAllowedDomains([]string{"localhost", "127.0.0.1"}),
	)

	err = downloader.Download(redirect1.URL, tmpFile.Name())
	require.NoError(t, err, "Download with multiple allowed redirects should succeed")

	// Verify content
	content, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err, "Failed to read downloaded file")
	require.Equal(t, testContent, string(content), "Downloaded content mismatch")
}

func Test_validateRedirect_DirectCall(t *testing.T) {
	tests := []struct {
		name           string
		allowedDomains []string
		redirectURL    string
		redirectCount  int
		expectError    bool
		errorContains  string
	}{
		{
			name:           "Valid redirect to allowed domain",
			allowedDomains: []string{"github.com"},
			redirectURL:    "https://github.com/releases",
			redirectCount:  5,
			expectError:    false,
		},
		{
			name:           "Redirect to untrusted domain",
			allowedDomains: []string{"github.com"},
			redirectURL:    "https://malicious.com/exploit",
			redirectCount:  2,
			expectError:    true,
			errorContains:  "redirect to untrusted domain",
		},
		{
			name:           "Redirect limit exceeded",
			allowedDomains: []string{"github.com"},
			redirectURL:    "https://github.com/releases",
			redirectCount:  10,
			expectError:    true,
			errorContains:  "stopped after 10 redirects",
		},
		{
			name:           "Redirect to subdomain of allowed domain",
			allowedDomains: []string{"github.com"},
			redirectURL:    "https://api.github.com/releases",
			redirectCount:  3,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloader := NewDownloader(WithAllowedDomains(tt.allowedDomains))

			// Create dummy previous requests to simulate redirect chain
			via := make([]*http.Request, tt.redirectCount)
			for i := 0; i < tt.redirectCount; i++ {
				req, err := http.NewRequest("GET", "https://example.com", nil)
				require.NoError(t, err)
				via[i] = req
			}

			// Create the redirect request
			req, err := http.NewRequest("GET", tt.redirectURL, nil)
			require.NoError(t, err)

			// Test the validateRedirect function
			err = downloader.validateRedirect(req, via)

			if tt.expectError {
				require.Error(t, err, "validateRedirect should return error")
				if tt.errorContains != "" {
					require.Contains(t, err.Error(), tt.errorContains, "Error message should contain expected text")
				}
			} else {
				require.NoError(t, err, "validateRedirect should not return error")
			}
		})
	}
}
