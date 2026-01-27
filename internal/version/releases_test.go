// SPDX-License-Identifier: Apache-2.0

package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsReleaseBuild(t *testing.T) {
	testCases := []struct {
		name      string
		buildMode string
		expected  bool
	}{
		// Release build cases
		{
			name:      "release build - exact match",
			buildMode: "release",
			expected:  true,
		},
		{
			name:      "release build - leading whitespace",
			buildMode: "  release",
			expected:  true,
		},
		{
			name:      "release build - trailing whitespace",
			buildMode: "release  ",
			expected:  true,
		},
		{
			name:      "release build - leading and trailing whitespace",
			buildMode: "  release  ",
			expected:  true,
		},
		{
			name:      "release build - with newline",
			buildMode: "release\n",
			expected:  true,
		},
		{
			name:      "release build - with tab",
			buildMode: "\trelease\t",
			expected:  true,
		},

		// Dev build cases (non-release)
		{
			name:      "dev build - empty string",
			buildMode: "",
			expected:  false,
		},
		{
			name:      "dev build - whitespace only",
			buildMode: "   ",
			expected:  false,
		},
		{
			name:      "dev build - dev string",
			buildMode: "dev",
			expected:  false,
		},
		{
			name:      "dev build - uppercase RELEASE",
			buildMode: "RELEASE",
			expected:  false,
		},
		{
			name:      "dev build - mixed case Release",
			buildMode: "Release",
			expected:  false,
		},
		{
			name:      "dev build - partial match",
			buildMode: "rel",
			expected:  false,
		},
		{
			name:      "dev build - extra characters",
			buildMode: "release-candidate",
			expected:  false,
		},
		{
			name:      "dev build - prefixed release",
			buildMode: "pre-release",
			expected:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Save original value
			original := buildMode
			// Set test value
			buildMode = tc.buildMode
			// Restore after test
			defer func() { buildMode = original }()

			result := IsReleaseBuild()
			require.Equal(t, tc.expected, result, "buildMode: %q", tc.buildMode)
		})
	}
}

func TestBuildMode(t *testing.T) {
	testCases := []struct {
		name         string
		buildModeVar string
		expected     string
	}{
		// Release build cases
		{
			name:         "release build returns 'release'",
			buildModeVar: "release",
			expected:     "release",
		},
		{
			name:         "release build with whitespace returns 'release'",
			buildModeVar: "  release  ",
			expected:     "release",
		},

		// Dev build cases
		{
			name:         "empty buildMode returns 'dev'",
			buildModeVar: "",
			expected:     "dev",
		},
		{
			name:         "whitespace only returns 'dev'",
			buildModeVar: "   ",
			expected:     "dev",
		},
		{
			name:         "any other value returns 'dev'",
			buildModeVar: "debug",
			expected:     "dev",
		},
		{
			name:         "uppercase RELEASE returns 'dev' (case sensitive)",
			buildModeVar: "RELEASE",
			expected:     "dev",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Save original value
			original := buildMode
			// Set test value
			buildMode = tc.buildModeVar
			// Restore after test
			defer func() { buildMode = original }()

			result := BuildMode()
			require.Equal(t, tc.expected, result, "buildModeVar: %q", tc.buildModeVar)
		})
	}
}

func TestCommit(t *testing.T) {
	// Commit should return the embedded commit value
	// It's embedded at build time, so we just verify it returns a non-panicking value
	result := Commit()
	require.NotPanics(t, func() {
		_ = Commit()
	})
	// The commit variable is embedded, so just verify it's a string (may be empty in tests)
	require.IsType(t, "", result)
}

func TestNumber(t *testing.T) {
	// Number should return the embedded version value
	// It's embedded at build time, so we just verify it returns a non-panicking value
	result := Number()
	require.NotPanics(t, func() {
		_ = Number()
	})
	// The number variable is embedded, so just verify it's a string (may be empty in tests)
	require.IsType(t, "", result)
}
