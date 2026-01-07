// SPDX-License-Identifier: Apache-2.0

package sanity

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/joomcode/errorx"
)

var (
	ErrInvalidFilename = errorx.IllegalArgument.New("invalid filename")
)

// Security validation patterns for paths
var (
	// shellMetachars contains dangerous shell metacharacters that should be rejected
	shellMetachars = regexp.MustCompile(`[;&|$\x60<>(){}[\]*?~]`)

	// validPathChars ensures paths only contain safe characters
	// Allows: alphanumeric, forward slash, dash, underscore, dot
	validPathChars = regexp.MustCompile(`^[a-zA-Z0-9/_.\-]+$`)
)

// Alphanumeric ensures the input string to be ascii alphanumeric
func Alphanumeric(s string) string {
	sb := []byte(s)
	j := 0
	for _, b := range sb {
		if ('a' <= b && b <= 'z') ||
			('A' <= b && b <= 'Z') ||
			('0' <= b && b <= '9') {
			sb[j] = b
			j++
		}
	}
	return string(sb[:j])
}

// isValidIdentifierChar checks if a byte is a valid character for identifiers
// (alphanumeric, underscore, or hyphen)
func isValidIdentifierChar(b byte) bool {
	return ('a' <= b && b <= 'z') ||
		('A' <= b && b <= 'Z') ||
		('0' <= b && b <= '9') ||
		b == '_' ||
		b == '-'
}

// filterValidIdentifierChars filters a string to contain only valid identifier characters
// Returns the filtered string and the count of valid characters
func filterValidIdentifierChars(s string) (string, int) {
	sb := []byte(s)
	j := 0
	for _, b := range sb {
		if isValidIdentifierChar(b) {
			sb[j] = b
			j++
		}
	}
	return string(sb[:j]), j
}

// Identifier validates and sanitizes a string to be a safe identifier.
// It only allows alphanumeric characters (a-z, A-Z, 0-9), underscores, and hyphens.
// This is useful for validating module names, filenames, usernames, and other identifiers.
// Returns an error if the identifier is empty or contains no valid characters after sanitization.
func Identifier(s string) (string, error) {
	sanitized, count := filterValidIdentifierChars(s)
	if count == 0 {
		return "", ErrInvalidFilename
	}
	return sanitized, nil
}

// ValidateIdentifier validates that a string contains only safe identifier characters
// without sanitizing/modifying it. It rejects any string that contains invalid characters.
// This is stricter than Identifier() which sanitizes by removing invalid characters.
// Use this when you need to ensure the input is already clean and reject invalid input.
func ValidateIdentifier(s string) error {
	if s == "" {
		return errorx.IllegalArgument.New("identifier cannot be empty")
	}

	// Check if all characters are valid
	for i := 0; i < len(s); i++ {
		if !isValidIdentifierChar(s[i]) {
			return errorx.IllegalArgument.New("identifier contains invalid characters: %s", s)
		}
	}

	return nil
}

// Filename is an alias for Identifier
func Filename(s string) (string, error) {
	return Identifier(s)
}

// ModuleName is an alias for Identifier
func ModuleName(s string) (string, error) {
	return Identifier(s)
}

// Username validates and sanitizes a username string to prevent security vulnerabilities.
//
// This function is particularly important when dealing with environment variables like SUDO_USER
// that could be manipulated by attackers. It ensures that the username:
//  1. Is not empty (precondition check)
//  2. Contains only alphanumeric characters (a-z, A-Z, 0-9), underscores, and hyphens
//  3. Does not contain path traversal sequences (e.g., "..", "/")
//  4. Does not contain shell metacharacters or special characters
//  5. Contains at least one valid character after sanitization
//
// Returns the sanitized username, or an error if the username is invalid or unsafe.
func Username(s string) (string, error) {
	if s == "" {
		return "", errorx.IllegalArgument.New("username cannot be empty")
	}

	// Check for path traversal attempts
	if strings.Contains(s, "..") {
		return "", errorx.IllegalArgument.New("username contains path traversal sequences: %s", s)
	}

	// Check for shell metacharacters
	if shellMetachars.MatchString(s) {
		return "", errorx.IllegalArgument.New("username contains shell metacharacters: %s", s)
	}

	// Sanitize: only allow alphanumeric, underscore, and hyphen
	sanitized, count := filterValidIdentifierChars(s)

	if count == 0 {
		return "", errorx.IllegalArgument.New("username contains no valid characters")
	}

	// Verify the sanitized version matches the original
	// This ensures no characters were removed during sanitization
	if sanitized != s {
		return "", errorx.IllegalArgument.New("username contains invalid characters: %s", s)
	}

	return sanitized, nil
}

