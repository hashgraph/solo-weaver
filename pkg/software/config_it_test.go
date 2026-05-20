// SPDX-License-Identifier: Apache-2.0

package software

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Test_Config_LoadInfrastructureCatalogYAML is an integration test that verifies the
// embedded infrastructure-catalog.yaml file can be loaded and parsed correctly into
// the InfrastructureCatalog structure.
func Test_Config_LoadInfrastructureCatalogYAML(t *testing.T) {
	config, err := LoadInfrastructureCatalog()
	require.NoError(t, err, "Should be able to load infrastructure-catalog.yaml")
	require.NotNil(t, config, "Config should not be nil")

	// Verify that we have artifacts defined
	require.NotEmpty(t, config.Host, "Should have at least one artifact defined")

	// Test each artifact in the configuration
	for _, artifact := range config.Host {
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

// Test_Config_GetSoftwareByName_Integration tests retrieving actual software from infrastructure-catalog.yaml
func Test_Config_GetSoftwareByName_Integration(t *testing.T) {
	config, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	// Test that we can find cri-o (which is shipped under host: in infrastructure-catalog.yaml)
	crioSoftware, err := config.GetHostArtifact("cri-o")
	if err == nil {
		// If cri-o exists, validate it
		require.NotNil(t, crioSoftware)
		require.Equal(t, "cri-o", crioSoftware.Name)
		require.NotEmpty(t, crioSoftware.Versions, "cri-o should have versions defined")
	}

	// Test non-existent software
	_, err = config.GetHostArtifact("non-existent-software")
	require.Error(t, err, "Should return error for non-existent software")
}

// Test_Config_GetDefaultVersion_Integration tests getting the default version from actual artifacts
func Test_Config_GetDefaultVersion_Integration(t *testing.T) {
	config, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	// Test each artifact declares an explicit default that resolves correctly
	for _, artifact := range config.Host {
		t.Run(artifact.Name, func(t *testing.T) {
			require.NotEmpty(t, artifact.Default,
				"Artifact %s must declare an explicit default version", artifact.Name)

			defaultVersion, err := artifact.GetDefaultVersion()
			require.NoError(t, err, "Should be able to get default version for %s", artifact.Name)
			require.NotEmpty(t, defaultVersion, "Default version should not be empty for %s", artifact.Name)

			// Verify the default version exists in the versions map
			_, exists := artifact.Versions[Version(defaultVersion)]
			require.True(t, exists, "Default version %s should exist in versions map for %s",
				defaultVersion, artifact.Name)
		})
	}
}

// Test_Config_GetChecksum_Integration tests getting checksums from actual artifacts
func Test_Config_GetChecksum_Integration(t *testing.T) {
	config, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	// Test a few artifacts if they exist
	for _, artifact := range config.Host {
		t.Run(artifact.Name, func(t *testing.T) {
			// Get the default version
			defaultVersion, err := artifact.GetDefaultVersion()
			if err != nil {
				t.Skipf("Skipping %s: cannot get default version", artifact.Name)
				return
			}

			versionInfo, exists := artifact.Versions[Version(defaultVersion)]
			if !exists {
				t.Skipf("Skipping %s: version %s not found", artifact.Name, defaultVersion)
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
	config, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for _, artifact := range config.Host {
		t.Run(artifact.Name, func(t *testing.T) {
			// Get the default version
			defaultVersion, err := artifact.GetDefaultVersion()
			if err != nil {
				t.Skipf("Skipping %s: cannot get default version", artifact.Name)
				return
			}

			// Try to get download URL for linux/amd64
			platformArtifact := artifact.withPlatform("linux", "amd64")

			// Get version info
			versionInfo, exists := platformArtifact.Versions[Version(defaultVersion)]
			if !exists {
				t.Skipf("Skipping %s: version %s not found", artifact.Name, defaultVersion)
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
				VERSION: defaultVersion,
				OS:      platform.os,
				ARCH:    platform.arch,
			}

			url, err := executeTemplate(urlTemplate, data)

			// If successful, validate the URL
			if err == nil {
				require.NotEmpty(t, url, "Download URL should not be empty")
				require.Contains(t, url, "http", "Download URL should contain http")
				// Verify version was substituted
				require.Contains(t, url, defaultVersion, "Download URL should contain version %s", defaultVersion)
			}
		})
	}
}

// Test_Config_GetBinaries_Integration tests getting binaries from actual artifacts
func Test_Config_GetBinaries_Integration(t *testing.T) {
	config, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for _, artifact := range config.Host {
		t.Run(artifact.Name, func(t *testing.T) {
			// Get the default version
			defaultVersion, err := artifact.GetDefaultVersion()
			if err != nil {
				t.Skipf("Skipping %s: cannot get default version", artifact.Name)
				return
			}

			// Get version info
			versionInfo, exists := artifact.Versions[Version(defaultVersion)]
			if !exists {
				t.Skipf("Skipping %s: version %s not found", artifact.Name, defaultVersion)
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

// Test_Config_VersionsAreSemanticVersions verifies that every version declared
// under `host:` and `cluster:` uses semantic versioning.
func Test_Config_VersionsAreSemanticVersions(t *testing.T) {
	config, err := LoadInfrastructureCatalog()
	require.NoError(t, err)

	for _, artifact := range config.Host {
		t.Run("host_"+artifact.Name, func(t *testing.T) {
			for version := range artifact.Versions {
				versionStr := string(version)
				require.True(t, isValidVersion(versionStr),
					"Version %s for host artifact %s should be a valid semantic version",
					versionStr, artifact.Name)
			}
		})
	}

	for _, chart := range config.Cluster {
		t.Run("cluster_"+chart.Name, func(t *testing.T) {
			for version := range chart.Versions {
				versionStr := string(version)
				require.True(t, isValidVersion(versionStr),
					"Version %s for cluster chart %s should be a valid semantic version",
					versionStr, chart.Name)
			}
		})
	}
}

// Test_Config_ClusterSection_Integration verifies the cluster section of the
// embedded catalog: every expected chart is present, every entry declares the
// fields enforced by the schema, and every version carries a non-empty
// algorithm/checksum.
func Test_Config_ClusterSection_Integration(t *testing.T) {
	config, err := LoadInfrastructureCatalog()
	require.NoError(t, err)
	require.NotEmpty(t, config.Cluster, "cluster section must not be empty")

	expectedCharts := map[string]struct {
		chartType ChartType
		version   string
	}{
		"alloy":                    {ChartTypeClassic, "1.4.0"},
		"teleport-cluster-agent":   {ChartTypeClassic, "18.6.4"},
		"metallb":                  {ChartTypeClassic, "0.15.2"},
		"metrics-server":           {ChartTypeClassic, "3.13.0"},
		"external-secrets":         {ChartTypeClassic, "0.20.2"},
		"node-exporter":            {ChartTypeOCI, "4.5.19"},
		"prometheus-operator-crds": {ChartTypeOCI, "24.0.1"},
		"solo-operator":            {ChartTypeOCI, "0.3.1"},
	}

	for name, expected := range expectedCharts {
		t.Run(name, func(t *testing.T) {
			chart, err := config.GetClusterComponent(name)
			require.NoError(t, err, "chart %s should be present in catalog", name)
			require.Equal(t, expected.chartType, chart.Type, "type mismatch for %s", name)
			require.NotEmpty(t, chart.Chart, "chart reference must not be empty for %s", name)
			if expected.chartType == ChartTypeClassic {
				require.NotEmpty(t, chart.Repo, "classic chart %s must declare a repo", name)
			} else {
				require.Empty(t, chart.Repo, "oci chart %s must not declare a repo", name)
			}
			defaultVersion, err := chart.GetDefaultVersion()
			require.NoError(t, err, "default version should resolve for %s", name)
			require.Equal(t, expected.version, defaultVersion, "default version mismatch for %s", name)
			for version, details := range chart.Versions {
				require.NotEmpty(t, details.Algorithm,
					"chart %s version %s: algorithm must not be empty", name, version)
				require.NotEmpty(t, details.Value,
					"chart %s version %s: checksum must not be empty", name, version)
			}
		})
	}
}
