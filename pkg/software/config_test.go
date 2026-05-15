// SPDX-License-Identifier: Apache-2.0

package software

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Config_GetDefaultVersion(t *testing.T) {
	tests := []struct {
		name     string
		item     ArtifactMetadata
		expected string
		wantErr  bool
	}{
		{
			name: "default points to existing version",
			item: ArtifactMetadata{
				Name:    "test-software",
				Default: "1.0.0",
				Versions: map[Version]VersionDetails{
					"1.0.0": {},
				},
			},
			expected: "1.0.0",
			wantErr:  false,
		},
		{
			name: "default selects one entry among many - not the highest semver",
			item: ArtifactMetadata{
				Name:    "test-software",
				Default: "1.1.0",
				Versions: map[Version]VersionDetails{
					"1.0.0":  {},
					"1.1.0":  {},
					"1.0.1":  {},
					"2.0.0":  {},
					"1.10.0": {},
				},
			},
			expected: "1.1.0",
			wantErr:  false,
		},
		{
			name: "default selects the highest semver when explicitly set",
			item: ArtifactMetadata{
				Name:    "test-software",
				Default: "1.34.0",
				Versions: map[Version]VersionDetails{
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
			name: "missing default returns error",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {},
				},
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "default pointing to unknown version returns error",
			item: ArtifactMetadata{
				Name:    "test-software",
				Default: "9.9.9",
				Versions: map[Version]VersionDetails{
					"1.0.0": {},
				},
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "no versions and no default returns error",
			item: ArtifactMetadata{
				Name:     "test-software",
				Versions: map[Version]VersionDetails{},
			},
			expected: "",
			wantErr:  true,
		},
		{
			name: "default with non-semver version string is returned as-is",
			item: ArtifactMetadata{
				Name:    "test-software",
				Default: "stable",
				Versions: map[Version]VersionDetails{
					"stable": {},
					"1.0.0":  {},
				},
			},
			expected: "stable",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.item.GetDefaultVersion()

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

func Test_Config_VersionSelection(t *testing.T) {

	//
	// Given
	//

	// Create a mock software item with multiple versions to test versionToBeInstalled selection
	mockSoftwareItem := &ArtifactMetadata{
		Name:    "cri-o",
		Default: "1.27.0",
		Versions: map[Version]VersionDetails{
			"1.25.0": {
				Binaries: []BinaryDetail{
					{
						Name: "crio",
						PlatformChecksum: PlatformChecksum{
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
					},
				},
			},
			"1.27.0": {
				Binaries: []BinaryDetail{
					{
						Name: "crio",
						PlatformChecksum: PlatformChecksum{
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
					},
				},
			},
			"1.26.1": {
				Binaries: []BinaryDetail{
					{
						Name: "crio",
						PlatformChecksum: PlatformChecksum{
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
				},
			},
		},
	}

	// Test that GetDefaultVersion returns the declared default
	defaultVersion, err := mockSoftwareItem.GetDefaultVersion()
	require.NoError(t, err, "GetDefaultVersion should not return an error")
	assert.Equal(t, "1.27.0", defaultVersion, "GetDefaultVersion should return the declared default 1.27.0")

	//
	// When
	//

	// Create a mock crio installer using the proper constructor pattern
	// We'll create a baseInstaller manually for testing since we need to inject mock data
	testSoftware := mockSoftwareItem.withPlatform("darwin", "arm64")

	// Create the baseInstaller with mock data for testing
	baseInstaller := &baseInstaller{
		software:             testSoftware,
		versionToBeInstalled: defaultVersion,
	}

	//
	// Then
	//

	// Verify that the versionToBeInstalled is valid (exists in the configuration and has checksums available)
	// Get the first binary's checksum for darwin/arm64
	versionInfo := baseInstaller.software.Versions[Version(baseInstaller.Version())]
	require.NotEmpty(t, versionInfo.Binaries, "Should have binaries defined")

	binary := versionInfo.Binaries[0]
	osInfo, exists := binary.PlatformChecksum["darwin"]
	require.True(t, exists, "Should have darwin platform")

	checksum, exists := osInfo["arm64"]
	require.True(t, exists, "Should have arm64 architecture")
	require.NotEmpty(t, checksum, "Checksum for the selected versionToBeInstalled should not be empty")
	assert.Equal(t, "yzx567", checksum.Value, "Should return the correct checksum for darwin/arm64")

	// Verify that the installer uses the declared default
	assert.Equal(t, "1.27.0", baseInstaller.Version(), "Installer should use the declared default (1.27.0)")

	// Additional verification: default selection is explicit and not derived from semver ordering.
	// 1.9.0 is alphabetically later than 1.10.0 but semantically earlier; we set Default to the
	// older 1.9.0 to confirm the explicit default wins regardless of either ordering.
	mockItemWithExplicitDefault := &ArtifactMetadata{
		Name:    "cri-o",
		Default: "1.9.0",
		Versions: map[Version]VersionDetails{
			"1.9.0": {
				Binaries: []BinaryDetail{
					{
						Name: "crio",
						PlatformChecksum: PlatformChecksum{
							"darwin": {
								"arm64": {Algorithm: "sha256", Value: "test1"},
							},
						},
					},
				},
			},
			"1.10.0": {
				Binaries: []BinaryDetail{
					{
						Name: "crio",
						PlatformChecksum: PlatformChecksum{
							"darwin": {
								"arm64": {Algorithm: "sha256", Value: "test2"},
							},
						},
					},
				},
			},
		},
	}

	explicitDefault, err := mockItemWithExplicitDefault.GetDefaultVersion()
	require.NoError(t, err)
	assert.Equal(t, "1.9.0", explicitDefault, "Should return the declared default 1.9.0 even though 1.10.0 is the higher semver")
}

func Test_Config_GetChecksum(t *testing.T) {
	tests := []struct {
		name        string
		item        ArtifactMetadata
		platform    struct{ os, arch string }
		version     string
		expectedErr bool
		expected    Checksum
	}{
		{
			name: "valid versionToBeInstalled and platform - darwin/arm64",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Binaries: []BinaryDetail{
							{
								Name: "test-binary",
								PlatformChecksum: PlatformChecksum{
									"darwin": {
										"arm64": {
											Algorithm: "sha256",
											Value:     "abc123",
										},
									},
								},
							},
						},
					},
				},
			},
			platform:    struct{ os, arch string }{"darwin", "arm64"},
			version:     "1.0.0",
			expectedErr: false,
			expected: Checksum{
				Algorithm: "sha256",
				Value:     "abc123",
			},
		},
		{
			name: "valid versionToBeInstalled and platform - linux/amd64",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Binaries: []BinaryDetail{
							{
								Name: "test-binary",
								PlatformChecksum: PlatformChecksum{
									"linux": {
										"amd64": {
											Algorithm: "sha256",
											Value:     "def456",
										},
									},
								},
							},
						},
					},
				},
			},
			platform:    struct{ os, arch string }{"linux", "amd64"},
			version:     "1.0.0",
			expectedErr: false,
			expected: Checksum{
				Algorithm: "sha256",
				Value:     "def456",
			},
		},
		{
			name: "versionToBeInstalled not found",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Binaries: []BinaryDetail{
							{
								Name: "test-binary",
								PlatformChecksum: PlatformChecksum{
									"darwin": {
										"arm64": {
											Algorithm: "sha256",
											Value:     "abc123",
										},
									},
								},
							},
						},
					},
				},
			},
			platform:    struct{ os, arch string }{"darwin", "arm64"},
			version:     "2.0.0",
			expectedErr: true,
		},
		{
			name: "OS not found",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Binaries: []BinaryDetail{
							{
								Name: "test-binary",
								PlatformChecksum: PlatformChecksum{
									"linux": {
										"amd64": {
											Algorithm: "sha256",
											Value:     "abc123",
										},
									},
								},
							},
						},
					},
				},
			},
			platform:    struct{ os, arch string }{"windows", "amd64"},
			version:     "1.0.0",
			expectedErr: true,
		},
		{
			name: "architecture not found",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Binaries: []BinaryDetail{
							{
								Name: "test-binary",
								PlatformChecksum: PlatformChecksum{
									"linux": {
										"amd64": {
											Algorithm: "sha256",
											Value:     "abc123",
										},
									},
								},
							},
						},
					},
				},
			},
			platform:    struct{ os, arch string }{"linux", "arm64"},
			version:     "1.0.0",
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test accessing checksums directly from binary PlatformChecksum
			versionInfo, exists := tt.item.Versions[Version(tt.version)]

			if !exists {
				if tt.expectedErr {
					// Expected error for version not found
					return
				}
				t.Fatalf("Version %s not found", tt.version)
			}

			if len(versionInfo.Binaries) == 0 {
				if tt.expectedErr {
					return
				}
				t.Fatal("No binaries found")
			}

			binary := versionInfo.Binaries[0]
			osInfo, exists := binary.PlatformChecksum[tt.platform.os]

			if !exists {
				if tt.expectedErr {
					// Expected error for OS not found
					return
				}
				t.Fatalf("OS %s not found", tt.platform.os)
			}

			result, exists := osInfo[tt.platform.arch]

			if !exists {
				if tt.expectedErr {
					// Expected error for arch not found
					return
				}
				t.Fatalf("Arch %s not found", tt.platform.arch)
			}

			if tt.expectedErr {
				t.Fatal("Expected error but got success")
			}

			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_Config_GetDownloadURL(t *testing.T) {
	tests := []struct {
		name        string
		item        ArtifactMetadata
		platform    struct{ os, arch string }
		version     string
		expectedErr bool
		expectedURL string
	}{
		{
			name: "valid template with version only - archive",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Archives: []ArchiveDetail{
							{
								Name: "software.tar.gz",
								URL:  "https://example.com/{{.VERSION}}/software.tar.gz",
							},
						},
					},
				},
			},
			platform:    struct{ os, arch string }{"linux", "amd64"},
			version:     "1.0.0",
			expectedErr: false,
			expectedURL: "https://example.com/1.0.0/software.tar.gz",
		},
		{
			name: "template with all variables - linux/amd64",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"2.1.0": {
						Binaries: []BinaryDetail{
							{
								Name: "software",
								URL:  "https://example.com/{{.VERSION}}/{{.OS}}/{{.ARCH}}/software.tar.gz",
							},
						},
					},
				},
			},
			platform:    struct{ os, arch string }{"linux", "amd64"},
			version:     "2.1.0",
			expectedErr: false,
			expectedURL: "https://example.com/2.1.0/linux/amd64/software.tar.gz",
		},
		{
			name: "template with all variables - darwin/arm64",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"2.1.0": {
						Binaries: []BinaryDetail{
							{
								Name: "software",
								URL:  "https://example.com/{{.VERSION}}/{{.OS}}/{{.ARCH}}/software.tar.gz",
							},
						},
					},
				},
			},
			platform:    struct{ os, arch string }{"darwin", "arm64"},
			version:     "2.1.0",
			expectedErr: false,
			expectedURL: "https://example.com/2.1.0/darwin/arm64/software.tar.gz",
		},
		{
			name: "template with all variables - windows/amd64",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"2.1.0": {
						Binaries: []BinaryDetail{
							{
								Name: "software",
								URL:  "https://example.com/{{.VERSION}}/{{.OS}}/{{.ARCH}}/software.tar.gz",
							},
						},
					},
				},
			},
			platform:    struct{ os, arch string }{"windows", "amd64"},
			version:     "2.1.0",
			expectedErr: false,
			expectedURL: "https://example.com/2.1.0/windows/amd64/software.tar.gz",
		},
		{
			name: "invalid template syntax",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Binaries: []BinaryDetail{
							{
								Name: "software",
								URL:  "https://example.com/{{.VERSION/software.tar.gz",
							},
						},
					},
				},
			},
			platform:    struct{ os, arch string }{"linux", "amd64"},
			version:     "1.0.0",
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use withPlatform to set a specific platform for testing
			testItem := tt.item.withPlatform(tt.platform.os, tt.platform.arch)

			// Get version info
			versionInfo, exists := testItem.Versions[Version(tt.version)]
			require.True(t, exists, "Version should exist")

			// Get the URL template from either archives or binaries
			var urlTemplate string
			if len(versionInfo.Archives) > 0 {
				urlTemplate = versionInfo.Archives[0].URL
			} else if len(versionInfo.Binaries) > 0 {
				urlTemplate = versionInfo.Binaries[0].URL
			} else {
				t.Fatal("No archives or binaries found")
			}

			// Execute template
			platform := testItem.getPlatform()
			data := TemplateData{
				VERSION: tt.version,
				OS:      platform.os,
				ARCH:    platform.arch,
			}

			result, err := executeTemplate(urlTemplate, data)

			if tt.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedURL, result)
			}
		})
	}
}

