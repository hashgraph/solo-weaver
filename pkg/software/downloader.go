// SPDX-License-Identifier: Apache-2.0

package software

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
)

// Downloader is responsible for downloading a software package and checking its integrity.
type Downloader struct {
	client         *http.Client
	timeout        time.Duration
	basePath       string   // Base directory for validating download/extraction paths
	allowedDomains []string // List of allowed domains for SSRF protection
}

// DownloaderOption is a function that configures a Downloader
type DownloaderOption func(*Downloader)

// WithTimeout sets a custom timeout for the downloader
func WithTimeout(timeout time.Duration) DownloaderOption {
	return func(d *Downloader) {
		d.timeout = timeout
		if d.client != nil {
			d.client.Timeout = timeout
		}
	}
}

// WithBasePath sets a custom base path for the downloader
func WithBasePath(basePath string) DownloaderOption {
	return func(d *Downloader) {
		d.basePath = basePath
	}
}

// WithAllowedDomains sets custom allowed domains for the downloader
func WithAllowedDomains(domains []string) DownloaderOption {
	return func(d *Downloader) {
		d.allowedDomains = domains
	}
}

// WithHTTPClient sets a custom HTTP client for the downloader (useful for testing)
func WithHTTPClient(client *http.Client) DownloaderOption {
	return func(d *Downloader) {
		d.client = client
	}
}

// validateRedirect validates redirect URLs to prevent redirect-based SSRF attacks
// where an attacker redirects from a trusted domain to an internal service.
func (fd *Downloader) validateRedirect(req *http.Request, via []*http.Request) error {
	// Limit the number of redirects to prevent redirect loops
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}

	// Validate the redirect URL against the allowlist
	if err := sanity.ValidateURL(req.URL.String(), fd.allowedDomains); err != nil {
		return fmt.Errorf("redirect to untrusted domain: %w", err)
	}

	return nil
}

// NewDownloader creates a new Downloader with default settings and optional configurations
func NewDownloader(opts ...DownloaderOption) *Downloader {
	// Set defaults
	downloader := &Downloader{
		timeout:        30 * time.Minute,
		basePath:       core.Paths().TempDir,
		allowedDomains: sanity.AllowedDomains(),
	}

	// Apply options
	for _, opt := range opts {
		opt(downloader)
	}

	// Create HTTP client if not provided via options
	if downloader.client == nil {
		transport := &http.Transport{
			// Use ProxyFromEnvironment to respect HTTP_PROXY, HTTPS_PROXY, and NO_PROXY
			Proxy: http.ProxyFromEnvironment,
		}

		downloader.client = &http.Client{
			Timeout:   downloader.timeout,
			Transport: transport,
		}
	}

	// Always set the redirect validation, even if a custom client was provided
	// This ensures SSRF protection is enforced regardless of how the client was configured
	downloader.client.CheckRedirect = downloader.validateRedirect

	return downloader
}

// Download downloads a file from the given URL to the specified destination
func (fd *Downloader) Download(url, destination string) error {
	// Validate URL before attempting download
	if err := sanity.ValidateURL(url, fd.allowedDomains); err != nil {
		return NewInvalidURLError(err, url)
	}

	// Validate and sanitize destination path
	cleanDest, err := sanity.ValidatePathWithinBase(fd.basePath, destination)
	if err != nil {
		return NewDownloadError(err, url, 0)
	}

	resp, err := fd.client.Get(url)
	if err != nil {
		return NewDownloadError(err, url, 0)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return NewDownloadError(nil, url, resp.StatusCode)
	}

	out, err := os.Create(cleanDest)
	if err != nil {
		return NewDownloadError(err, url, 0)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return NewDownloadError(err, url, 0)
	}

	return nil
}

// ExtractTarGz extracts a tar.gz file
func (fd *Downloader) Extract(compressedFilePath string, destDir string) error {
	// Validate and sanitize the compressed file path
	cleanCompressedPath, err := sanity.ValidatePathWithinBase(fd.basePath, compressedFilePath)
	if err != nil {
		return NewExtractionError(err, compressedFilePath, destDir)
	}

	// Validate and sanitize the destination directory
	cleanDestDir, err := sanity.ValidatePathWithinBase(fd.basePath, destDir)
	if err != nil {
		return NewExtractionError(err, compressedFilePath, destDir)
	}

	ctx, cancel := context.WithTimeout(context.Background(), fd.timeout)
	defer cancel()

	file, err := os.Open(cleanCompressedPath)
	if err != nil {
		return NewFileNotFoundError(cleanCompressedPath)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
	}
	defer gz.Close()

	tarReader := tar.NewReader(gz)
	for {
		select {
		case <-ctx.Done():
			return NewExtractionError(ctx.Err(), cleanCompressedPath, cleanDestDir)
		default:
			hdr, err := tarReader.Next()
			if err == io.EOF {
				return nil // End of archive
			}
			if err != nil {
				return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
			}

			// Validate and canonicalize the extraction path to prevent path traversal attacks
			targetPath := filepath.Join(cleanDestDir, hdr.Name)

			switch hdr.Typeflag {
			case tar.TypeDir:
				// Create directories
				if err := os.MkdirAll(targetPath, core.DefaultDirOrExecPerm); err != nil {
					return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
				}
			case tar.TypeReg:
				// Ensure parent directory exists
				if err := os.MkdirAll(filepath.Dir(targetPath), core.DefaultDirOrExecPerm); err != nil {
					return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
				}

				// Extract files
				out, err := os.Create(targetPath)
				if err != nil {
					return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
				}
				if _, err := io.Copy(out, tarReader); err != nil {
					out.Close()
					return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
				}
				out.Close()

				// Set file permissions from tar header
				if err := os.Chmod(targetPath, hdr.FileInfo().Mode()); err != nil {
					return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
				}
			default:
				return NewExtractionError(fmt.Errorf("unknown type flag: %c", hdr.Typeflag), cleanCompressedPath, cleanDestDir)
			}
		}
	}
}
