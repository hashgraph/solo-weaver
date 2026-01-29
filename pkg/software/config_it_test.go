// SPDX-License-Identifier: Apache-2.0

package software

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Test_Config_LoadArtifactYAML is an integration test that verifies the artifact.yaml file
// can be loaded and parsed correctly into the ArtifactCollection structure
func Test_Config_LoadArtifactYAML(t *testing.T) {
	// Load the actual artifact.yaml file
	config, err := LoadArtifactConfig()
	require.NoError(t, err, "Should be able to load artifact.yaml")
	require.NotNil(t, config, "Config should not be nil")

	// Verify that we have artifacts defined
	require.NotEmpty(t, config.Artifact, "Should have at least one artifact defined")

	// Test each artifact in the configuration
	for _, artifact := range config.Artifact {
		t.Run("Artifact_"+artifact.Name, func(t *testing.T) {
			// Verify basic fields
			require.NotEmpty(t, artifact.Name, "Artifact name should not be empty")
			require.NotEmpty(t, artifact.Versions, "Artifact should have at least one version")

			// Verify each version
			for version, details := range artifact.Versions {
				t.Run("Version_"+string(version), func(t *testing.T) {
					// At least one of archives or binaries should be defined
					hasArchives := len(details.Archives) > 0
					hasBinaries := len(details.Binaries) > 0
					require.True(t, hasArchives || hasBinaries,
						"Version %s should have either archives or binaries defined", version)

					// Validate archives
					for i, archive := range details.Archives {
						t.Run("Archive_"+archive.Name, func(t *testing.T) {
							require.NotEmpty(t, archive.Name, "Archive %d name should not be empty", i)
							require.NotEmpty(t, archive.URL, "Archive %d URL should not be empty", i)

							// Verify platform checksums exist
							require.NotEmpty(t, archive.PlatformChecksum,
								"Archive %s should have platform checksums", archive.Name)

							// Validate each platform checksum
							for os, archMap := range archive.PlatformChecksum {
								require.NotEmpty(t, archMap,
									"Archive %s should have checksums for OS %s", archive.Name, os)

								for arch, checksum := range archMap {
									require.NotEmpty(t, checksum.Algorithm,
										"Archive %s checksum algorithm should not be empty for %s/%s",
										archive.Name, os, arch)
									require.NotEmpty(t, checksum.Value,
										"Archive %s checksum value should not be empty for %s/%s",
										archive.Name, os, arch)
								}
							}
						})
					}

					// Validate binaries
					for i, binary := range details.Binaries {
						t.Run("Binary_"+binary.Name, func(t *testing.T) {
							require.NotEmpty(t, binary.Name, "Binary %d name should not be empty", i)

							// Binary should have either a URL or an Archive reference
							hasURL := binary.URL != ""
							hasArchive := binary.Archive != ""
							require.True(t, hasURL || hasArchive,
								"Binary %s should have either URL or Archive reference", binary.Name)

							// Verify platform checksums exist
							require.NotEmpty(t, binary.PlatformChecksum,
								"Binary %s should have platform checksums", binary.Name)

							// Validate each platform checksum
							for os, archMap := range binary.PlatformChecksum {
								require.NotEmpty(t, archMap,
									"Binary %s should have checksums for OS %s", binary.Name, os)

								for arch, checksum := range archMap {
									require.NotEmpty(t, checksum.Algorithm,
										"Binary %s checksum algorithm should not be empty for %s/%s",
										binary.Name, os, arch)
									require.NotEmpty(t, checksum.Value,
										"Binary %s checksum value should not be empty for %s/%s",
										binary.Name, os, arch)
								}
							}
						})
					}

					// Validate configs (if present)
					for i, config := range details.Configs {
						require.NotEmpty(t, config.Name, "Config %d name should not be empty", i)
						// Binary should have either a URL or an Archive reference
						hasURL := config.URL != ""
						hasArchive := config.Archive != ""
						require.True(t, hasURL || hasArchive,
							"Config %s should have either URL or Archive reference", config.Name)
						require.NotEmpty(t, config.Algorithm, "Config %d algorithm should not be empty", i)
						require.NotEmpty(t, config.Value, "Config %d checksum value should not be empty", i)
					}
				})
			}
		})
	}
}

// Test_Config_GetSoftwareByName_Integration tests retrieving actual software from artifact.yaml
func Test_Config_GetSoftwareByName_Integration(t *testing.T) {
	config, err := LoadArtifactConfig()
	require.NoError(t, err)

	// Test that we can find cri-o (which should be in the artifact.yaml based on the sample)
	crioSoftware, err := config.GetArtifactByName("cri-o")
	if err == nil {
		// If cri-o exists, validate it
		require.NotNil(t, crioSoftware)
		require.Equal(t, "cri-o", crioSoftware.Name)
		require.NotEmpty(t, crioSoftware.Versions, "cri-o should have versions defined")
	}

	// Test non-existent software
	_, err = config.GetArtifactByName("non-existent-software")
	require.Error(t, err, "Should return error for non-existent software")
}

// Test_Config_GetLatestVersion_Integration tests getting the latest version from actual artifacts
func Test_Config_GetLatestVersion_Integration(t *testing.T) {
	config, err := LoadArtifactConfig()
	require.NoError(t, err)

	// Test each artifact can return a latest version
	for _, artifact := range config.Artifact {
		t.Run(artifact.Name, func(t *testing.T) {
			latest, err := artifact.GetLatestVersion()
			require.NoError(t, err, "Should be able to get latest version for %s", artifact.Name)
			require.NotEmpty(t, latest, "Latest version should not be empty for %s", artifact.Name)

			// Verify the latest version exists in the versions map
			_, exists := artifact.Versions[Version(latest)]
			require.True(t, exists, "Latest version %s should exist in versions map for %s",
				latest, artifact.Name)
		})
	}
}

