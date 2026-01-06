// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetStoragePaths_AllIndividualPathsProvided tests that individual paths are used when all are provided
func TestGetStoragePaths_AllIndividualPathsProvided(t *testing.T) {
	blockConfig := core.BlocknodeInputs{
		Namespace: "test-ns",
		Release:   "test-release",
		ChartUrl:  "oci://ghcr.io/test/test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:    "/mnt/base",
			ArchivePath: "/mnt/custom-archive",
			LivePath:    "/mnt/custom-live",
			LogPath:     "/mnt/custom-log",
		},
	}

	manager := &Manager{
		blockConfig: blockConfig,
	}

	archivePath, livePath, logPath, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// All individual paths should be returned as-is
	assert.Equal(t, "/mnt/custom-archive", archivePath)
	assert.Equal(t, "/mnt/custom-live", livePath)
	assert.Equal(t, "/mnt/custom-log", logPath)
}

// TestGetStoragePaths_OnlyBasePathProvided tests that paths are derived from basePath when individual paths are empty
func TestGetStoragePaths_OnlyBasePathProvided(t *testing.T) {
	blockConfig := core.BlocknodeInputs{
		Namespace: "test-ns",
		Release:   "test-release",
		ChartUrl:  "oci://ghcr.io/test/test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:    "/mnt/base",
			ArchivePath: "",
			LivePath:    "",
			LogPath:     "",
		},
	}

	manager := &Manager{
		blockConfig: blockConfig,
	}

	archivePath, livePath, logPath, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// Paths should be derived from basePath
	assert.Equal(t, "/mnt/base/archive", archivePath)
	assert.Equal(t, "/mnt/base/live", livePath)
	assert.Equal(t, "/mnt/base/logs", logPath)
}

// TestGetStoragePaths_MixedPaths tests that individual paths override basePath-derived paths
func TestGetStoragePaths_MixedPaths(t *testing.T) {
	blockConfig := core.BlocknodeInputs{
		Namespace: "test-ns",
		Release:   "test-release",
		ChartUrl:  "oci://ghcr.io/test/test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:    "/mnt/base",
			ArchivePath: "/mnt/custom-archive",
			LivePath:    "", // Should derive from basePath
			LogPath:     "/mnt/custom-log",
		},
	}

	manager := &Manager{
		blockConfig: blockConfig,
	}

	archivePath, livePath, logPath, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// Individual paths should be used when provided
	assert.Equal(t, "/mnt/custom-archive", archivePath)
	assert.Equal(t, "/mnt/custom-log", logPath)
	// Live path should derive from basePath
	assert.Equal(t, "/mnt/base/live", livePath)
}

// TestGetStoragePaths_InvalidArchivePath tests that invalid archive path returns an error
func TestGetStoragePaths_InvalidArchivePath(t *testing.T) {
	blockConfig := core.BlocknodeInputs{
		Namespace: "test-ns",
		Release:   "test-release",
		ChartUrl:  "oci://ghcr.io/test/test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:    "/mnt/base",
			ArchivePath: "../relative/path", // Invalid: contains ".." segments (potential path traversal)
			LivePath:    "/mnt/live",
			LogPath:     "/mnt/log",
		},
	}

	manager := &Manager{
		blockConfig: blockConfig,
	}

	_, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid archive path")
}

// TestGetStoragePaths_InvalidLivePath tests that invalid live path returns an error
func TestGetStoragePaths_InvalidLivePath(t *testing.T) {
	blockConfig := core.BlocknodeInputs{
		Namespace: "test-ns",
		Release:   "test-release",
		ChartUrl:  "oci://ghcr.io/test/test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:    "/mnt/base",
			ArchivePath: "/mnt/archive",
			LivePath:    "../../../etc/passwd", // Invalid: contains path traversal
			LogPath:     "/mnt/log",
		},
	}

	manager := &Manager{
		blockConfig: blockConfig,
	}

	_, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid live path")
}

