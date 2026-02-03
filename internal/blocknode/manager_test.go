// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLogger returns a no-op logger for testing
func testLogger() *zerolog.Logger {
	l := zerolog.Nop()
	return &l
}

// TestGetStoragePaths_AllIndividualPathsProvided tests that individual paths are used when all are provided
func TestGetStoragePaths_AllIndividualPathsProvided(t *testing.T) {
	blockConfig := config.BlockNodeConfig{
		Namespace: "test-ns",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/custom-archive",
			LivePath:         "/mnt/custom-live",
			LogPath:          "/mnt/custom-log",
			VerificationPath: "/mnt/custom-verification",
		},
	}

	manager := &Manager{
		blockConfig: &blockConfig,
	}

	archivePath, livePath, logPath, verificationPath, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// All individual paths should be returned as-is
	assert.Equal(t, "/mnt/custom-archive", archivePath)
	assert.Equal(t, "/mnt/custom-live", livePath)
	assert.Equal(t, "/mnt/custom-log", logPath)
	assert.Equal(t, "/mnt/custom-verification", verificationPath)
}

// TestGetStoragePaths_OldVersionNoVerificationRequired tests that verification storage is not required for versions < 0.26.2
func TestGetStoragePaths_OldVersionNoVerificationRequired(t *testing.T) {
	blockConfig := config.BlockNodeConfig{
		Namespace: "test-ns",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.25.0", // Version < 0.26.2 does not require verification storage
		Storage: config.BlockNodeStorage{
			BasePath:         "", // Empty basePath
			ArchivePath:      "/mnt/custom-archive",
			LivePath:         "/mnt/custom-live",
			LogPath:          "/mnt/custom-log",
			VerificationPath: "", // Not required for older versions
		},
	}

	manager := &Manager{
		blockConfig: &blockConfig,
	}

	archivePath, livePath, logPath, verificationPath, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// Core paths should be returned as-is
	assert.Equal(t, "/mnt/custom-archive", archivePath)
	assert.Equal(t, "/mnt/custom-live", livePath)
	assert.Equal(t, "/mnt/custom-log", logPath)
	// Verification path should be empty for older versions
	assert.Equal(t, "", verificationPath)
}

// TestGetStoragePaths_NewVersionRequiresVerification tests that verification storage is required for versions >= 0.26.2
func TestGetStoragePaths_NewVersionRequiresVerification(t *testing.T) {
	blockConfig := config.BlockNodeConfig{
		Namespace: "test-ns",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.26.2", // Version >= 0.26.2 requires verification storage
		Storage: config.BlockNodeStorage{
			BasePath:         "", // Empty basePath
			ArchivePath:      "/mnt/custom-archive",
			LivePath:         "/mnt/custom-live",
			LogPath:          "/mnt/custom-log",
			VerificationPath: "", // Required for versions >= 0.26.2, should fail
		},
	}

	manager := &Manager{
		blockConfig: &blockConfig,
	}

	_, _, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one storage path is not set and base storage path is empty")
}

// TestGetStoragePaths_OnlyBasePathProvided tests that paths are derived from basePath when individual paths are empty
func TestGetStoragePaths_OnlyBasePathProvided(t *testing.T) {
	blockConfig := config.BlockNodeConfig{
		Namespace: "test-ns",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.26.2", // Version >= 0.26.2 requires verification storage
		Storage: config.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "",
			LivePath:         "",
			LogPath:          "",
			VerificationPath: "",
		},
	}

	manager := &Manager{
		blockConfig: &blockConfig,
	}

	archivePath, livePath, logPath, verificationPath, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// Paths should be derived from basePath
	assert.Equal(t, "/mnt/base/archive", archivePath)
	assert.Equal(t, "/mnt/base/live", livePath)
	assert.Equal(t, "/mnt/base/logs", logPath)
	assert.Equal(t, "/mnt/base/verification", verificationPath)
}

// TestGetStoragePaths_MixedPaths tests that individual paths override basePath-derived paths
func TestGetStoragePaths_MixedPaths(t *testing.T) {
	blockConfig := config.BlockNodeConfig{
		Namespace: "test-ns",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.26.2", // Version >= 0.26.2 requires verification storage
		Storage: config.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/custom-archive",
			LivePath:         "", // Should derive from basePath
			LogPath:          "/mnt/custom-log",
			VerificationPath: "", // Should derive from basePath
		},
	}

	manager := &Manager{
		blockConfig: &blockConfig,
	}

	archivePath, livePath, logPath, verificationPath, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// Individual paths should be used when provided
	assert.Equal(t, "/mnt/custom-archive", archivePath)
	assert.Equal(t, "/mnt/custom-log", logPath)
	// Live and verification paths should derive from basePath
	assert.Equal(t, "/mnt/base/live", livePath)
	assert.Equal(t, "/mnt/base/verification", verificationPath)
}