func Test_Config_GetFilename(t *testing.T) {
	tests := []struct {
		name             string
		item             ArtifactMetadata
		platform         struct{ os, arch string }
		version          string
		expectedErr      bool
		expectedFilename string
	}{
		{
			name: "valid template with version only",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Binaries: []BinaryDetail{
							{
								Name: "software-{{.VERSION}}.tar.gz",
							},
						},
					},
				},
			},
			platform:         struct{ os, arch string }{"linux", "amd64"},
			version:          "1.0.0",
			expectedErr:      false,
			expectedFilename: "software-1.0.0.tar.gz",
		},
		{
			name: "template with all variables - linux/amd64",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"2.1.0": {
						Binaries: []BinaryDetail{
							{
								Name: "software-{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz",
							},
						},
					},
				},
			},
			platform:         struct{ os, arch string }{"linux", "amd64"},
			version:          "2.1.0",
			expectedErr:      false,
			expectedFilename: "software-2.1.0-linux-amd64.tar.gz",
		},
		{
			name: "template with all variables - darwin/arm64",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"2.1.0": {
						Binaries: []BinaryDetail{
							{
								Name: "software-{{.VERSION}}-{{.OS}}-{{.ARCH}}.tar.gz",
							},
						},
					},
				},
			},
			platform:         struct{ os, arch string }{"darwin", "arm64"},
			version:          "2.1.0",
			expectedErr:      false,
			expectedFilename: "software-2.1.0-darwin-arm64.tar.gz",
		},
		{
			name: "template with all variables - windows/amd64",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"2.1.0": {
						Binaries: []BinaryDetail{
							{
								Name: "software-{{.VERSION}}-{{.OS}}-{{.ARCH}}.exe",
							},
						},
					},
				},
			},
			platform:         struct{ os, arch string }{"windows", "amd64"},
			version:          "2.1.0",
			expectedErr:      false,
			expectedFilename: "software-2.1.0-windows-amd64.exe",
		},
		{
			name: "invalid template syntax",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Binaries: []BinaryDetail{
							{
								Name: "software-{{.VERSION.tar.gz",
							},
						},
					},
				},
			},
			platform:    struct{ os, arch string }{"linux", "amd64"},
			version:     "1.0.0",
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use withPlatform to set a specific platform for testing
			testItem := tt.item.withPlatform(tt.platform.os, tt.platform.arch)

			// Get version info
			versionInfo, exists := testItem.Versions[Version(tt.version)]
			if !exists {
				if tt.expectedErr {
					return
				}
				t.Fatalf("Version %s not found", tt.version)
			}

			if len(versionInfo.Binaries) == 0 {
				if tt.expectedErr {
					return
				}
				t.Fatal("No binaries found")
			}

			// Resolve the binary name using template
			platform := testItem.getPlatform()
			data := TemplateData{
				VERSION: tt.version,
				OS:      platform.os,
				ARCH:    platform.arch,
			}

			result, err := executeTemplate(versionInfo.Binaries[0].Name, data)

			if tt.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedFilename, result)
			}
		})
	}
}

