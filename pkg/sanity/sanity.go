// SPDX-License-Identifier: Apache-2.0

package sanity

import (
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

// Filename sanitize the input string to be safe filename
// It only allows alphanumeric characters (a-z, 0-9) and underscore
// It returns error if the filename is empty string after the sanitization
func Filename(s string) (string, error) {
	sb := []byte(s)
	j := 0
	for _, b := range sb {
		if ('a' <= b && b <= 'z') ||
			('A' <= b && b <= 'Z') ||
			('0' <= b && b <= '9') ||
			b == '_' ||
			b == '-' {
			sb[j] = b
			j++
		}
	}

	if j == 0 {
		return "", ErrInvalidFilename
	}

	return string(sb[:j]), nil
}

// SanitizePath validates and sanitizes the given path according to strict security rules.
//
// Specifically, it:
//  1. Rejects paths containing shell metacharacters (e.g., ; & | $ ` < > ( ) { } [ ] * ? ~).
//  2. Rejects path traversal attempts (e.g., segments like "../", "/..", or paths ending with "..").
//  3. Requires the input path to be absolute.
//  4. Normalizes the path by removing redundant slashes and dot directories (using filepath.Clean).
//  5. May return a cleaned version of the input path that differs from the original.
//
// Returns the sanitized (cleaned) path, or an error if the input is invalid or unsafe.
func SanitizePath(path string) (string, error) {
	if path == "" {
		return "", errorx.IllegalArgument.New("path cannot be empty")
	}

	// Ensure it's an absolute path
	if !filepath.IsAbs(path) {
		return "", errorx.IllegalArgument.New("path must be absolute: %s", path)
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

	// Check for shell metacharacters in the original path
	if shellMetachars.MatchString(path) {
		return "", errorx.IllegalArgument.New("path contains shell metacharacters: %s", path)
	}

	// Check for valid characters in the original path
	if !validPathChars.MatchString(path) {
		return "", errorx.IllegalArgument.New("path contains invalid characters: %s", path)
	}

	return filepath.Clean(path), nil
}