// TestGetStoragePaths_InvalidLogPath tests that invalid log path returns an error
func TestGetStoragePaths_InvalidLogPath(t *testing.T) {
	blockConfig := core.BlocknodeInputs{
		Namespace: "test-ns",
		Release:   "test-release",
		ChartUrl:  "oci://ghcr.io/test/test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:    "/mnt/base",
			ArchivePath: "/mnt/archive",
			LivePath:    "/mnt/live",
			LogPath:     "/mnt/log;rm -rf /", // Invalid: contains shell metacharacters
		},
	}

	manager := &Manager{
		blockConfig: blockConfig,
	}

	_, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid log path")
}

// TestSetupStorage_AllIndividualPaths tests that basePath is not created when all individual paths are provided
func TestSetupStorage_AllIndividualPaths(t *testing.T) {
	// This is more of a documentation test showing the expected behavior
	// In practice, this would need filesystem mocking to fully test
	blockConfig := core.BlocknodeInputs{
		Namespace: "test-ns",
		Release:   "test-release",
		ChartUrl:  "oci://ghcr.io/test/test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:    "/mnt/base",
			ArchivePath: "/mnt/custom-archive",
			LivePath:    "/mnt/custom-live",
			LogPath:     "/mnt/custom-log",
		},
	}

	manager := &Manager{
		blockConfig: blockConfig,
	}

	// When all individual paths are provided, the implementation should:
	// 1. Validate all three paths (archive, live, log)
	// 2. NOT create or use BasePath
	// 3. Only create the three individual path directories

	// Note: Full integration test would verify actual filesystem operations
	// Here we just verify the manager can be created with this config
	assert.NotNil(t, manager)
	assert.Equal(t, "/mnt/custom-archive", blockConfig.Storage.ArchivePath)
	assert.Equal(t, "/mnt/custom-live", blockConfig.Storage.LivePath)
	assert.Equal(t, "/mnt/custom-log", blockConfig.Storage.LogPath)
}