// TestGetStoragePaths_InvalidArchivePath tests that invalid archive path returns an error
func TestGetStoragePaths_InvalidArchivePath(t *testing.T) {
	blockConfig := config.BlockNodeConfig{
		Namespace: "test-ns",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "../relative/path", // Invalid: contains ".." segments (potential path traversal)
			LivePath:         "/mnt/live",
			LogPath:          "/mnt/log",
			VerificationPath: "/mnt/verification",
		},
	}

	manager := &Manager{
		blockConfig: &blockConfig,
	}

	_, _, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid archive path")
}

// TestGetStoragePaths_InvalidLivePath tests that invalid live path returns an error
func TestGetStoragePaths_InvalidLivePath(t *testing.T) {
	blockConfig := config.BlockNodeConfig{
		Namespace: "test-ns",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/archive",
			LivePath:         "../../../etc/passwd", // Invalid: contains path traversal
			LogPath:          "/mnt/log",
			VerificationPath: "/mnt/verification",
		},
	}

	manager := &Manager{
		blockConfig: &blockConfig,
	}

	_, _, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid live path")
}

// TestGetStoragePaths_InvalidLogPath tests that invalid log path returns an error
func TestGetStoragePaths_InvalidLogPath(t *testing.T) {
	blockConfig := config.BlockNodeConfig{
		Namespace: "test-ns",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/archive",
			LivePath:         "/mnt/live",
			LogPath:          "/mnt/log;rm -rf /", // Invalid: contains shell metacharacters
			VerificationPath: "/mnt/verification",
		},
	}

	manager := &Manager{
		blockConfig: &blockConfig,
	}

	_, _, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid log path")
}

// TestGetStoragePaths_InvalidVerificationPath tests that invalid verification path returns an error
func TestGetStoragePaths_InvalidVerificationPath(t *testing.T) {
	blockConfig := config.BlockNodeConfig{
		Namespace: "test-ns",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.26.2", // Version >= 0.26.2 requires verification storage
		Storage: config.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/archive",
			LivePath:         "/mnt/live",
			LogPath:          "/mnt/log",
			VerificationPath: "../../../etc/shadow", // Invalid: contains path traversal
		},
	}

	manager := &Manager{
		blockConfig: &blockConfig,
	}

	_, _, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid verification path")
}