// Test_Config_GetChecksum_Integration tests getting checksums from actual artifacts
func Test_Config_GetChecksum_Integration(t *testing.T) {
	config, err := LoadArtifactConfig()
	require.NoError(t, err)

	// Test a few artifacts if they exist
	for _, artifact := range config.Artifact {
		t.Run(artifact.Name, func(t *testing.T) {
			// Get the latest version
			latest, err := artifact.GetLatestVersion()
			if err != nil {
				t.Skipf("Skipping %s: cannot get latest version", artifact.Name)
				return
			}

			versionInfo, exists := artifact.Versions[Version(latest)]
			if !exists {
				t.Skipf("Skipping %s: version %s not found", artifact.Name, latest)
				return
			}

			// Try to get checksum for archives if they exist
			if len(versionInfo.Archives) > 0 {
				archive := versionInfo.Archives[0]
				if osInfo, exists := archive.PlatformChecksum["linux"]; exists {
					if checksum, exists := osInfo["amd64"]; exists {
						require.NotEmpty(t, checksum.Algorithm, "Checksum algorithm should not be empty")
						require.NotEmpty(t, checksum.Value, "Checksum value should not be empty")
					}
				}
			}

			// Try to get checksum for binaries if they exist
			if len(versionInfo.Binaries) > 0 {
				binary := versionInfo.Binaries[0]
				if osInfo, exists := binary.PlatformChecksum["linux"]; exists {
					if checksum, exists := osInfo["amd64"]; exists {
						require.NotEmpty(t, checksum.Algorithm, "Checksum algorithm should not be empty")
						require.NotEmpty(t, checksum.Value, "Checksum value should not be empty")
					}
				}
			}
		})
	}
}

// Test_Config_GetDownloadURL_Integration tests getting download URLs from actual artifacts
func Test_Config_GetDownloadURL_Integration(t *testing.T) {
	config, err := LoadArtifactConfig()
	require.NoError(t, err)

	for _, artifact := range config.Artifact {
		t.Run(artifact.Name, func(t *testing.T) {
			// Get the latest version
			latest, err := artifact.GetLatestVersion()
			if err != nil {
				t.Skipf("Skipping %s: cannot get latest version", artifact.Name)
				return
			}

			// Try to get download URL for linux/amd64
			platformArtifact := artifact.withPlatform("linux", "amd64")

			// Get version info
			versionInfo, exists := platformArtifact.Versions[Version(latest)]
			if !exists {
				t.Skipf("Skipping %s: version %s not found", artifact.Name, latest)
				return
			}

			// Get the URL template from either archives or binaries
			var urlTemplate string
			if len(versionInfo.Archives) > 0 {
				urlTemplate = versionInfo.Archives[0].URL
			} else if len(versionInfo.Binaries) > 0 && versionInfo.Binaries[0].URL != "" {
				urlTemplate = versionInfo.Binaries[0].URL
			} else {
				t.Skipf("Skipping %s: no downloadable archives or binaries with URLs", artifact.Name)
				return
			}

			// Execute template to get actual URL
			platform := platformArtifact.getPlatform()
			data := TemplateData{
				VERSION: latest,
				OS:      platform.os,
				ARCH:    platform.arch,
			}

			url, err := executeTemplate(urlTemplate, data)

			// If successful, validate the URL
			if err == nil {
				require.NotEmpty(t, url, "Download URL should not be empty")
				require.Contains(t, url, "http", "Download URL should contain http")
				// Verify version was substituted
				require.Contains(t, url, latest, "Download URL should contain version %s", latest)
			}
		})
	}
}

// Test_Config_GetBinaries_Integration tests getting binaries from actual artifacts
func Test_Config_GetBinaries_Integration(t *testing.T) {
	config, err := LoadArtifactConfig()
	require.NoError(t, err)

	for _, artifact := range config.Artifact {
		t.Run(artifact.Name, func(t *testing.T) {
			// Get the latest version
			latest, err := artifact.GetLatestVersion()
			if err != nil {
				t.Skipf("Skipping %s: cannot get latest version", artifact.Name)
				return
			}

			// Get version info
			versionInfo, exists := artifact.Versions[Version(latest)]
			if !exists {
				t.Skipf("Skipping %s: version %s not found", artifact.Name, latest)
				return
			}

			// Try to get binaries for this version
			binaries := versionInfo.GetBinaries()
			require.NotEmpty(t, binaries, "Should have at least one binary")

			// Validate each binary has required fields
			for _, binary := range binaries {
				require.NotEmpty(t, binary.Name, "Binary name should not be empty")
				// Binary should have either URL or Archive field
				hasURL := binary.URL != ""
				hasArchive := binary.Archive != ""
				require.True(t, hasURL || hasArchive,
					"Binary %s should have either URL or Archive field", binary.Name)
			}
		})
	}
}

// Test_Config_VersionsAreSemanticVersions verifies all versions in artifact.yaml use semantic versioning
func Test_Config_VersionsAreSemanticVersions(t *testing.T) {
	config, err := LoadArtifactConfig()
	require.NoError(t, err)

	for _, artifact := range config.Artifact {
		t.Run(artifact.Name, func(t *testing.T) {
			for version := range artifact.Versions {
				versionStr := string(version)
				require.True(t, isValidVersion(versionStr),
					"Version %s for artifact %s should be a valid semantic version",
					versionStr, artifact.Name)
			}
		})
	}
}
