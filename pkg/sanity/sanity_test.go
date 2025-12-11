// SPDX-License-Identifier: Apache-2.0

package sanity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanity_Alphanumeric(t *testing.T) {
	req := require.New(t)
	testCases := []struct {
		input  string
		output string
	}{
		{
			input:  "a,bc9",
			output: "abc9",
		},
		{
			input:  "a-,bc_9!",
			output: "abc9",
		},
		{
			input:  "",
			output: "",
		},
	}

	for _, testCase := range testCases {
		req.Equal(testCase.output, Alphanumeric(testCase.input), testCase.input)

	}
}

func TestSanity_Filename(t *testing.T) {
	req := require.New(t)
	testCases := []struct {
		input  string
		output string
		err    error
	}{
		{
			input:  "a,bc9",
			output: "abc9",
		},
		{
			input:  "_a-,bc_9!",
			output: "_a-bc_9",
		},
		{
			input:  "\\u2318",
			output: "u2318",
		},
		{
			input:  "日本語",
			output: "",
			err:    ErrInvalidFilename,
		},
		{
			input:  "⌘",
			output: "",
			err:    ErrInvalidFilename,
		},
		{
			input:  "",
			output: "",
			err:    ErrInvalidFilename,
		},
	}

	for _, testCase := range testCases {
		output, err := Filename(testCase.input)
		req.Equal(testCase.output, output, testCase.input)
		req.Equal(testCase.err, err, testCase.input)
	}
}

func TestSanity_Username(t *testing.T) {
	testCases := []struct {
		name      string
		input     string
		expected  string
		shouldErr bool
		errMsg    string
	}{
		// Valid usernames
		{
			name:      "valid simple username",
			input:     "john",
			expected:  "john",
			shouldErr: false,
		},
		{
			name:      "valid username with underscore",
			input:     "john_doe",
			expected:  "john_doe",
			shouldErr: false,
		},
		{
			name:      "valid username with hyphen",
			input:     "john-doe",
			expected:  "john-doe",
			shouldErr: false,
		},
		{
			name:      "valid username with numbers",
			input:     "user123",
			expected:  "user123",
			shouldErr: false,
		},
		{
			name:      "valid username with mixed case",
			input:     "JohnDoe",
			expected:  "JohnDoe",
			shouldErr: false,
		},
		{
			name:      "valid username with all allowed characters",
			input:     "user_123-test",
			expected:  "user_123-test",
			shouldErr: false,
		},

		// Invalid usernames - empty or invalid
		{
			name:      "empty username",
			input:     "",
			shouldErr: true,
			errMsg:    "username cannot be empty",
		},
		{
			name:      "username with spaces",
			input:     "john doe",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},

		// Invalid usernames - path traversal attempts
		{
			name:      "username with forward slash",
			input:     "john/doe",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with backslash",
			input:     "john\\doe",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with double dots",
			input:     "../john",
			shouldErr: true,
			errMsg:    "username contains path traversal sequences",
		},
		{
			name:      "username with double dots in middle",
			input:     "john..doe",
			shouldErr: true,
			errMsg:    "username contains path traversal sequences",
		},
		{
			name:      "path traversal attempt",
			input:     "../../etc/passwd",
			shouldErr: true,
			errMsg:    "username contains path traversal sequences",
		},

		// Invalid usernames - shell metacharacters
		{
			name:      "username with semicolon",
			input:     "john;rm",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with pipe",
			input:     "john|command",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with dollar sign",
			input:     "john$var",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with backtick",
			input:     "john`cmd`",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with ampersand",
			input:     "john&command",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with greater than",
			input:     "john>file",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with less than",
			input:     "john<file",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with parentheses",
			input:     "john(test)",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with braces",
			input:     "john{test}",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with brackets",
			input:     "john[test]",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with asterisk",
			input:     "john*",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with question mark",
			input:     "john?",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "username with tilde",
			input:     "john~",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},

		// Invalid usernames - special characters
		{
			name:      "username with at sign",
			input:     "john@test",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with hash",
			input:     "john#test",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with percent",
			input:     "john%test",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with exclamation",
			input:     "john!test",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with plus",
			input:     "john+test",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with equals",
			input:     "john=test",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with comma",
			input:     "john,doe",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with period",
			input:     "john.doe",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with colon",
			input:     "john:test",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},

		// Invalid usernames - only invalid characters
		{
			name:      "username with only special characters",
			input:     "!!!",
			shouldErr: true,
			errMsg:    "username contains no valid characters",
		},
		{
			name:      "username with only spaces",
			input:     "   ",
			shouldErr: true,
			errMsg:    "username contains no valid characters",
		},

		// Invalid usernames - control characters
		{
			name:      "username with null byte",
			input:     "john\x00doe",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with newline",
			input:     "john\ndoe",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with carriage return",
			input:     "john\rdoe",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with tab",
			input:     "john\tdoe",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with bell character",
			input:     "john\x07doe",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "username with invalid character",
			input:     "john\x1bdoe",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},

		// Potential attack vectors
		{
			name:      "SQL injection attempt",
			input:     "admin' OR '1'='1",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
		{
			name:      "Command injection attempt",
			input:     "user; rm -rf /",
			shouldErr: true,
			errMsg:    "username contains shell metacharacters",
		},
		{
			name:      "Path traversal with absolute path",
			input:     "/etc/passwd",
			shouldErr: true,
			errMsg:    "username contains invalid characters",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			result, err := Username(tc.input)
			if tc.shouldErr {
				req.Error(err, "expected error for input: %s", tc.input)
				if tc.errMsg != "" {
					req.Contains(err.Error(), tc.errMsg, "error message should contain: %s", tc.errMsg)
				}
			} else {
				req.NoError(err, "expected no error for input: %s", tc.input)
				req.Equal(tc.expected, result, "output should match expected")
			}
		})
	}
}

