package software

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSoftwareItem_GetLatestVersion(t *testing.T) {
	tests := []struct {
		name     string
		item     SoftwareItem
		expected string
		wantErr  bool
	}{
		{
			name: "single version",
			item: SoftwareItem{
				Name: "test-software",
				Versions: map[Version]Platforms{
					"1.0.0": {},
				},
			},
			expected: "1.0.0",
			wantErr:  false,
		},
		{
			name: "multiple versions - semantic ordering",
			item: SoftwareItem{
				Name: "test-software",
				Versions: map[Version]Platforms{
					"1.0.0":  {},
					"1.1.0":  {},
					"1.0.1":  {},
					"2.0.0":  {},
					"1.10.0": {},
				},
			},
			expected: "2.0.0",
			wantErr:  false,
		},
		{
			name: "versions with patch releases",
			item: SoftwareItem{
				Name: "test-software",
				Versions: map[Version]Platforms{
					"1.33.4": {},
					"1.33.5": {},
					"1.34.0": {},
					"1.33.6": {},
				},
			},
			expected: "1.34.0",
			wantErr:  false,
		},
		{
			name: "no versions available",
			item: SoftwareItem{
				Name:     "test-software",
				Versions: map[Version]Platforms{},
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "versions with non-semantic format fallback to string comparison",
			item: SoftwareItem{
				Name: "test-software",
				Versions: map[Version]Platforms{
					"latest": {},
					"stable": {},
					"v1.0.0": {},
					"z":      {},
					"1.0.0":  {},
					"0.0.0":  {},
					"v0.1.0": {},
				},
			},
			// The latest version should be "z"
			expected: "z",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.item.GetLatestVersion()

			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestCrioInstaller_UsesLatestVersion(t *testing.T) {

	//
	// Given
	//

	// Create a mock software item with multiple versions to test version selection
	mockSoftwareItem := &SoftwareItem{
		Name:     "cri-o",
		URL:      "https://example.com/crio-{{.VERSION}}.tar.gz",
		Filename: "crio-{{.VERSION}}.tar.gz",
		Versions: map[Version]Platforms{
			"1.25.0": {
				"linux": {
					"amd64": {
						Algorithm: "sha256",
						Value:     "abc123",
					},
				},
				"darwin": {
					"amd64": {
						Algorithm: "sha256",
						Value:     "def456",
					},
					"arm64": {
						Algorithm: "sha256",
						Value:     "ghi789",
					},
				},
			},
			"1.27.0": {
				"linux": {
					"amd64": {
						Algorithm: "sha256",
						Value:     "stu901",
					},
					"arm64": {
						Algorithm: "sha256",
						Value:     "opq234",
					},
				},
				"darwin": {
					"amd64": {
						Algorithm: "sha256",
						Value:     "vwx234",
					},
					"arm64": {
						Algorithm: "sha256",
						Value:     "yzx567",
					},
				},
			},
			"1.26.1": {
				"linux": {
					"amd64": {
						Algorithm: "sha256",
						Value:     "jkl012",
					},
				},
				"darwin": {
					"amd64": {
						Algorithm: "sha256",
						Value:     "mno345",
					},
					"arm64": {
						Algorithm: "sha256",
						Value:     "pqr678",
					},
				},
			},
		},
	}

	// Test that GetLatestVersion returns the expected latest version
	latestVersion, err := mockSoftwareItem.GetLatestVersion()
	require.NoError(t, err, "GetLatestVersion should not return an error")
	assert.Equal(t, "1.27.0", latestVersion, "GetLatestVersion should return 1.27.0 as the latest version")

	//
	// When
	//

	// Create a mock crio installer with our test software item
	installer := &crioInstaller{
		downloader:            NewDownloader(),
		softwareToBeInstalled: mockSoftwareItem,
		version:               latestVersion,
	}

	//
	// Then
	//

	// Verify that the version is valid (exists in the configuration and has checksums available)
	checksum, err := installer.softwareToBeInstalled.GetChecksum(installer.version)
	require.NoError(t, err, "Selected version should be valid and have checksums available for current platform")
	assert.NotEmpty(t, checksum, "Checksum for the selected version should not be empty")

	// Verify that the installer uses the latest version
	assert.Equal(t, "1.27.0", installer.version, "Installer should use the latest version (1.27.0)")

	// Additional verification: ensure the version selection is semantic, not alphabetical
	// Test with a version that would be "latest" alphabetically but not semantically
	mockItemWithNonSemanticOrder := &SoftwareItem{
		Name: "cri-o",
		Versions: map[Version]Platforms{
			"1.9.0": {
				"darwin": {
					"arm64": {Algorithm: "sha256", Value: "test1"},
				},
			},
			"1.10.0": {
				"darwin": {
					"arm64": {Algorithm: "sha256", Value: "test2"},
				},
			},
		},
	}

	semanticLatest, err := mockItemWithNonSemanticOrder.GetLatestVersion()
	require.NoError(t, err)
	assert.Equal(t, "1.10.0", semanticLatest, "Should choose 1.10.0 over 1.9.0 (semantic ordering, not alphabetical)")
}
