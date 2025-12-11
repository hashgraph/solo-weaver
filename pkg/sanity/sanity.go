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

// Filename sanitize the input string to be safe filename
// It only allows alphanumeric characters (a-z, A-Z, 0-9), underscores, and hyphens
// It returns error if the filename is empty string after the sanitization
func Filename(s string) (string, error) {
	sanitized, count := filterValidIdentifierChars(s)
	if count == 0 {
		return "", ErrInvalidFilename
	}
	return sanitized, nil
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
