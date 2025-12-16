// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestCreatePersistentVolumes_ValidYAMLOutput verifies that the generated storage config is valid YAML
func TestCreatePersistentVolumes_ValidYAMLOutput(t *testing.T) {
	tests := []struct {
		name        string
		liveSize    string
		archiveSize string
		logSize     string
	}{
		{
			name:        "default sizes",
			liveSize:    "5Gi",
			archiveSize: "5Gi",
			logSize:     "5Gi",
		},
		{
			name:        "custom sizes",
			liveSize:    "10Gi",
			archiveSize: "20Gi",
			logSize:     "5Gi",
		},
		{
			name:        "large sizes",
			liveSize:    "100Gi",
			archiveSize: "500Gi",
			logSize:     "50Gi",
		},
		{
			name:        "mixed units",
			liveSize:    "1024Mi",
			archiveSize: "1Ti",
			logSize:     "512Mi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			basePath := filepath.Join(tempDir, "storage")

			blockConfig := config.BlockNodeConfig{
				Namespace: "test-namespace",
				Release:   "test-release",
				Chart:     "test-chart",
				Version:   "0.1.0",
				Storage: config.BlockNodeStorage{
					BasePath:    basePath,
					LiveSize:    tt.liveSize,
					ArchiveSize: tt.archiveSize,
					LogSize:     tt.logSize,
				},
			}

			manager := &Manager{
				blockConfig: &blockConfig,
			}

			// Get the computed storage paths
			archivePath, livePath, logPath, err := manager.GetStoragePaths()
			require.NoError(t, err)

			// Prepare template data
			data := struct {
				Namespace   string
				LivePath    string
				ArchivePath string
				LogPath     string
				LiveSize    string
				ArchiveSize string
				LogSize     string
			}{
				Namespace:   manager.blockConfig.Namespace,
				LivePath:    livePath,
				ArchivePath: archivePath,
				LogPath:     logPath,
				LiveSize:    manager.blockConfig.Storage.LiveSize,
				ArchiveSize: manager.blockConfig.Storage.ArchiveSize,
				LogSize:     manager.blockConfig.Storage.LogSize,
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

			// Should have 6 documents (3 PVs + 3 PVCs)
			assert.Equal(t, 6, len(documents), "Should have 6 Kubernetes resources")

			// Count occurrences of each size in the YAML
			yamlStr := string(yamlContent)

			// When all sizes are the same, we need to handle it differently
			if tt.liveSize == tt.archiveSize && tt.archiveSize == tt.logSize {
				// All sizes are the same, should appear 6 times total (3 PVs + 3 PVCs)
				count := strings.Count(yamlStr, "storage: "+tt.liveSize)
				assert.Equal(t, 6, count, "When all sizes are equal, should appear 6 times total")
			} else {
				liveCount := strings.Count(yamlStr, "storage: "+tt.liveSize)
				archiveCount := strings.Count(yamlStr, "storage: "+tt.archiveSize)
				logCount := strings.Count(yamlStr, "storage: "+tt.logSize)

				assert.Equal(t, 2, liveCount, "Live storage size should appear twice (PV + PVC)")
				assert.Equal(t, 2, archiveCount, "Archive storage size should appear twice (PV + PVC)")
				assert.Equal(t, 2, logCount, "Log storage size should appear twice (PV + PVC)")
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

	blockConfig := config.BlockNodeConfig{
		Namespace: "block-node",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:    basePath,
			LiveSize:    "10Gi",
			ArchiveSize: "20Gi",
			LogSize:     "5Gi",
		},
	}

	manager := &Manager{
		blockConfig: &blockConfig,
	}

	// Get the computed storage paths
	archivePath, livePath, logPath, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// Prepare template data
	data := struct {
		Namespace   string
		LivePath    string
		ArchivePath string
		LogPath     string
		LiveSize    string
		ArchiveSize string
		LogSize     string
	}{
		Namespace:   manager.blockConfig.Namespace,
		LivePath:    livePath,
		ArchivePath: archivePath,
		LogPath:     logPath,
		LiveSize:    manager.blockConfig.Storage.LiveSize,
		ArchiveSize: manager.blockConfig.Storage.ArchiveSize,
		LogSize:     manager.blockConfig.Storage.LogSize,
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

	// Count occurrences to ensure proper replacement
	assert.Equal(t, 2, strings.Count(yamlStr, "storage: 10Gi"), "Should have exactly 2 occurrences of 10Gi (live PV + PVC)")
	assert.Equal(t, 2, strings.Count(yamlStr, "storage: 20Gi"), "Should have exactly 2 occurrences of 20Gi (archive PV + PVC)")
	assert.Equal(t, 2, strings.Count(yamlStr, "storage: 5Gi"), "Should have exactly 2 occurrences of 5Gi (log PV + PVC)")
}
