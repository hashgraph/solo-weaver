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
