package software

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.hedera.com/solo-provisioner/internal/core"
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

// checksum verifies the hash of a file
// hashType is the hash function to use, e.g. md5.New(), sha256.New(), sha512.New()
func (fd *Downloader) Checksum(filePath string, expectedHash string, hashFunction hash.Hash) error {
	file, err := os.Open(filePath)
	if err != nil {
		return NewFileNotFoundError(filePath)
	}
	defer file.Close()

	if _, err := io.Copy(hashFunction, file); err != nil {
		return NewChecksumError(filePath, "unknown", expectedHash, "")
	}

	calculatedHash := fmt.Sprintf("%x", hashFunction.Sum(nil))
	if calculatedHash != expectedHash {
		return NewChecksumError(filePath, "unknown", expectedHash, calculatedHash)
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
				if err := os.MkdirAll(target, core.DefaultFilePerm); err != nil {
					return NewExtractionError(err, compressedFilePath, destDir)
				}
			case tar.TypeReg:
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
			default:
				return NewExtractionError(fmt.Errorf("unknown type flag: %c", hdr.Typeflag), compressedFilePath, destDir)
			}
		}
	}
}

// VerifyChecksum dynamically verifies the checksum of a file using the specified algorithm
func (fd *Downloader) VerifyChecksum(filePath string, expectedValue string, algorithm string) error {
	switch algorithm {
	case "md5":
		return fd.Checksum(filePath, expectedValue, md5.New())
	case "sha256":
		return fd.Checksum(filePath, expectedValue, sha256.New())
	case "sha512":
		return fd.Checksum(filePath, expectedValue, sha512.New())
	default:
		return NewChecksumError(filePath, algorithm, expectedValue, "")
	}
}