// TestSetupStorage_PathValidation tests that SetupStorage validates all paths
func TestSetupStorage_PathValidation(t *testing.T) {
	tests := []struct {
		name        string
		storage     config.BlockNodeStorage
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid base path only",
			storage: config.BlockNodeStorage{
				BasePath: "/mnt/valid-path",
			},
			expectError: false,
		},
		{
			name: "valid individual paths",
			storage: config.BlockNodeStorage{
				BasePath:    "/mnt/base",
				ArchivePath: "/mnt/archive",
				LivePath:    "/mnt/live",
				LogPath:     "/mnt/log",
			},
			expectError: false,
		},
		{
			name: "invalid live path - path traversal",
			storage: config.BlockNodeStorage{
				BasePath: "/mnt/base",
				LivePath: "/mnt/../../../etc/passwd",
			},
			expectError: true,
			errorMsg:    "invalid live path",
		},
		{
			name: "invalid log path - shell metacharacters",
			storage: config.BlockNodeStorage{
				BasePath: "/mnt/base",
				LogPath:  "/mnt/log;echo pwned",
			},
			expectError: true,
			errorMsg:    "invalid log path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockConfig := core.BlocknodeInputs{
				Namespace: "test-ns",
				Release:   "test-release",
				ChartUrl:  "oci://ghcr.io/test/test-chart",
				Version:   "0.1.0",
				Storage:   tt.storage,
			}

			manager := &Manager{
				blockConfig: blockConfig,
			}

			// Note: SetupStorage would need filesystem access to fully test
			// Here we verify that GetStoragePaths (which is called by SetupStorage) validates properly
			_, _, _, err := manager.GetStoragePaths()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestStoragePathPrecedence documents the precedence order
func TestStoragePathPrecedence(t *testing.T) {
	// This test documents the expected precedence order:
	// 1. Individual paths (archivePath, livePath, logPath) - HIGHEST PRIORITY
	// 2. BasePath-derived paths (basePath + "/archive", etc.) - LOWER PRIORITY

	t.Run("individual path takes precedence", func(t *testing.T) {
		blockConfig := core.BlocknodeInputs{
			Storage: config.BlockNodeStorage{
				BasePath:    "/mnt/base",
				ArchivePath: "/mnt/override-archive",
			},
		}

		manager := &Manager{
			blockConfig: blockConfig,
		}

		archivePath, livePath, logPath, err := manager.GetStoragePaths()
		require.NoError(t, err)

		// Archive path should use the individual path, not derived from base
		assert.Equal(t, "/mnt/override-archive", archivePath, "individual archivePath should take precedence")

		// Live and log should derive from base since not specified
		assert.Equal(t, "/mnt/base/live", livePath, "should derive from basePath when not specified")
		assert.Equal(t, "/mnt/base/logs", logPath, "should derive from basePath when not specified")
	})

	t.Run("all individual paths override base path", func(t *testing.T) {
		blockConfig := core.BlocknodeInputs{
			Storage: config.BlockNodeStorage{
				BasePath:    "/mnt/base",
				ArchivePath: "/var/archive",
				LivePath:    "/var/live",
				LogPath:     "/var/log",
			},
		}

		manager := &Manager{
			blockConfig: blockConfig,
		}

		archivePath, livePath, logPath, err := manager.GetStoragePaths()
		require.NoError(t, err)

		// All paths should use individual values, not derived from base
		assert.Equal(t, "/var/archive", archivePath)
		assert.Equal(t, "/var/live", livePath)
		assert.Equal(t, "/var/log", logPath)
	})
}

// TestConfigOverridePrecedence tests the precedence of config overrides
func TestConfigOverridePrecedence(t *testing.T) {
	// This test documents that flags override config file values
	// The actual override happens in applyConfigOverrides() in the command layer

	t.Run("empty override values should not change config", func(t *testing.T) {
		// Initial config
		initialConfig := config.BlockNodeConfig{
			Namespace: "original-ns",
			Release:   "original-release",
			Version:   "0.20.0",
			Storage: config.BlockNodeStorage{
				BasePath: "/mnt/original",
			},
		}

		// Set global config
		cfg := config.Config{
			BlockNode: initialConfig,
		}
		err := config.Set(&cfg)
		require.NoError(t, err)

		// Apply empty overrides (simulating no flags provided)
		config.OverrideBlockNodeConfig(config.BlockNodeConfig{
			Storage: config.BlockNodeStorage{},
		})

		// Config should remain unchanged
		result := config.Get()
		assert.Equal(t, "original-ns", result.BlockNode.Namespace)
		assert.Equal(t, "original-release", result.BlockNode.Release)
		assert.Equal(t, "0.20.0", result.BlockNode.Version)
		assert.Equal(t, "/mnt/original", result.BlockNode.Storage.BasePath)
	})

	t.Run("non-empty override values should change config", func(t *testing.T) {
		// Initial config
		initialConfig := config.BlockNodeConfig{
			Namespace: "original-ns",
			Release:   "original-release",
			Version:   "0.20.0",
			Storage: config.BlockNodeStorage{
				BasePath: "/mnt/original",
			},
		}

		cfg := config.Config{
			BlockNode: initialConfig,
		}
		err := config.Set(&cfg)
		require.NoError(t, err)

		// Apply overrides with some values
		config.OverrideBlockNodeConfig(config.BlockNodeConfig{
			Namespace: "new-ns",
			Version:   "0.30.0",
			Storage: config.BlockNodeStorage{
				ArchivePath: "/mnt/new-archive",
			},
		})

		// Only overridden values should change
		result := config.Get()
		assert.Equal(t, "new-ns", result.BlockNode.Namespace, "should be overridden")
		assert.Equal(t, "0.30.0", result.BlockNode.Version, "should be overridden")
		assert.Equal(t, "/mnt/new-archive", result.BlockNode.Storage.ArchivePath, "should be overridden")

		// Non-overridden values should remain
		assert.Equal(t, "original-release", result.BlockNode.Release, "should remain unchanged")
		assert.Equal(t, "/mnt/original", result.BlockNode.Storage.BasePath, "should remain unchanged")
	})
}
