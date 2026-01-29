// SPDX-License-Identifier: Apache-2.0

package software

import (
	"archive/tar"
	"compress/gzip"
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

// Test_Downloader_Extract_Symlink tests extraction of tar archives containing symlinks
func Test_Downloader_Extract_Symlink(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "test_extract_symlink_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test tar.gz file with a symlink
	tarGzPath := filepath.Join(tempDir, "test_symlink.tar.gz")
	err = createTarGzWithSymlink(tarGzPath, "target.txt", "This is the target file", "link.txt", "target.txt")
	require.NoError(t, err, "Failed to create test tar.gz with symlink")

	// Create extraction destination
	extractDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create extraction directory")

	// Test extraction
	downloader := NewDownloader(
		WithBasePath(filepath.Dir(tempDir)),
	)
	err = downloader.Extract(tarGzPath, extractDir)
	require.NoError(t, err, "Extract with symlink failed")

	// Verify the target file exists
	targetPath := filepath.Join(extractDir, "target.txt")
	content, err := os.ReadFile(targetPath)
	require.NoError(t, err, "Failed to read target file")
	require.Equal(t, "This is the target file", string(content), "Target file content mismatch")

	// Verify the symlink exists and points to the correct target
	linkPath := filepath.Join(extractDir, "link.txt")
	linkTarget, err := os.Readlink(linkPath)
	require.NoError(t, err, "Failed to read symlink")
	require.Equal(t, "target.txt", linkTarget, "Symlink target mismatch")

	// Verify we can read through the symlink
	linkContent, err := os.ReadFile(linkPath)
	require.NoError(t, err, "Failed to read through symlink")
	require.Equal(t, "This is the target file", string(linkContent), "Content read through symlink mismatch")
}

// Test_Downloader_Extract_Hardlink tests extraction of tar archives containing hardlinks
func Test_Downloader_Extract_Hardlink(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "test_extract_hardlink_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test tar.gz file with a hardlink
	tarGzPath := filepath.Join(tempDir, "test_hardlink.tar.gz")
	err = createTarGzWithHardlink(tarGzPath, "original.txt", "This is the original file", "hardlink.txt", "original.txt")
	require.NoError(t, err, "Failed to create test tar.gz with hardlink")

	// Create extraction destination
	extractDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create extraction directory")

	// Test extraction
	downloader := NewDownloader(
		WithBasePath(filepath.Dir(tempDir)),
	)
	err = downloader.Extract(tarGzPath, extractDir)
	require.NoError(t, err, "Extract with hardlink failed")

	// Verify the original file exists
	originalPath := filepath.Join(extractDir, "original.txt")
	content, err := os.ReadFile(originalPath)
	require.NoError(t, err, "Failed to read original file")
	require.Equal(t, "This is the original file", string(content), "Original file content mismatch")

	// Verify the hardlink exists and has the same content
	hardlinkPath := filepath.Join(extractDir, "hardlink.txt")
	hardlinkContent, err := os.ReadFile(hardlinkPath)
	require.NoError(t, err, "Failed to read hardlink")
	require.Equal(t, "This is the original file", string(hardlinkContent), "Hardlink content mismatch")

	// Verify they share the same inode (hardlink property)
	originalInfo, err := os.Stat(originalPath)
	require.NoError(t, err, "Failed to stat original file")
	hardlinkInfo, err := os.Stat(hardlinkPath)
	require.NoError(t, err, "Failed to stat hardlink")

	// On Unix systems, hardlinks should have the same inode
	// We can't easily check inode in a portable way, but we can verify
	// that modifying one affects the other
	err = os.WriteFile(originalPath, []byte("Modified content"), 0644)
	require.NoError(t, err, "Failed to modify original file")

	// Read through hardlink - should see modified content
	modifiedContent, err := os.ReadFile(hardlinkPath)
	require.NoError(t, err, "Failed to read hardlink after modification")
	require.Equal(t, "Modified content", string(modifiedContent), "Hardlink should reflect changes to original")

	// Verify sizes match (another property of hardlinks)
	require.Equal(t, originalInfo.Size(), hardlinkInfo.Size(), "Hardlink and original should have same size")
}

// Test_Downloader_Extract_SymlinkPathTraversal tests that symlinks with path traversal in target are rejected
func Test_Downloader_Extract_SymlinkPathTraversal(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "test_extract_symlink_traversal_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test tar.gz file with a symlink that tries to escape via its target (linkname)
	// The symlink target points outside the extraction directory
	tarGzPath := filepath.Join(tempDir, "test_symlink_escape.tar.gz")
	err = createTarGzWithSymlink(tarGzPath, "safe.txt", "Safe content", "malicious_link", "../../../etc/passwd")
	require.NoError(t, err, "Failed to create test tar.gz with malicious symlink")

	// Create extraction destination
	extractDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create extraction directory")

	// Test extraction - symlink with path traversal target should be rejected
	downloader := NewDownloader(
		WithBasePath(filepath.Dir(tempDir)),
	)
	err = downloader.Extract(tarGzPath, extractDir)
	require.Error(t, err, "Extract should fail for symlink with path traversal target")
	require.Contains(t, err.Error(), "symlink target escapes extraction directory", "Error should mention symlink target escape")

	// Verify the malicious symlink was NOT created
	linkPath := filepath.Join(extractDir, "malicious_link")
	_, err = os.Lstat(linkPath)
	require.True(t, os.IsNotExist(err), "Malicious symlink should not be created")
}

// Test_Downloader_Extract_MaliciousHeaderName tests that tar entries with path traversal in hdr.Name are rejected
func Test_Downloader_Extract_MaliciousHeaderName(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "test_extract_malicious_name_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test tar.gz file with a file that has path traversal in its name
	tarGzPath := filepath.Join(tempDir, "test_malicious_name.tar.gz")
	err = createTarGzWithMaliciousName(tarGzPath, "../../../tmp/pwned.txt", "You've been pwned!")
	require.NoError(t, err, "Failed to create test tar.gz with malicious name")

	// Create extraction destination
	extractDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create extraction directory")

	// Test extraction - should fail because path traversal in hdr.Name is rejected
	downloader := NewDownloader(
		WithBasePath(filepath.Dir(tempDir)),
	)
	err = downloader.Extract(tarGzPath, extractDir)
	require.Error(t, err, "Extract should fail for path traversal in hdr.Name")
	require.Contains(t, err.Error(), "path traversal attempt", "Error should mention path traversal")
}

// Test_Downloader_Extract_SymlinkInSubdirectory tests symlinks in subdirectories
func Test_Downloader_Extract_SymlinkInSubdirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_extract_symlink_subdir_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	tarGzPath := filepath.Join(tempDir, "test_symlink_subdir.tar.gz")
	err = createTarGzWithSymlinkInSubdir(tarGzPath)
	require.NoError(t, err, "Failed to create test tar.gz")

	extractDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create extraction directory")

	downloader := NewDownloader(
		WithBasePath(filepath.Dir(tempDir)),
	)
	err = downloader.Extract(tarGzPath, extractDir)
	require.NoError(t, err, "Extract failed")

	// Verify the symlink in the subdirectory
	linkPath := filepath.Join(extractDir, "subdir", "link_to_file.txt")
	linkTarget, err := os.Readlink(linkPath)
	require.NoError(t, err, "Failed to read symlink")
	require.Equal(t, "../file.txt", linkTarget, "Symlink target mismatch")

	// Verify we can read through the symlink
	content, err := os.ReadFile(linkPath)
	require.NoError(t, err, "Failed to read through symlink")
	require.Equal(t, "File content", string(content), "Content read through symlink mismatch")
}