func Test_Config_GetConfigs(t *testing.T) {
	tests := []struct {
		name            string
		item            ArtifactMetadata
		version         string
		expectedErr     bool
		expectedConfigs []ConfigDetail
	}{
		{
			name: "versionToBeInstalled with configs",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Configs: []ConfigDetail{
							{
								Name:      "config1",
								URL:       "https://example.com/config1.yaml",
								Algorithm: "sha256",
								Value:     "abc123",
							},
							{
								Name:      "config2",
								URL:       "https://example.com/config2.yaml",
								Algorithm: "sha256",
								Value:     "def456",
							},
						},
					},
				},
			},
			version:     "1.0.0",
			expectedErr: false,
			expectedConfigs: []ConfigDetail{
				{
					Name:      "config1",
					URL:       "https://example.com/config1.yaml",
					Algorithm: "sha256",
					Value:     "abc123",
				},
				{
					Name:      "config2",
					URL:       "https://example.com/config2.yaml",
					Algorithm: "sha256",
					Value:     "def456",
				},
			},
		},
		{
			name: "versionToBeInstalled without configs",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Configs: []ConfigDetail{},
					},
				},
			},
			version:         "1.0.0",
			expectedErr:     false,
			expectedConfigs: []ConfigDetail{},
		},
		{
			name: "versionToBeInstalled not found",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Configs: []ConfigDetail{},
					},
				},
			},
			version:     "2.0.0",
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versionInfo, exists := tt.item.Versions[Version(tt.version)]

			if tt.expectedErr {
				assert.False(t, exists)
			} else {
				assert.True(t, exists)
				result := versionInfo.GetConfigs()
				assert.Equal(t, tt.expectedConfigs, result)
			}
		})
	}
}