func TestSanity_SanitizePath(t *testing.T) {
	testCases := []struct {
		name      string
		input     string
		expected  string
		shouldErr bool
		errMsg    string
	}{
		// Valid paths that don't need sanitization
		{
			name:      "valid absolute path",
			input:     "/var/data/test",
			expected:  "/var/data/test",
			shouldErr: false,
		},
		{
			name:      "valid path with underscores",
			input:     "/var/data_dir/test_file",
			expected:  "/var/data_dir/test_file",
			shouldErr: false,
		},
		{
			name:      "valid path with dots in filename",
			input:     "/var/data.dir/test.file",
			expected:  "/var/data.dir/test.file",
			shouldErr: false,
		},
		{
			name:      "valid path with dashes",
			input:     "/var/my-data/test-file",
			expected:  "/var/my-data/test-file",
			shouldErr: false,
		},

		// Paths that should be rejected - empty or invalid
		{
			name:      "empty path",
			input:     "",
			shouldErr: true,
			errMsg:    "path cannot be empty",
		},
		{
			name:      "relative path",
			input:     "relative/path",
			shouldErr: true,
			errMsg:    "path must be absolute",
		},

		// Paths with shell metacharacters - should be rejected
		{
			name:      "path with semicolon",
			input:     "/var/data;rm",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with pipe",
			input:     "/var/data|command",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with dollar sign",
			input:     "/var/data$VAR",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with backtick",
			input:     "/var/data`cmd`",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with ampersand",
			input:     "/var/data&command",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with greater than",
			input:     "/var/data>file",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with less than",
			input:     "/var/data<file",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with parentheses",
			input:     "/var/data(test)",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with opening parenthesis",
			input:     "/var/data(test",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with closing parenthesis",
			input:     "/var/data)test",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with braces",
			input:     "/var/data{test}",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with opening brace",
			input:     "/var/data{test",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with closing brace",
			input:     "/var/data}test",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with brackets",
			input:     "/var/data[test]",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with opening bracket",
			input:     "/var/data[test",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with closing bracket",
			input:     "/var/data]test",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with asterisk",
			input:     "/var/data*",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with question mark",
			input:     "/var/data?test",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with tilde",
			input:     "/var/data~",
			shouldErr: true,
			errMsg:    "shell metacharacters",
		},
		{
			name:      "path with tilde expansion attempt",
			input:     "~/data/test",
			shouldErr: true,
			errMsg:    "path must be absolute",
		},

		// Paths with traversal patterns - should be rejected
		{
			name:      "path with parent directory traversal",
			input:     "/var/data/../etc",
			shouldErr: true,
			errMsg:    "'..' segments",
		},
		{
			name:      "path with multiple traversals",
			input:     "/var/data/../../etc/passwd",
			shouldErr: true,
			errMsg:    "'..' segments",
		},
		{
			name:      "path ending with double dot",
			input:     "/var/data/..",
			shouldErr: true,
			errMsg:    "'..' segments",
		},
		{
			name:      "path with double dot at end after slash",
			input:     "/var/data/../",
			shouldErr: true,
			errMsg:    "'..' segments",
		},

		// Paths with special characters - should be rejected
		{
			name:      "path with spaces",
			input:     "/var/data test/file",
			shouldErr: true,
			errMsg:    "invalid characters",
		},
		{
			name:      "path with at sign",
			input:     "/var/data@test",
			shouldErr: true,
			errMsg:    "invalid characters",
		},
		{
			name:      "path with hash",
			input:     "/var/data#test",
			shouldErr: true,
			errMsg:    "invalid characters",
		},
		{
			name:      "path with percent",
			input:     "/var/data%test",
			shouldErr: true,
			errMsg:    "invalid characters",
		},

		// Paths with redundant elements - should be CLEANED (sanitized)
		{
			name:      "path with double slashes",
			input:     "/var//data/test",
			expected:  "/var/data/test",
			shouldErr: false,
		},
		{
			name:      "path with multiple consecutive slashes",
			input:     "/var///data////test",
			expected:  "/var/data/test",
			shouldErr: false,
		},
		{
			name:      "path with trailing slash",
			input:     "/var/data/test/",
			expected:  "/var/data/test",
			shouldErr: false,
		},
		{
			name:      "path with dot directory",
			input:     "/var/./data/test",
			expected:  "/var/data/test",
			shouldErr: false,
		},
		{
			name:      "path with multiple dot directories",
			input:     "/var/././data/./test",
			expected:  "/var/data/test",
			shouldErr: false,
		},
		{
			name:      "path with mixed redundant elements",
			input:     "/var//./data///./test/",
			expected:  "/var/data/test",
			shouldErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			result, err := SanitizePath(tc.input)
			if tc.shouldErr {
				req.Error(err, "expected error for input: %s", tc.input)
				if tc.errMsg != "" {
					req.Contains(err.Error(), tc.errMsg, "error message should contain: %s", tc.errMsg)
				}
			} else {
				req.NoError(err, "expected no error for input: %s", tc.input)
				req.Equal(tc.expected, result, "output should match expected")
			}
		})
	}
}

