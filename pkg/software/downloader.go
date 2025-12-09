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
)

// Downloader is responsible for downloading a software package and check its integrity.
type Downloader struct {
	client  *http.Client
	timeout time.Duration
}

// NewDownloader creates a new Downloader with default settings
func NewDownloader() *Downloader {
	return &Downloader{
		client: &http.Client{
			Timeout: 30 * time.Minute, // Default timeout for large downloads
		},
		timeout: 30 * time.Minute,
	}
}

// NewDownloaderWithTimeout creates a new Downloader with custom timeout
func NewDownloaderWithTimeout(timeout time.Duration) *Downloader {
	return &Downloader{
		client: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// Download downloads a file from the given URL to the specified destination
func (fd *Downloader) Download(url, destination string) error {
	resp, err := fd.client.Get(url)
	if err != nil {
		return NewDownloadError(err, url, 0)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return NewDownloadError(nil, url, resp.StatusCode)
	}

	out, err := os.Create(destination)
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
	ctx, cancel := context.WithTimeout(context.Background(), fd.timeout)
	defer cancel()

	file, err := os.Open(compressedFilePath)
	if err != nil {
		return NewFileNotFoundError(compressedFilePath)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return NewExtractionError(err, compressedFilePath, destDir)
	}
	defer gz.Close()

	tarReader := tar.NewReader(gz)
	for {
		select {
		case <-ctx.Done():
			return NewExtractionError(ctx.Err(), compressedFilePath, destDir)
		default:
			hdr, err := tarReader.Next()
			if err == io.EOF {
				return nil // End of archive
			}
			if err != nil {
				return NewExtractionError(err, compressedFilePath, destDir)
			}

			target := filepath.Join(destDir, hdr.Name)
			switch hdr.Typeflag {
			case tar.TypeDir:
				// Create directories
				if err := os.MkdirAll(target, core.DefaultDirOrExecPerm); err != nil {
					return NewExtractionError(err, compressedFilePath, destDir)
				}
			case tar.TypeReg:
				// Ensure parent directory exists
				if err := os.MkdirAll(filepath.Dir(target), core.DefaultDirOrExecPerm); err != nil {
					return NewExtractionError(err, compressedFilePath, destDir)
				}

				// Extract files
				out, err := os.Create(target)
				if err != nil {
					return NewExtractionError(err, compressedFilePath, destDir)
				}
				if _, err := io.Copy(out, tarReader); err != nil {
					out.Close()
					return NewExtractionError(err, compressedFilePath, destDir)
				}
				out.Close()

				// Set file permissions from tar header
				if err := os.Chmod(target, hdr.FileInfo().Mode()); err != nil {
					return NewExtractionError(err, compressedFilePath, destDir)
				}
			default:
				return NewExtractionError(fmt.Errorf("unknown type flag: %c", hdr.Typeflag), compressedFilePath, destDir)
			}
		}
	}
}