func Test_Config_GetConfigByName(t *testing.T) {
	tests := []struct {
		name           string
		item           ArtifactMetadata
		version        string
		configName     string
		expectedErr    bool
		expectedConfig *ConfigDetail
	}{
		{
			name: "config found",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Configs: []ConfigDetail{
							{
								Name:      "config1",
								URL:       "https://example.com/config1.yaml",
								Algorithm: "sha256",
								Value:     "abc123",
							},
							{
								Name:      "config2",
								URL:       "https://example.com/config2.yaml",
								Algorithm: "sha256",
								Value:     "def456",
							},
						},
					},
				},
			},
			version:     "1.0.0",
			configName:  "config1",
			expectedErr: false,
			expectedConfig: &ConfigDetail{
				Name:      "config1",
				URL:       "https://example.com/config1.yaml",
				Algorithm: "sha256",
				Value:     "abc123",
			},
		},
		{
			name: "config not found",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Configs: []ConfigDetail{
							{
								Name:      "config1",
								URL:       "https://example.com/config1.yaml",
								Algorithm: "sha256",
								Value:     "abc123",
							},
						},
					},
				},
			},
			version:     "1.0.0",
			configName:  "config2",
			expectedErr: true,
		},
		{
			name: "versionToBeInstalled not found",
			item: ArtifactMetadata{
				Name: "test-software",
				Versions: map[Version]VersionDetails{
					"1.0.0": {
						Configs: []ConfigDetail{},
					},
				},
			},
			version:     "2.0.0",
			configName:  "config1",
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versionDetail := tt.item.Versions[Version(tt.version)]
			result, err := versionDetail.GetConfigByName(tt.configName)

			if tt.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedConfig, result)
			}
		})
	}
}