// Test_Downloader_Extract_OverwriteExistingSymlink tests that existing symlinks are overwritten
func Test_Downloader_Extract_OverwriteExistingSymlink(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_extract_overwrite_symlink_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create extraction destination with a pre-existing symlink
	extractDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create extraction directory")

	// Create a file and an existing symlink pointing to it
	existingTarget := filepath.Join(extractDir, "existing_target.txt")
	err = os.WriteFile(existingTarget, []byte("Existing target"), 0644)
	require.NoError(t, err, "Failed to create existing target")

	existingLink := filepath.Join(extractDir, "link.txt")
	err = os.Symlink("existing_target.txt", existingLink)
	require.NoError(t, err, "Failed to create existing symlink")

	// Create tar.gz with a symlink that should overwrite the existing one
	tarGzPath := filepath.Join(tempDir, "test_overwrite.tar.gz")
	err = createTarGzWithSymlink(tarGzPath, "new_target.txt", "New target content", "link.txt", "new_target.txt")
	require.NoError(t, err, "Failed to create test tar.gz")

	downloader := NewDownloader(
		WithBasePath(filepath.Dir(tempDir)),
	)
	err = downloader.Extract(tarGzPath, extractDir)
	require.NoError(t, err, "Extract should succeed and overwrite existing symlink")

	// Verify the symlink now points to the new target
	linkTarget, err := os.Readlink(existingLink)
	require.NoError(t, err, "Failed to read symlink")
	require.Equal(t, "new_target.txt", linkTarget, "Symlink should point to new target")

	// Verify content through symlink
	content, err := os.ReadFile(existingLink)
	require.NoError(t, err, "Failed to read through symlink")
	require.Equal(t, "New target content", string(content), "Content should be from new target")
}

// Test_Downloader_Extract_AbsoluteSymlinkTarget tests that absolute symlink targets are rejected
func Test_Downloader_Extract_AbsoluteSymlinkTarget(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_extract_absolute_symlink_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	tarGzPath := filepath.Join(tempDir, "test_absolute_symlink.tar.gz")
	err = createTarGzWithSymlink(tarGzPath, "safe.txt", "Safe content", "absolute_link", "/etc/passwd")
	require.NoError(t, err, "Failed to create test tar.gz")

	extractDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create extraction directory")

	downloader := NewDownloader(
		WithBasePath(filepath.Dir(tempDir)),
	)
	err = downloader.Extract(tarGzPath, extractDir)
	require.Error(t, err, "Extract should fail for absolute symlink target")
	require.Contains(t, err.Error(), "absolute symlink target not allowed", "Error should mention absolute symlink")
}