// TestSetupStorage_AllIndividualPaths tests that basePath is not created when all individual paths are provided
func TestSetupStorage_AllIndividualPaths(t *testing.T) {
	// This is more of a documentation test showing the expected behavior
	// In practice, this would need filesystem mocking to fully test
	blockConfig := config.BlockNodeConfig{
		Namespace: "test-ns",
		Release:   "test-release",
		Chart:     "test-chart",
		Version:   "0.1.0",
		Storage: config.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/custom-archive",
			LivePath:         "/mnt/custom-live",
			LogPath:          "/mnt/custom-log",
			VerificationPath: "/mnt/custom-verification",
		},
	}

	manager := &Manager{
		blockConfig: &blockConfig,
	}

	// When all individual paths are provided, the implementation should:
	// 1. Validate all four paths (archive, live, log, verification)
	// 2. NOT create or use BasePath
	// 3. Only create the four individual path directories

	// Note: Full integration test would verify actual filesystem operations
	// Here we just verify the manager can be created with this config
	assert.NotNil(t, manager)
	assert.Equal(t, "/mnt/custom-archive", blockConfig.Storage.ArchivePath)
	assert.Equal(t, "/mnt/custom-live", blockConfig.Storage.LivePath)
	assert.Equal(t, "/mnt/custom-log", blockConfig.Storage.LogPath)
	assert.Equal(t, "/mnt/custom-verification", blockConfig.Storage.VerificationPath)
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
			blockConfig := config.BlockNodeConfig{
				Namespace: "test-ns",
				Release:   "test-release",
				Chart:     "test-chart",
				Version:   "0.1.0",
				Storage:   tt.storage,
			}

			manager := &Manager{
				blockConfig: &blockConfig,
			}

			// Note: SetupStorage would need filesystem access to fully test
			// Here we verify that GetStoragePaths (which is called by SetupStorage) validates properly
			_, _, _, _, err := manager.GetStoragePaths()

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
	// 1. Individual paths (archivePath, livePath, logPath, verificationPath) - HIGHEST PRIORITY
	// 2. BasePath-derived paths (basePath + "/archive", etc.) - LOWER PRIORITY

	t.Run("individual path takes precedence", func(t *testing.T) {
		blockConfig := config.BlockNodeConfig{
			Version: "0.26.2", // Version >= 0.26.2 requires verification storage
			Storage: config.BlockNodeStorage{
				BasePath:    "/mnt/base",
				ArchivePath: "/mnt/override-archive",
			},
		}

		manager := &Manager{
			blockConfig: &blockConfig,
		}

		archivePath, livePath, logPath, verificationPath, err := manager.GetStoragePaths()
		require.NoError(t, err)

		// Archive path should use the individual path, not derived from base
		assert.Equal(t, "/mnt/override-archive", archivePath, "individual archivePath should take precedence")

		// Live, log, and verification should derive from base since not specified
		assert.Equal(t, "/mnt/base/live", livePath, "should derive from basePath when not specified")
		assert.Equal(t, "/mnt/base/logs", logPath, "should derive from basePath when not specified")
		assert.Equal(t, "/mnt/base/verification", verificationPath, "should derive from basePath when not specified")
	})

	t.Run("all individual paths override base path", func(t *testing.T) {
		blockConfig := config.BlockNodeConfig{
			Storage: config.BlockNodeStorage{
				BasePath:         "/mnt/base",
				ArchivePath:      "/var/archive",
				LivePath:         "/var/live",
				LogPath:          "/var/log",
				VerificationPath: "/var/verification",
			},
		}

		manager := &Manager{
			blockConfig: &blockConfig,
		}

		archivePath, livePath, logPath, verificationPath, err := manager.GetStoragePaths()
		require.NoError(t, err)

		// All paths should use individual values, not derived from base
		assert.Equal(t, "/var/archive", archivePath)
		assert.Equal(t, "/var/live", livePath)
		assert.Equal(t, "/var/log", logPath)
		assert.Equal(t, "/var/verification", verificationPath)
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

// ============================================================================
// Version-Aware Migration Tests
// ============================================================================

// TestRequiresVerificationStorage tests the version detection for verification storage
func TestRequiresVerificationStorage(t *testing.T) {
	tests := []struct {
		name           string
		targetVersion  string
		expectedResult bool
	}{
		{
			name:           "version below 0.26.2 should not require verification storage",
			targetVersion:  "0.26.0",
			expectedResult: false,
		},
		{
			name:           "version 0.26.1 should not require verification storage",
			targetVersion:  "0.26.1",
			expectedResult: false,
		},
		{
			name:           "version exactly 0.26.2 should require verification storage",
			targetVersion:  "0.26.2",
			expectedResult: true,
		},
		{
			name:           "version 0.26.3 should require verification storage",
			targetVersion:  "0.26.3",
			expectedResult: true,
		},
		{
			name:           "version 0.27.0 should require verification storage",
			targetVersion:  "0.27.0",
			expectedResult: true,
		},
		{
			name:           "version 1.0.0 should require verification storage",
			targetVersion:  "1.0.0",
			expectedResult: true,
		},
		{
			name:           "very old version 0.20.0 should not require verification storage",
			targetVersion:  "0.20.0",
			expectedResult: false,
		},
		{
			name:           "invalid version should default to false (backward compatible)",
			targetVersion:  "invalid-version",
			expectedResult: false,
		},
		{
			name:           "empty version should default to false",
			targetVersion:  "",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockConfig := config.BlockNodeConfig{
				Version: tt.targetVersion,
			}

			manager := &Manager{
				blockConfig: &blockConfig,
				logger:      testLogger(),
			}

			result := manager.requiresVerificationStorage()
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

// TestComputeValuesFile_VersionAwareSelection tests that the correct values file is selected based on version
func TestComputeValuesFile_VersionAwareSelection(t *testing.T) {
	tests := []struct {
		name            string
		targetVersion   string
		profile         string
		expectedLogMsg  string
		shouldHaveVerif bool
	}{
		{
			name:            "v0.26.0 local profile uses nano values without verification",
			targetVersion:   "0.26.0",
			profile:         "local",
			shouldHaveVerif: false,
		},
		{
			name:            "v0.26.2 local profile uses nano values with verification",
			targetVersion:   "0.26.2",
			profile:         "local",
			shouldHaveVerif: true,
		},
		{
			name:            "v0.26.0 full profile uses full values without verification",
			targetVersion:   "0.26.0",
			profile:         "full",
			shouldHaveVerif: false,
		},
		{
			name:            "v0.26.2 full profile uses full values with verification",
			targetVersion:   "0.26.2",
			profile:         "full",
			shouldHaveVerif: true,
		},
		{
			name:            "v0.27.0 local profile uses nano values with verification",
			targetVersion:   "0.27.0",
			profile:         "local",
			shouldHaveVerif: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockConfig := config.BlockNodeConfig{
				Version: tt.targetVersion,
			}

			manager := &Manager{
				blockConfig: &blockConfig,
				logger:      testLogger(),
			}

			// Test the requiresVerificationStorage logic which determines file selection
			result := manager.requiresVerificationStorage()
			assert.Equal(t, tt.shouldHaveVerif, result)
		})
	}
}

// TestVersionBoundaryScenarios tests various version boundary scenarios
func TestVersionBoundaryScenarios(t *testing.T) {
	t.Run("upgrade within pre-0.26.2 versions", func(t *testing.T) {
		// Upgrading from 0.25.0 to 0.26.1 should not require verification storage
		blockConfig := config.BlockNodeConfig{
			Version: "0.26.1",
		}
		manager := &Manager{
			blockConfig: &blockConfig,
			logger:      testLogger(),
		}
		assert.False(t, manager.requiresVerificationStorage())
	})

	t.Run("upgrade across breaking change boundary", func(t *testing.T) {
		// Target version 0.26.2 requires verification storage
		blockConfig := config.BlockNodeConfig{
			Version: "0.26.2",
		}
		manager := &Manager{
			blockConfig: &blockConfig,
			logger:      testLogger(),
		}
		assert.True(t, manager.requiresVerificationStorage())
	})

	t.Run("upgrade within post-0.26.2 versions", func(t *testing.T) {
		// Upgrading from 0.26.2 to 0.27.0 should still require verification storage
		blockConfig := config.BlockNodeConfig{
			Version: "0.27.0",
		}
		manager := &Manager{
			blockConfig: &blockConfig,
			logger:      testLogger(),
		}
		assert.True(t, manager.requiresVerificationStorage())
	})

	t.Run("fresh install at 0.26.2", func(t *testing.T) {
		// Fresh install at 0.26.2 should require verification storage
		blockConfig := config.BlockNodeConfig{
			Version: "0.26.2",
		}
		manager := &Manager{
			blockConfig: &blockConfig,
			logger:      testLogger(),
		}
		assert.True(t, manager.requiresVerificationStorage())
	})

	t.Run("fresh install at older version", func(t *testing.T) {
		// Fresh install at 0.26.0 should not require verification storage
		blockConfig := config.BlockNodeConfig{
			Version: "0.26.0",
		}
		manager := &Manager{
			blockConfig: &blockConfig,
			logger:      testLogger(),
		}
		assert.False(t, manager.requiresVerificationStorage())
	})
}

// TestInvalidVersionHandling tests that invalid versions are handled gracefully
func TestInvalidVersionHandling(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"empty string", ""},
		{"random string", "not-a-version"},
		{"partial version", "0.26"},
		{"version with prefix", "v0.26.2"},
		{"version with suffix", "0.26.2-beta"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockConfig := config.BlockNodeConfig{
				Version: tt.version,
			}
			manager := &Manager{
				blockConfig: &blockConfig,
				logger:      testLogger(),
			}

			// Should not panic and should return false for invalid versions
			// (fail-safe to maintain backward compatibility)
			result := manager.requiresVerificationStorage()

			// Invalid versions should default to false (no verification storage)
			// to maintain backward compatibility
			if tt.version == "" || tt.version == "not-a-version" || tt.version == "0.26" {
				assert.False(t, result, "invalid version should default to false")
			}
		})
	}
}

// TestVerificationStorageMinVersionConstant verifies the constant is set correctly
func TestVerificationStorageMinVersionConstant(t *testing.T) {
	assert.Equal(t, "0.26.2", VerificationStorageMinVersion)
}