// SanitizePath validates and sanitizes the given path according to strict security rules.
//
// Specifically, it:
//  1. Rejects paths containing shell metacharacters (e.g., ; & | $ ` < > ( ) { } [ ] * ? ~).
//  2. Rejects path traversal attempts (e.g., segments like "../", "/..", or paths ending with "..").
//  3. Converts relative paths to absolute paths.
//  4. Normalizes the path by removing redundant slashes and dot directories (using filepath.Clean).
//  5. May return a cleaned version of the input path that differs from the original.
//
// Returns the sanitized (cleaned) absolute path, or an error if the input is invalid or unsafe.
func SanitizePath(path string) (string, error) {
	if path == "" {
		return "", errorx.IllegalArgument.New("path cannot be empty")
	}

	// Check for path traversal patterns BEFORE cleaning
	// This catches patterns like "../", "/..", and paths ending with ".."
	// which could allow escaping the intended directory structure
	// Check for ".." as a path segment
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return "", errorx.IllegalArgument.New("path cannot contain '..' segments: %s", path)
		}
	}

	// Convert to absolute path if not already
	absPath := path
	if !filepath.IsAbs(path) {
		var err error
		absPath, err = filepath.Abs(path)
		if err != nil {
			return "", errorx.IllegalArgument.Wrap(err, "failed to resolve file path: %s", path)
		}
	}

	// Check for shell metacharacters in the original path
	if shellMetachars.MatchString(absPath) {
		return "", errorx.IllegalArgument.New("path contains shell metacharacters: %s", path)
	}

	// Check for valid characters in the original path
	if !validPathChars.MatchString(absPath) {
		return "", errorx.IllegalArgument.New("path contains invalid characters: %s", path)
	}

	return filepath.Clean(absPath), nil
}

// ValidateURL validates a URL to ensure it's safe to use for downloads.
//
// This function provides SSRF (Server-Side Request Forgery) protection by checking that:
//  1. The URL is not empty and can be parsed
//  2. The scheme is HTTPS only (HTTP is rejected for security)
//  3. The host is not empty
//  4. The host is in the allowed domain list for trusted registries
//
// Returns an error if the URL is invalid or unsafe.
func ValidateURL(rawURL string, allowedDomains []string) error {
	if rawURL == "" {
		return errorx.IllegalArgument.New("URL cannot be empty")
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return errorx.IllegalArgument.New("invalid URL: %s", err.Error())
	}

	// Only allow HTTPS scheme for security (reject HTTP)
	if parsedURL.Scheme != "https" {
		return errorx.IllegalArgument.New("URL scheme must be https for security, got: %s", parsedURL.Scheme)
	}

	// Ensure host is not empty
	if parsedURL.Host == "" {
		return errorx.IllegalArgument.New("URL must have a valid host")
	}

	// Extract hostname without port
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return errorx.IllegalArgument.New("URL must have a valid hostname")
	}

	// Validate against allowed domains - this is our primary security control
	if !isAllowedDomain(hostname, allowedDomains) {
		return errorx.IllegalArgument.New("URL host %s is not in the allowed domain list", hostname)
	}

	return nil
}

// isASCII checks if a string contains only ASCII characters.
// This prevents Unicode homograph attacks where visually similar characters
// from different scripts could be used to spoof trusted domains.
// For example: "githսb.com" using Armenian 'ս' instead of Latin 'u'.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

// isAllowedDomain checks if a hostname is in the allowlist of trusted domains.
// This implements a domain allowlist strategy to prevent downloads from untrusted sources.
//
// SECURITY: This function protects against:
//  1. Untrusted domain access - only allowlisted domains are permitted
//  2. Unicode homograph attacks - non-ASCII characters are rejected
//  3. Case variations - domains are normalized to lowercase
func isAllowedDomain(hostname string, allowedDomains []string) bool {
	// Reject non-ASCII hostnames to prevent Unicode homograph attacks
	// This prevents attacks like using "githսb.com" (Armenian 'ս') instead of "github.com"
	if !isASCII(hostname) {
		return false
	}

	// Normalize hostname to lowercase
	hostname = strings.ToLower(hostname)

	// Check exact match
	for _, allowed := range allowedDomains {
		// Normalize allowed domain to lowercase for robust comparison
		// This prevents future bugs if uppercase domains are added to the allowlist
		allowedLower := strings.ToLower(allowed)

		if hostname == allowedLower {
			return true
		}
		// Also check if it's a subdomain of an allowed domain
		if strings.HasSuffix(hostname, "."+allowedLower) {
			return true
		}
	}

	return false
}