// Test_Downloader_Extract_HardlinkPathTraversal tests that hardlinks with path traversal are rejected
func Test_Downloader_Extract_HardlinkPathTraversal(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_extract_hardlink_traversal_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	tarGzPath := filepath.Join(tempDir, "test_hardlink_escape.tar.gz")
	err = createTarGzWithHardlink(tarGzPath, "safe.txt", "Safe content", "malicious_hardlink", "../../../etc/passwd")
	require.NoError(t, err, "Failed to create test tar.gz")

	extractDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create extraction directory")

	downloader := NewDownloader(
		WithBasePath(filepath.Dir(tempDir)),
	)
	err = downloader.Extract(tarGzPath, extractDir)
	require.Error(t, err, "Extract should fail for hardlink with path traversal target")
	require.Contains(t, err.Error(), "hardlink target escapes extraction directory", "Error should mention hardlink target escape")
}

// Test_Downloader_Extract_AbsoluteHardlinkTarget tests that absolute hardlink targets are rejected
func Test_Downloader_Extract_AbsoluteHardlinkTarget(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test_extract_absolute_hardlink_*")
	require.NoError(t, err, "Failed to create temp dir")
	defer func() { _ = os.RemoveAll(tempDir) }()

	tarGzPath := filepath.Join(tempDir, "test_absolute_hardlink.tar.gz")
	err = createTarGzWithHardlink(tarGzPath, "safe.txt", "Safe content", "absolute_hardlink", "/etc/passwd")
	require.NoError(t, err, "Failed to create test tar.gz")

	extractDir := filepath.Join(tempDir, "extracted")
	err = os.MkdirAll(extractDir, core.DefaultDirOrExecPerm)
	require.NoError(t, err, "Failed to create extraction directory")

	downloader := NewDownloader(
		WithBasePath(filepath.Dir(tempDir)),
	)
	err = downloader.Extract(tarGzPath, extractDir)
	require.Error(t, err, "Extract should fail for absolute hardlink target")
	require.Contains(t, err.Error(), "absolute hardlink target not allowed", "Error should mention absolute hardlink")
}

// Helper functions to create tar.gz files with symlinks and hardlinks

func createTarGzWithSymlink(outputPath, targetName, targetContent, linkName, linkTarget string) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Write the target file first
	targetData := []byte(targetContent)
	err = tw.WriteHeader(&tar.Header{
		Name:    targetName,
		Mode:    0644,
		Size:    int64(len(targetData)),
		ModTime: time.Now(),
	})
	if err != nil {
		return err
	}
	if _, err := tw.Write(targetData); err != nil {
		return err
	}

	// Write the symlink
	err = tw.WriteHeader(&tar.Header{
		Name:     linkName,
		Mode:     0777,
		Typeflag: tar.TypeSymlink,
		Linkname: linkTarget,
		ModTime:  time.Now(),
	})
	if err != nil {
		return err
	}

	return nil
}

func createTarGzWithHardlink(outputPath, originalName, originalContent, linkName, linkTarget string) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Write the original file first
	originalData := []byte(originalContent)
	err = tw.WriteHeader(&tar.Header{
		Name:    originalName,
		Mode:    0644,
		Size:    int64(len(originalData)),
		ModTime: time.Now(),
	})
	if err != nil {
		return err
	}
	if _, err := tw.Write(originalData); err != nil {
		return err
	}

	// Write the hardlink
	err = tw.WriteHeader(&tar.Header{
		Name:     linkName,
		Mode:     0644,
		Typeflag: tar.TypeLink,
		Linkname: linkTarget,
		ModTime:  time.Now(),
	})
	if err != nil {
		return err
	}

	return nil
}

func createTarGzWithMaliciousName(outputPath, maliciousName, content string) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Write a file with path traversal in its name
	data := []byte(content)
	err = tw.WriteHeader(&tar.Header{
		Name:    maliciousName,
		Mode:    0644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	})
	if err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}

	return nil
}

func createTarGzWithSymlinkInSubdir(outputPath string) error {
	outFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Create subdirectory
	err = tw.WriteHeader(&tar.Header{
		Name:     "subdir/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
		ModTime:  time.Now(),
	})
	if err != nil {
		return err
	}

	// Write a file at root level
	fileContent := []byte("File content")
	err = tw.WriteHeader(&tar.Header{
		Name:    "file.txt",
		Mode:    0644,
		Size:    int64(len(fileContent)),
		ModTime: time.Now(),
	})
	if err != nil {
		return err
	}
	if _, err := tw.Write(fileContent); err != nil {
		return err
	}

	// Write a symlink in subdirectory pointing to parent file
	err = tw.WriteHeader(&tar.Header{
		Name:     "subdir/link_to_file.txt",
		Mode:     0777,
		Typeflag: tar.TypeSymlink,
		Linkname: "../file.txt",
		ModTime:  time.Now(),
	})
	if err != nil {
		return err
	}

	return nil
}
