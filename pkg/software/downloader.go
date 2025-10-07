package software

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/joomcode/errorx"
)

// FileDownloader is the default implementation of the Downloader interface
type FileDownloader struct {
	client  *http.Client
	timeout time.Duration
}

// NewFileDownloader creates a new FileDownloader with default settings
func NewFileDownloader() *FileDownloader {
	return &FileDownloader{
		client: &http.Client{
			Timeout: 30 * time.Minute, // Default timeout for large downloads
		},
		timeout: 30 * time.Minute,
	}
}

// NewFileDownloaderWithTimeout creates a new FileDownloader with custom timeout
func NewFileDownloaderWithTimeout(timeout time.Duration) *FileDownloader {
	return &FileDownloader{
		client: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// Download downloads a file from the given URL to the specified destination
func (fd *FileDownloader) Download(url, destination string) error {
	resp, err := fd.client.Get(url)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to download from URL: %s", url)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errorx.IllegalState.New("failed to download: HTTP %d %s", resp.StatusCode, resp.Status)
	}

	out, err := os.Create(destination)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create destination file: %s", destination)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write downloaded content to file: %s", destination)
	}

	return nil
}

// VerifyMD5 verifies the MD5 hash of a file
func (fd *FileDownloader) VerifyMD5(filePath, expectedMD5 string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to open file for MD5 verification: %s", filePath)
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to calculate MD5 hash for file: %s", filePath)
	}

	calculatedMD5 := fmt.Sprintf("%x", hash.Sum(nil))
	if calculatedMD5 != expectedMD5 {
		return errorx.IllegalState.New("MD5 hash mismatch for file %s: got %s, expected %s", filePath, calculatedMD5, expectedMD5)
	}

	return nil
}

// VerifySHA256 verifies the SHA256 hash of a file
func (fd *FileDownloader) VerifySHA256(filePath, expectedSHA256 string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to open file for SHA256 verification: %s", filePath)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to calculate SHA256 hash for file: %s", filePath)
	}

	calculatedSHA256 := fmt.Sprintf("%x", hash.Sum(nil))
	if calculatedSHA256 != expectedSHA256 {
		return errorx.IllegalState.New("SHA256 hash mismatch for file %s: got %s, expected %s", filePath, calculatedSHA256, expectedSHA256)
	}

	return nil
}

// VerifySHA512 verifies the SHA512 hash of a file
func (fd *FileDownloader) VerifySHA512(filePath, expectedSHA512 string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to open file for SHA512 verification: %s", filePath)
	}
	defer file.Close()

	hash := sha512.New()
	if _, err := io.Copy(hash, file); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to calculate SHA512 hash for file: %s", filePath)
	}

	calculatedSHA512 := fmt.Sprintf("%x", hash.Sum(nil))
	if calculatedSHA512 != expectedSHA512 {
		return errorx.IllegalState.New("SHA512 hash mismatch for file %s: got %s, expected %s", filePath, calculatedSHA512, expectedSHA512)
	}

	return nil
}

// ExtractTarGz extracts a tar.gz file
func (fd *FileDownloader) ExtractTarGz(gzPath, destDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fd.timeout)
	defer cancel()

	file, err := os.Open(gzPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to open tar.gz file: %s", gzPath)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create gzip reader for file: %s", gzPath)
	}
	defer gz.Close()

	tarReader := tar.NewReader(gz)
	for {
		select {
		case <-ctx.Done():
			return errorx.IllegalState.New("timeout while extracting tar.gz file: %s", gzPath)
		default:
			hdr, err := tarReader.Next()
			if err == io.EOF {
				return nil // End of archive
			}
			if err != nil {
				return errorx.IllegalState.Wrap(err, "failed to read next file from tar.gz archive: %s", gzPath)
			}

			target := filepath.Join(destDir, hdr.Name)
			switch hdr.Typeflag {
			case tar.TypeDir:
				// Create directories
				if err := os.MkdirAll(target, 0755); err != nil {
					return errorx.IllegalState.Wrap(err, "failed to create directory from tar.gz archive: %s", target)
				}
			case tar.TypeReg:
				// Extract files
				out, err := os.Create(target)
				if err != nil {
					return errorx.IllegalState.Wrap(err, "failed to create file from tar.gz archive: %s", target)
				}
				if _, err := io.Copy(out, tarReader); err != nil {
					out.Close()
					return errorx.IllegalState.Wrap(err, "failed to write file from tar.gz archive: %s", target)
				}
				out.Close()
			default:
				return errorx.IllegalState.New("unknown type flag in tar.gz archive: %c", hdr.Typeflag)
			}
		}
	}
}

// ExtractZip extracts a zip file
func (fd *FileDownloader) ExtractZip(zipPath, destDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fd.timeout)
	defer cancel()

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to open zip file: %s", zipPath)
	}
	defer r.Close()

	for _, f := range r.File {
		select {
		case <-ctx.Done():
			return errorx.IllegalState.New("timeout while extracting zip file: %s", zipPath)
		default:
			fpath := filepath.Join(destDir, f.Name)
			switch f.FileInfo().Mode() & os.ModeType {
			case os.ModeDir:
				// Create directories
				if err := os.MkdirAll(fpath, 0755); err != nil {
					return errorx.IllegalState.Wrap(err, "failed to create directory from zip archive: %s", fpath)
				}
			default:
				// Extract files
				out, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
				if err != nil {
					return errorx.IllegalState.Wrap(err, "failed to create file from zip archive: %s", fpath)
				}
				rc, err := f.Open()
				if err != nil {
					out.Close()
					return errorx.IllegalState.Wrap(err, "failed to open file inside zip archive: %s", fpath)
				}
				_, err = io.Copy(out, rc)
				rc.Close()
				if err != nil {
					out.Close()
					return errorx.IllegalState.Wrap(err, "failed to write file from zip archive: %s", fpath)
				}
				out.Close()
			}
		}
	}

	return nil
}