// ValidatePathWithinBase validates that a path is within a specific base directory.
//
// This function:
//  1. Sanitizes the input path
//  2. Ensures the sanitized path starts with the base directory
//  3. Prevents path traversal outside the base directory
//
// Returns the sanitized path or an error if the path is outside the base directory.
func ValidatePathWithinBase(basePath, targetPath string) (string, error) {
	if basePath == "" {
		return "", errorx.IllegalArgument.New("base path cannot be empty")
	}

	if targetPath == "" {
		return "", errorx.IllegalArgument.New("target path cannot be empty")
	}

	// Sanitize the target path
	cleanTarget, err := SanitizePath(targetPath)
	if err != nil {
		return "", err
	}

	// Clean the base path
	cleanBase := filepath.Clean(basePath)

	// Ensure the clean base ends with a separator for prefix matching
	// This prevents /opt/solo/weaver/tmp from matching /opt/solo/weaver/tmp-evil
	if !strings.HasSuffix(cleanBase, string(filepath.Separator)) {
		cleanBase += string(filepath.Separator)
	}

	// Check if the clean target starts with the clean base
	if !strings.HasPrefix(cleanTarget+string(filepath.Separator), cleanBase) {
		return "", errorx.IllegalArgument.New("path '%s' is outside the allowed base directory '%s'", cleanTarget, basePath)
	}

	return cleanTarget, nil
}

// ValidateInputFile validates a file path intended for reading user-provided input files.
//
// This function provides comprehensive validation to prevent path traversal attacks
// and ensure the file is safe to read. It:
//  1. Converts relative paths to absolute paths
//  2. Sanitizes the path to prevent path traversal and shell injection
//  3. Verifies the file exists
//  4. Ensures the path points to a regular file (not a directory, device, socket, etc.)
//
// This is designed to be used in defense-in-depth scenarios where the same validation
// is applied at multiple layers (CLI entry point and internal APIs).
//
// Returns the sanitized absolute path or an error if validation fails.
func ValidateInputFile(filePath string) (string, error) {
	if filePath == "" {
		return "", errorx.IllegalArgument.New("file path cannot be empty")
	}

	// Sanitize the path to prevent path traversal and shell injection attacks
	sanitizedPath, err := SanitizePath(filePath)
	if err != nil {
		return "", errorx.IllegalArgument.Wrap(err, "invalid file path: %s", filePath)
	}

	// Verify file exists and get file info
	fileInfo, err := os.Stat(sanitizedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errorx.IllegalArgument.New("file does not exist: %s", sanitizedPath)
		}
		return "", errorx.InternalError.Wrap(err, "failed to stat file: %s", sanitizedPath)
	}

	// Ensure it's a regular file (not a directory, device, socket, etc.).
	// Symlinks are followed; symlinks to regular files are allowed.
	// This prevents attacks using special files like /dev/zero or named pipes.
	if !fileInfo.Mode().IsRegular() {
		return "", errorx.IllegalArgument.New("path is not a regular file: %s", sanitizedPath)
	}

	return sanitizedPath, nil
}

