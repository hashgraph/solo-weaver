// SPDX-License-Identifier: Apache-2.0

package software

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
)

// Default retry settings for DownloadAndVerify.
const (
	defaultMaxAttempts = 3               // initial attempt + 2 retries
	defaultRetryDelay  = 1 * time.Second // base delay; grows exponentially per attempt
	maxRetryDelay      = 30 * time.Second
)

// Downloader is responsible for downloading a software package and checking its integrity.
type Downloader struct {
	client         *http.Client
	timeout        time.Duration
	basePath       string        // Base directory for validating download/extraction paths
	allowedDomains []string      // List of allowed domains for SSRF protection
	insecureTLS    bool          // Skip TLS certificate verification (for local dev with self-signed certs)
	maxAttempts    int           // Number of download+verify attempts before giving up
	retryDelay     time.Duration // Base backoff delay between attempts (0 disables sleeping)
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

// WithMaxAttempts sets the number of download+verify attempts before giving up.
// Values below 1 are clamped to 1.
func WithMaxAttempts(attempts int) DownloaderOption {
	return func(d *Downloader) {
		if attempts < 1 {
			attempts = 1
		}
		d.maxAttempts = attempts
	}
}

// WithRetryDelay sets the base backoff delay between download attempts.
// A zero delay disables sleeping (useful for tests).
func WithRetryDelay(delay time.Duration) DownloaderOption {
	return func(d *Downloader) {
		d.retryDelay = delay
	}
}

// WithInsecureTLS skips TLS certificate verification.
// WARNING: Only use this for local development with self-signed certificates!
// This option is ignored in release builds for security.
func WithInsecureTLS(insecure bool) DownloaderOption {
	return func(d *Downloader) {
		d.insecureTLS = insecure
	}
}

// validateRedirect validates redirect URLs to prevent redirect-based SSRF attacks
// where an attacker redirects from a trusted domain to an internal service.
func (fd *Downloader) validateRedirect(req *http.Request, via []*http.Request) error {
	// Limit the number of redirects to prevent redirect loops
	if len(via) >= 10 {
		return errorx.RejectedOperation.New("stopped after 10 redirects")
	}

	// Validate the redirect URL against the allowlist
	if err := sanity.ValidateURL(req.URL.String(), &sanity.ValidateURLOptions{AllowedDomains: fd.allowedDomains}); err != nil {
		return errorx.RejectedOperation.Wrap(err, "redirect to untrusted domain")
	}

	return nil
}

// NewDownloader creates a new Downloader with default settings and optional configurations
func NewDownloader(opts ...DownloaderOption) *Downloader {
	// Set defaults
	// Use HomeDir as basePath since downloads and extractions span multiple folders under home
	downloader := &Downloader{
		timeout:        30 * time.Minute,
		basePath:       models.Paths().HomeDir,
		allowedDomains: sanity.AllowedDomains(),
		insecureTLS:    false,
		maxAttempts:    defaultMaxAttempts,
		retryDelay:     defaultRetryDelay,
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

		// Configure TLS if insecure mode is requested (for local dev with self-signed certs)
		if downloader.insecureTLS {
			transport.TLSClientConfig = &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // Intentional for local dev only
			}
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
	if err := sanity.ValidateURL(url, &sanity.ValidateURLOptions{AllowedDomains: fd.allowedDomains}); err != nil {
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

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		_ = os.Remove(cleanDest)
		return NewDownloadError(err, url, 0)
	}

	// Reject a zero-byte body: a 200-with-empty-response is a corrupt download,
	// not a valid file. This is treated as a retryable download error.
	if written == 0 {
		_ = os.Remove(cleanDest)
		return NewDownloadError(errorx.ExternalError.New("downloaded file is empty (0 bytes written)"), url, 0)
	}

	// When the response advertises a Content-Length, fail fast on a size mismatch
	// (a truncated copy or mid-stream reset) rather than persisting a partial file.
	// ContentLength is -1 when unknown (e.g. chunked transfer), so only check when >= 0.
	if resp.ContentLength >= 0 && written != resp.ContentLength {
		_ = os.Remove(cleanDest)
		return NewDownloadError(
			errorx.ExternalError.New("download truncated: wrote %d of %d advertised bytes", written, resp.ContentLength),
			url, 0)
	}

	return nil
}

// DownloadAndVerify downloads a file from url to destination and verifies its
// checksum, retrying transient corruption. Both download errors (network reset,
// empty/truncated body) and a subsequent checksum mismatch are treated as
// retryable: on failure the bad file is removed and the download is re-attempted
// with capped exponential backoff, up to maxAttempts. The last error is returned
// once attempts are exhausted.
func (fd *Downloader) DownloadAndVerify(url, destination, expectedValue, algorithm string) error {
	attempts := fd.maxAttempts
	if attempts < 1 {
		attempts = 1
	}

	// Sanitize the destination once so the download, verification, and cleanup all
	// operate on the exact same path (Download sanitizes internally, so a caller
	// passing a relative/unclean path would otherwise write to one path and verify
	// or remove another).
	cleanDest, err := sanity.ValidatePathWithinBase(fd.basePath, destination)
	if err != nil {
		return NewDownloadError(err, url, 0)
	}

	// Fail fast on deterministic errors that no retry can fix: an invalid/unsafe URL
	// and an unsupported checksum algorithm will fail identically on every attempt,
	// so retrying them only adds backoff delay and log noise.
	if err := sanity.ValidateURL(url, &sanity.ValidateURLOptions{AllowedDomains: fd.allowedDomains}); err != nil {
		return NewInvalidURLError(err, url)
	}
	if !IsSupportedAlgorithm(algorithm) {
		return NewChecksumError(cleanDest, algorithm, expectedValue, "")
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		lastErr = fd.Download(url, cleanDest)
		if lastErr == nil {
			lastErr = VerifyChecksum(cleanDest, expectedValue, algorithm)
			if lastErr == nil {
				return nil
			}
		}

		// Remove the corrupt/partial file so a bad download never persists and the
		// next attempt (or a later run) starts clean.
		_ = os.Remove(cleanDest)

		if attempt < attempts {
			logx.As().Warn().
				Str("url", url).
				Int("attempt", attempt).
				Int("maxAttempts", attempts).
				Err(lastErr).
				Msg("Download or checksum verification failed; retrying after backoff")
			fd.sleepBackoff(attempt)
		}
	}

	return lastErr
}

// sleepBackoff sleeps for a capped exponential delay before the next attempt.
// A zero base retryDelay disables sleeping.
func (fd *Downloader) sleepBackoff(attempt int) {
	if fd.retryDelay <= 0 {
		return
	}

	delay := fd.retryDelay << (attempt - 1)
	if delay > maxRetryDelay || delay < fd.retryDelay {
		delay = maxRetryDelay
	}
	time.Sleep(delay)
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

			// Validate that the target path is within the extraction directory
			if _, err := sanity.ValidatePathWithinBase(cleanDestDir, targetPath); err != nil {
				return NewExtractionError(errorx.IllegalState.New("path traversal attempt in hdr.ServerName: %s", hdr.Name), cleanCompressedPath, cleanDestDir)
			}

			switch hdr.Typeflag {
			case tar.TypeDir:
				// Create directories
				if err := os.MkdirAll(targetPath, models.DefaultDirOrExecPerm); err != nil {
					return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
				}
			case tar.TypeReg:
				// Ensure parent directory exists
				if err := os.MkdirAll(filepath.Dir(targetPath), models.DefaultDirOrExecPerm); err != nil {
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
			case tar.TypeSymlink:
				// Validate symlink target to prevent path traversal attacks
				// Reject absolute paths in symlink targets
				if filepath.IsAbs(hdr.Linkname) {
					return NewExtractionError(errorx.IllegalState.New("absolute symlink target not allowed: %s -> %s", hdr.Name, hdr.Linkname), cleanCompressedPath, cleanDestDir)
				}

				// Resolve the symlink target relative to the symlink's parent directory
				// and verify it stays within the extraction directory
				symlinkDir := filepath.Dir(targetPath)
				resolvedTarget := filepath.Join(symlinkDir, hdr.Linkname)
				if _, err := sanity.ValidatePathWithinBase(cleanDestDir, resolvedTarget); err != nil {
					return NewExtractionError(errorx.IllegalState.New("symlink target escapes extraction directory: %s -> %s", hdr.Name, hdr.Linkname), cleanCompressedPath, cleanDestDir)
				}

				// Ensure parent directory exists
				if err := os.MkdirAll(filepath.Dir(targetPath), models.DefaultDirOrExecPerm); err != nil {
					return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
				}

				// Remove existing symlink if it exists
				if _, err := os.Lstat(targetPath); err == nil {
					if err := os.Remove(targetPath); err != nil {
						return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
					}
				}

				// Create symlink - hdr.Linkname contains the target of the symlink
				if err := os.Symlink(hdr.Linkname, targetPath); err != nil {
					return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
				}
			case tar.TypeLink:
				// Validate hardlink target to prevent path traversal attacks
				// Reject absolute paths in hardlink targets
				if filepath.IsAbs(hdr.Linkname) {
					return NewExtractionError(errorx.IllegalState.New("absolute hardlink target not allowed: %s -> %s", hdr.Name, hdr.Linkname), cleanCompressedPath, cleanDestDir)
				}

				// Hard links - hdr.Linkname contains the target path (relative to archive root)
				linkTarget := filepath.Join(cleanDestDir, hdr.Linkname)
				if _, err := sanity.ValidatePathWithinBase(cleanDestDir, linkTarget); err != nil {
					return NewExtractionError(errorx.IllegalState.New("hardlink target escapes extraction directory: %s -> %s", hdr.Name, hdr.Linkname), cleanCompressedPath, cleanDestDir)
				}

				// Ensure parent directory exists
				if err := os.MkdirAll(filepath.Dir(targetPath), models.DefaultDirOrExecPerm); err != nil {
					return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
				}

				// Remove existing file if it exists
				if _, err := os.Lstat(targetPath); err == nil {
					if err := os.Remove(targetPath); err != nil {
						return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
					}
				}

				// Create hard link
				if err := os.Link(linkTarget, targetPath); err != nil {
					return NewExtractionError(err, cleanCompressedPath, cleanDestDir)
				}
			default:
				// Skip unknown type flags instead of failing
				// This allows extracting archives with special file types we don't handle
				logx.As().Warn().
					Str("archive", cleanCompressedPath).
					Str("entry", hdr.Name).
					Int("typeFlag", int(hdr.Typeflag)).
					Msg("Skipping unknown tar entry type during extraction")
				continue
			}
		}
	}
}
