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

// Downloader is responsible for downloading a software package and check its integrity.
type Downloader struct {
	client   *http.Client
	timeout  time.Duration
	basePath string // Base directory for validating download/extraction paths
}

// NewDownloader creates a new Downloader with default settings
func NewDownloader() *Downloader {
	transport := &http.Transport{
		// Use ProxyFromEnvironment to respect HTTP_PROXY, HTTPS_PROXY, and NO_PROXY
		Proxy: http.ProxyFromEnvironment,
	}

	return &Downloader{
		client: &http.Client{
			Timeout:   30 * time.Minute, // Default timeout for large downloads
			Transport: transport,
		},
		timeout:  30 * time.Minute,
		basePath: core.Paths().TempDir,
	}
}

// NewDownloaderWithTimeout creates a new Downloader with custom timeout
func NewDownloaderWithTimeout(timeout time.Duration) *Downloader {
	transport := &http.Transport{
		// Use ProxyFromEnvironment to respect HTTP_PROXY, HTTPS_PROXY, and NO_PROXY
		Proxy: http.ProxyFromEnvironment,
	}

	return &Downloader{
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		timeout:  timeout,
		basePath: core.Paths().TempDir,
	}
}

// Download downloads a file from the given URL to the specified destination
func (fd *Downloader) Download(url, destination string) error {
	// Validate URL before attempting download
	if err := sanity.ValidateURL(url); err != nil {
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