// ValidateVersion validates a semantic version string to ensure it's safe to use.
// Accepts versions like: "1.0.0", "1.0.0-alpha", "1.0.0-beta.1", "0.24.0", etc.
// This prevents injection attacks through version parameters.
// From the bottom of the page at https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string
func ValidateVersion(version string) error {
	if version == "" {
		return errorx.IllegalArgument.New("version cannot be empty")
	}

	// Semantic version pattern: digits, dots, hyphens, and alphanumeric for pre-release/build metadata
	// Examples: 1.0.0, 1.0.0-alpha, 1.0.0-beta.1, 1.0.0+build.123
	validVersionPattern := regexp.MustCompile(`^(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)(?:-(?P<prerelease>(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)
	if !validVersionPattern.MatchString(version) {
		return errorx.IllegalArgument.New("version contains invalid characters: %s", version)
	}

	return nil
}

// ValidateChartReference validates a Helm chart reference (OCI URL or repo/chart name).
// This prevents injection attacks through chart parameters while allowing legitimate chart references.
// Accepts:
//   - OCI references: oci://registry.example.com/path/to/chart
//   - Repository URLs: https://charts.example.com/chart-name
//   - Simple chart names: my-chart, repo/chart-name
func ValidateChartReference(chart string) error {
	if chart == "" {
		return errorx.IllegalArgument.New("chart reference cannot be empty")
	}

	// Check for dangerous shell metacharacters that could enable command injection
	// Note: We allow some characters like : / @ for valid OCI and URL references
	dangerousChars := regexp.MustCompile(`[;&|$\x60<>(){}[\]*?~\s]`)
	if dangerousChars.MatchString(chart) {
		return errorx.IllegalArgument.New("chart reference contains invalid characters: %s", chart)
	}

	// If it's an OCI reference, validate the OCI URL format
	if strings.HasPrefix(chart, "oci://") {
		return validateOCIReference(chart)
	}

	// If it's an HTTP/HTTPS URL, validate as URL
	if strings.HasPrefix(chart, "https://") || strings.HasPrefix(chart, "http://") {
		// Parse as URL to ensure it's well-formed
		_, err := url.Parse(chart)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid chart URL: %s", chart)
		}
		return nil
	}

	// Otherwise, it's a simple chart name or repo/chart format
	// Allow alphanumeric, hyphens, underscores, dots, and forward slashes
	validChartPattern := regexp.MustCompile(`^[a-zA-Z0-9.\-_/]+$`)
	if !validChartPattern.MatchString(chart) {
		return errorx.IllegalArgument.New("chart name contains invalid characters: %s", chart)
	}

	return nil
}

// validateOCIReference validates an OCI registry reference
func validateOCIReference(ociRef string) error {
	// Remove the oci:// prefix for validation
	ref := strings.TrimPrefix(ociRef, "oci://")
	if ref == "" {
		return errorx.IllegalArgument.New("OCI reference missing registry path")
	}

	// OCI reference format: registry.example.com[:port]/path/to/chart[:tag][@digest]
	// We'll validate this has reasonable structure without being too strict
	// since Helm itself will do thorough validation

	// Basic structure check: should have at least a registry and path
	parts := strings.Split(ref, "/")
	if len(parts) < 2 {
		return errorx.IllegalArgument.New("OCI reference must include registry and path: %s", ociRef)
	}

	// Validate the registry part (first component)
	registry := parts[0]
	if registry == "" {
		return errorx.IllegalArgument.New("OCI reference missing registry: %s", ociRef)
	}

	// Registry should be alphanumeric with dots, hyphens, and optional :port
	validRegistryPattern := regexp.MustCompile(`^[a-zA-Z0-9.\-]+(:[0-9]+)?$`)
	if !validRegistryPattern.MatchString(registry) {
		return errorx.IllegalArgument.New("invalid OCI registry format: %s", registry)
	}

	return nil
}

// ValidateStorageSize validates a Kubernetes storage size string.
// Accepts sizes like: "5Gi", "10Mi", "1Ti", "100Gi", etc.
// This prevents injection attacks through storage size parameters while ensuring
// the size matches Kubernetes quantity format requirements.
// The numeric value must be greater than zero.
func ValidateStorageSize(size string) error {
	if size == "" {
		return errorx.IllegalArgument.New("storage size cannot be empty")
	}

	// Kubernetes storage size pattern: number followed by unit (Gi, Mi, or Ti)
	// Examples: 5Gi, 10Mi, 1Ti, 100Gi
	validStorageSizePattern := regexp.MustCompile(`^([0-9]+)(Gi|Mi|Ti)$`)
	matches := validStorageSizePattern.FindStringSubmatch(size)
	if matches == nil {
		return errorx.IllegalArgument.New("storage size must be in format '<number>(Gi|Mi|Ti)', got: %s", size)
	}

	// Extract the numeric part (first capture group)
	numericPart := matches[1]

	// Check if the numeric value is zero
	// We need to check if all characters are '0'
	allZeros := true
	for _, ch := range numericPart {
		if ch != '0' {
			allZeros = false
			break
		}
	}

	if allZeros {
		return errorx.IllegalArgument.New("storage size must be greater than zero, got: %s", size)
	}

	return nil
}

func Contains[T comparable](item T, slice []T) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}