func TestSanity_ValidateURL(t *testing.T) {
	testCases := []struct {
		name      string
		url       string
		shouldErr bool
		errMsg    string
	}{
		// Valid URLs
		{
			name:      "valid http URL",
			url:       "http://example.com/file.tar.gz",
			shouldErr: false,
		},
		{
			name:      "valid https URL",
			url:       "https://example.com/file.tar.gz",
			shouldErr: false,
		},
		{
			name:      "valid https URL with port",
			url:       "https://example.com:8443/file.tar.gz",
			shouldErr: false,
		},
		{
			name:      "valid https URL with query params",
			url:       "https://example.com/file.tar.gz?version=1.0",
			shouldErr: false,
		},
		{
			name:      "valid https URL with path",
			url:       "https://example.com/path/to/file.tar.gz",
			shouldErr: false,
		},

		// Invalid URLs - empty or malformed
		{
			name:      "empty URL",
			url:       "",
			shouldErr: true,
			errMsg:    "URL cannot be empty",
		},
		{
			name:      "malformed URL",
			url:       "ht!tp://example.com",
			shouldErr: true,
			errMsg:    "invalid URL",
		},

		// Invalid URLs - wrong scheme
		{
			name:      "ftp scheme",
			url:       "ftp://example.com/file.tar.gz",
			shouldErr: true,
			errMsg:    "URL scheme must be http or https",
		},
		{
			name:      "file scheme",
			url:       "file:///etc/passwd",
			shouldErr: true,
			errMsg:    "URL scheme must be http or https",
		},
		{
			name:      "javascript scheme",
			url:       "javascript:alert(1)",
			shouldErr: true,
			errMsg:    "URL scheme must be http or https",
		},
		{
			name:      "data scheme",
			url:       "data:text/html,<script>alert(1)</script>",
			shouldErr: true,
			errMsg:    "URL scheme must be http or https",
		},

		// Invalid URLs - missing host
		{
			name:      "missing host",
			url:       "https:///path/file.tar.gz",
			shouldErr: true,
			errMsg:    "URL must have a valid host",
		},
		{
			name:      "relative URL",
			url:       "/path/to/file.tar.gz",
			shouldErr: true,
			errMsg:    "URL scheme must be http or https",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			err := ValidateURL(tc.url)
			if tc.shouldErr {
				req.Error(err, "expected error for URL: %s", tc.url)
				if tc.errMsg != "" {
					req.Contains(err.Error(), tc.errMsg, "error message should contain: %s", tc.errMsg)
				}
			} else {
				req.NoError(err, "expected no error for URL: %s", tc.url)
			}
		})
	}
}

