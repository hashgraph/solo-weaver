// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestCreatePersistentVolumes_ValidYAMLOutput verifies that the generated storage config is valid YAML
func TestCreatePersistentVolumes_ValidYAMLOutput(t *testing.T) {
	tests := []struct {
		name             string
		liveSize         string
		archiveSize      string
		logSize          string
		verificationSize string
		pluginsSize      string
	}{
		{
			name:             "default sizes",
			liveSize:         "5Gi",
			archiveSize:      "5Gi",
			logSize:          "5Gi",
			verificationSize: "5Gi",
			pluginsSize:      "5Gi",
		},
		{
			name:             "custom sizes",
			liveSize:         "10Gi",
			archiveSize:      "20Gi",
			logSize:          "3Gi",
			verificationSize: "15Gi",
			pluginsSize:      "7Gi",
		},
		{
			name:             "large sizes",
			liveSize:         "100Gi",
			archiveSize:      "500Gi",
			logSize:          "50Gi",
			verificationSize: "200Gi",
			pluginsSize:      "75Gi",
		},
		{
			name:             "mixed units",
			liveSize:         "1024Mi",
			archiveSize:      "1Ti",
			logSize:          "512Mi",
			verificationSize: "2048Mi",
			pluginsSize:      "256Mi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			basePath := filepath.Join(tempDir, "storage")

			blockConfig := core.BlocknodeInputs{
				Namespace: "test-namespace",
				Release:   "test-release",
				Chart:     "test-chart",
				Version:   "0.1.0",
				Storage: core.BlockNodeStorage{
					BasePath:         basePath,
					LiveSize:         tt.liveSize,
					ArchiveSize:      tt.archiveSize,
					LogSize:          tt.logSize,
					VerificationSize: tt.verificationSize,
					PluginsSize:      tt.pluginsSize,
				},
			}

			manager := &Manager{
				blockConfig: blockConfig,
			}

			// Get the computed storage paths
			// Use a version that includes all optional storages for this test
			manager.blockConfig.Version = "0.28.1"
			archivePath, livePath, logPath, optionalPaths, err := manager.GetStoragePaths()
			manager.blockConfig.Version = "0.1.0"
			require.NoError(t, err)

			// Map optional paths by name using the registry order
			verificationPath := ""
			pluginsPath := ""
			applicable := GetApplicableOptionalStorages("0.28.1")
			for i, optStor := range applicable {
				if i < len(optionalPaths) {
					switch optStor.Name {
					case "verification":
						verificationPath = optionalPaths[i]
					case "plugins":
						pluginsPath = optionalPaths[i]
					}
				}
			}

			// Prepare template data
			data := struct {
				Namespace           string
				LivePath            string
				ArchivePath         string
				LogPath             string
				VerificationPath    string
				PluginsPath         string
				LiveSize            string
				ArchiveSize         string
				LogSize             string
				VerificationSize    string
				PluginsSize         string
				IncludeVerification bool
				IncludePlugins      bool
			}{
				Namespace:           manager.blockConfig.Namespace,
				LivePath:            livePath,
				ArchivePath:         archivePath,
				LogPath:             logPath,
				VerificationPath:    verificationPath,
				PluginsPath:         pluginsPath,
				LiveSize:            manager.blockConfig.Storage.LiveSize,
				ArchiveSize:         manager.blockConfig.Storage.ArchiveSize,
				LogSize:             manager.blockConfig.Storage.LogSize,
				VerificationSize:    manager.blockConfig.Storage.VerificationSize,
				PluginsSize:         manager.blockConfig.Storage.PluginsSize,
				IncludeVerification: true, // Include verification storage in tests
				IncludePlugins:      true, // Include plugins storage in tests
			}

			// Render the storage config template
			storageConfig, err := templates.Render("files/block-node/storage-config.yaml", data)
			require.NoError(t, err)

			// Write to temp file
			configFilePath := filepath.Join(tempDir, "block-node-storage-config.yaml")
			err = os.WriteFile(configFilePath, []byte(storageConfig), 0644)
			require.NoError(t, err)

			// Read the generated YAML file
			yamlContent, err := os.ReadFile(configFilePath)
			require.NoError(t, err)

			// Verify it's valid YAML by unmarshaling it
			var documents []map[string]interface{}
			decoder := yaml.NewDecoder(strings.NewReader(string(yamlContent)))
			for {
				var doc map[string]interface{}
				if err := decoder.Decode(&doc); err != nil {
					break
				}
				if doc != nil {
					documents = append(documents, doc)
				}
			}

			// Should have 10 documents (5 PVs + 5 PVCs)
			assert.Equal(t, 10, len(documents), "Should have 10 Kubernetes resources")

			// Count occurrences of each size in the YAML
			yamlStr := string(yamlContent)

			// When all sizes are the same, we need to handle it differently
			if tt.liveSize == tt.archiveSize && tt.archiveSize == tt.logSize && tt.logSize == tt.verificationSize {
				// All sizes are the same, should appear 10 times total (5 PVs + 5 PVCs)
				count := strings.Count(yamlStr, "storage: "+tt.liveSize)
				assert.Equal(t, 10, count, "When all sizes are equal, should appear 10 times total")
			} else {
				liveCount := strings.Count(yamlStr, "storage: "+tt.liveSize)
				archiveCount := strings.Count(yamlStr, "storage: "+tt.archiveSize)
				logCount := strings.Count(yamlStr, "storage: "+tt.logSize)
				verificationCount := strings.Count(yamlStr, "storage: "+tt.verificationSize)
				pluginsCount := strings.Count(yamlStr, "storage: "+tt.pluginsSize)

				assert.Equal(t, 2, liveCount, "Live storage size should appear twice (PV + PVC)")
				assert.Equal(t, 2, archiveCount, "Archive storage size should appear twice (PV + PVC)")
				assert.Equal(t, 2, logCount, "Log storage size should appear twice (PV + PVC)")
				assert.Equal(t, 2, verificationCount, "Verification storage size should appear twice (PV + PVC)")
				assert.Equal(t, 2, pluginsCount, "Plugins storage size should appear twice (PV + PVC)")
			}

			// Verify namespace is correctly set for PVCs
			assert.Contains(t, yamlStr, "namespace: test-namespace", "PVCs should have correct namespace")
		})
	}
}