func Test_Config_SoftwareCollection_GetSoftwareByName(t *testing.T) {
	collection := &ArtifactCollection{
		Artifact: []ArtifactMetadata{
			{
				Name: "software1",
			},
			{
				Name: "software2",
			},
		},
	}

	tests := []struct {
		name         string
		softwareName string
		expectedErr  bool
		expectedName string
	}{
		{
			name:         "software found",
			softwareName: "software1",
			expectedErr:  false,
			expectedName: "software1",
		},
		{
			name:         "software not found",
			softwareName: "nonexistent",
			expectedErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := collection.GetArtifactByName(tt.softwareName)

			if tt.expectedErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedName, result.Name)
			}
		})
	}
}

func Test_Config_TemplateExecution(t *testing.T) {
	tests := []struct {
		name         string
		templateStr  string
		data         TemplateData
		expectedErr  bool
		expectedText string
	}{
		{
			name:        "simple versionToBeInstalled template",
			templateStr: "versionToBeInstalled-{{.VERSION}}",
			data: TemplateData{
				VERSION: "1.0.0",
				OS:      "linux",
				ARCH:    "amd64",
			},
			expectedErr:  false,
			expectedText: "versionToBeInstalled-1.0.0",
		},
		{
			name:        "all variables template",
			templateStr: "{{.VERSION}}-{{.OS}}-{{.ARCH}}",
			data: TemplateData{
				VERSION: "2.1.0",
				OS:      "darwin",
				ARCH:    "arm64",
			},
			expectedErr:  false,
			expectedText: "2.1.0-darwin-arm64",
		},
		{
			name:        "complex template",
			templateStr: "https://releases.example.com/{{.VERSION}}/{{.OS}}/{{.ARCH}}/software.tar.gz",
			data: TemplateData{
				VERSION: "1.33.4",
				OS:      "linux",
				ARCH:    "amd64",
			},
			expectedErr:  false,
			expectedText: "https://releases.example.com/1.33.4/linux/amd64/software.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test template execution directly
			result, err := executeTemplate(tt.templateStr, tt.data)

			if tt.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedText, result)
			}
		})
	}
}