func TestSanity_ValidatePathWithinBase(t *testing.T) {
	testCases := []struct {
		name       string
		basePath   string
		targetPath string
		expected   string
		shouldErr  bool
		errMsg     string
	}{
		// Valid paths - absolute target paths
		{
			name:       "valid path within base",
			basePath:   "/opt/solo/weaver/tmp",
			targetPath: "/opt/solo/weaver/tmp/file.txt",
			expected:   "/opt/solo/weaver/tmp/file.txt",
			shouldErr:  false,
		},
		{
			name:       "valid nested path within base",
			basePath:   "/opt/solo/weaver/tmp",
			targetPath: "/opt/solo/weaver/tmp/subdir/file.txt",
			expected:   "/opt/solo/weaver/tmp/subdir/file.txt",
			shouldErr:  false,
		},
		{
			name:       "valid path with redundant separators",
			basePath:   "/opt/solo/weaver/tmp",
			targetPath: "/opt/solo/weaver/tmp//subdir///file.txt",
			expected:   "/opt/solo/weaver/tmp/subdir/file.txt",
			shouldErr:  false,
		},
		{
			name:       "valid path with dot segments (cleaned)",
			basePath:   "/opt/solo/weaver/tmp",
			targetPath: "/opt/solo/weaver/tmp/./subdir/./file.txt",
			expected:   "/opt/solo/weaver/tmp/subdir/file.txt",
			shouldErr:  false,
		},

		// Valid paths - simulating extract scenarios with filepath.Join
		{
			name:       "extract simple file",
			basePath:   "/tmp/extract",
			targetPath: "/tmp/extract/file.txt", // would be filepath.Join("/tmp/extract", "file.txt")
			expected:   "/tmp/extract/file.txt",
			shouldErr:  false,
		},
		{
			name:       "extract nested file",
			basePath:   "/tmp/extract",
			targetPath: "/tmp/extract/dir/subdir/file.txt", // would be filepath.Join("/tmp/extract", "dir/subdir/file.txt")
			expected:   "/tmp/extract/dir/subdir/file.txt",
			shouldErr:  false,
		},
		{
			name:       "extract with dots in filename",
			basePath:   "/tmp/extract",
			targetPath: "/tmp/extract/file.tar.gz",
			expected:   "/tmp/extract/file.tar.gz",
			shouldErr:  false,
		},

		// Invalid paths - empty
		{
			name:       "empty base path",
			basePath:   "",
			targetPath: "/opt/solo/weaver/tmp/file.txt",
			shouldErr:  true,
			errMsg:     "base path cannot be empty",
		},
		{
			name:       "empty target path",
			basePath:   "/opt/solo/weaver/tmp",
			targetPath: "",
			shouldErr:  true,
			errMsg:     "target path cannot be empty",
		},

		// Invalid paths - outside base
		{
			name:       "path outside base directory",
			basePath:   "/opt/solo/weaver/tmp",
			targetPath: "/opt/solo/weaver/bin/file.txt",
			shouldErr:  true,
			errMsg:     "is outside the allowed base directory",
		},
		{
			name:       "path traversal with double dots",
			basePath:   "/opt/solo/weaver/tmp",
			targetPath: "/opt/solo/weaver/tmp/../bin/file.txt",
			shouldErr:  true,
			errMsg:     "path cannot contain '..' segments",
		},
		{
			name:       "path traversal escaping base",
			basePath:   "/opt/solo/weaver/tmp",
			targetPath: "/opt/solo/weaver/tmp/../../etc/passwd",
			shouldErr:  true,
			errMsg:     "path cannot contain '..' segments",
		},
		{
			name:       "absolute path outside base",
			basePath:   "/opt/solo/weaver/tmp",
			targetPath: "/etc/passwd",
			shouldErr:  true,
			errMsg:     "is outside the allowed base directory",
		},
		{
			name:       "sibling directory attack",
			basePath:   "/opt/solo/weaver/tmp",
			targetPath: "/opt/solo/weaver/tmp-evil/file.txt",
			shouldErr:  true,
			errMsg:     "is outside the allowed base directory",
		},
		{
			name:       "root directory",
			basePath:   "/opt/solo/weaver/tmp",
			targetPath: "/",
			shouldErr:  true,
			errMsg:     "is outside the allowed base directory",
		},

		// Invalid paths - extract-style path traversal attacks
		{
			name:       "extract path traversal with double dots",
			basePath:   "/tmp/extract",
			targetPath: "/tmp/extract/../etc/passwd", // would be filepath.Join("/tmp/extract", "../etc/passwd")
			shouldErr:  true,
			errMsg:     "path cannot contain '..' segments",
		},
		{
			name:       "extract path traversal in middle",
			basePath:   "/tmp/extract",
			targetPath: "/tmp/extract/dir/../../../etc/passwd",
			shouldErr:  true,
			errMsg:     "path cannot contain '..' segments",
		},
		{
			name:       "extract path traversal with multiple double dots",
			basePath:   "/tmp/extract",
			targetPath: "/tmp/extract/../../../../../../etc/passwd",
			shouldErr:  true,
			errMsg:     "path cannot contain '..' segments",
		},
		{
			name:       "extract absolute path attempt",
			basePath:   "/tmp/extract",
			targetPath: "/etc/passwd", // malicious tar entry with absolute path
			shouldErr:  true,
			errMsg:     "is outside the allowed base directory",
		},
		{
			name:       "extract path to sibling directory",
			basePath:   "/var/data",
			targetPath: "/var/data-evil/file.txt",
			shouldErr:  true,
			errMsg:     "is outside the allowed base directory",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			result, err := ValidatePathWithinBase(tc.basePath, tc.targetPath)
			if tc.shouldErr {
				req.Error(err, "expected error for basePath=%s targetPath=%s", tc.basePath, tc.targetPath)
				if tc.errMsg != "" {
					req.Contains(err.Error(), tc.errMsg, "error message should contain: %s", tc.errMsg)
				}
			} else {
				req.NoError(err, "expected no error for basePath=%s targetPath=%s", tc.basePath, tc.targetPath)
				req.Equal(tc.expected, result, "output should match expected")
			}
		})
	}
}