// TestStorageConfigNoCorruption verifies that storage config replacement doesn't corrupt the YAML
func TestStorageConfigNoCorruption(t *testing.T) {
	tempDir := t.TempDir()
	basePath := filepath.Join(tempDir, "storage")

	blockConfig := core.BlocknodeInputs{
		Namespace: "block-node",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.1.0",
		Storage: core.BlockNodeStorage{
			BasePath:         basePath,
			LiveSize:         "10Gi",
			ArchiveSize:      "20Gi",
			LogSize:          "5Gi",
			VerificationSize: "15Gi",
			PluginsSize:      "8Gi",
		},
	}

	manager := &Manager{
		blockConfig: blockConfig,
	}

	// Get the computed storage paths
	// Use a version that includes all optional storages for this test
	manager.blockConfig.Version = "0.28.1"
	archivePath, livePath, logPath, optionalPaths, err := manager.GetStoragePaths()
	manager.blockConfig.Version = "0.1.0"
	require.NoError(t, err)

	// Map optional paths by name using the registry order
	verificationPath := ""
	pluginsPath := ""
	applicable := GetApplicableOptionalStorages("0.28.1")
	for i, optStor := range applicable {
		if i < len(optionalPaths) {
			switch optStor.Name {
			case "verification":
				verificationPath = optionalPaths[i]
			case "plugins":
				pluginsPath = optionalPaths[i]
			}
		}
	}

	// Prepare template data
	data := struct {
		Namespace           string
		LivePath            string
		ArchivePath         string
		LogPath             string
		VerificationPath    string
		PluginsPath         string
		LiveSize            string
		ArchiveSize         string
		LogSize             string
		VerificationSize    string
		PluginsSize         string
		IncludeVerification bool
		IncludePlugins      bool
	}{
		Namespace:           manager.blockConfig.Namespace,
		LivePath:            livePath,
		ArchivePath:         archivePath,
		LogPath:             logPath,
		VerificationPath:    verificationPath,
		PluginsPath:         pluginsPath,
		LiveSize:            manager.blockConfig.Storage.LiveSize,
		ArchiveSize:         manager.blockConfig.Storage.ArchiveSize,
		LogSize:             manager.blockConfig.Storage.LogSize,
		VerificationSize:    manager.blockConfig.Storage.VerificationSize,
		PluginsSize:         manager.blockConfig.Storage.PluginsSize,
		IncludeVerification: true,
		IncludePlugins:      true,
	}

	// Render the storage config template
	storageConfig, err := templates.Render("files/block-node/storage-config.yaml", data)
	require.NoError(t, err)

	// Write to temp file
	configFilePath := filepath.Join(tempDir, "block-node-storage-config.yaml")
	err = os.WriteFile(configFilePath, []byte(storageConfig), 0644)
	require.NoError(t, err)

	// Read the generated YAML file
	yamlContent, err := os.ReadFile(configFilePath)
	require.NoError(t, err)

	yamlStr := string(yamlContent)

	// Verify no malformed patterns
	assert.NotContains(t, yamlStr, "storage: storage:", "Should not have duplicate 'storage:' prefixes")
	assert.NotContains(t, yamlStr, "5Gi 10Gi", "Should not have concatenated sizes")
	assert.NotContains(t, yamlStr, "5Gi 20Gi", "Should not have concatenated sizes")
	assert.NotContains(t, yamlStr, "5Gi 5Gi", "Should not have duplicate sizes")

	// Verify correct patterns exist
	assert.Contains(t, yamlStr, "storage: 10Gi", "Should contain live storage size")
	assert.Contains(t, yamlStr, "storage: 20Gi", "Should contain archive storage size")
	assert.Contains(t, yamlStr, "storage: 5Gi", "Should contain log storage size")
	assert.Contains(t, yamlStr, "storage: 15Gi", "Should contain verification storage size")
	assert.Contains(t, yamlStr, "storage: 8Gi", "Should contain plugins storage size")

	// Count occurrences to ensure proper replacement
	assert.Equal(t, 2, strings.Count(yamlStr, "storage: 10Gi"), "Should have exactly 2 occurrences of 10Gi (live PV + PVC)")
	assert.Equal(t, 2, strings.Count(yamlStr, "storage: 20Gi"), "Should have exactly 2 occurrences of 20Gi (archive PV + PVC)")
	assert.Equal(t, 2, strings.Count(yamlStr, "storage: 5Gi"), "Should have exactly 2 occurrences of 5Gi (log PV + PVC)")
	assert.Equal(t, 2, strings.Count(yamlStr, "storage: 15Gi"), "Should have exactly 2 occurrences of 15Gi (verification PV + PVC)")
	assert.Equal(t, 2, strings.Count(yamlStr, "storage: 8Gi"), "Should have exactly 2 occurrences of 8Gi (plugins PV + PVC)")
}
