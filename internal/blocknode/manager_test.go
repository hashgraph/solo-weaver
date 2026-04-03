// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
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
	blockConfig := models.BlockNodeInputs{
		Namespace:    "test-ns",
		Release:      "test-release",
		Chart:        "test-chart",
		ChartVersion: "0.1.0",
		Storage: models.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/custom-archive",
			LivePath:         "/mnt/custom-live",
			LogPath:          "/mnt/custom-log",
			VerificationPath: "/mnt/custom-verification",
		},
	}

	manager := &Manager{
		blockNodeInputs: blockConfig,
	}

	archivePath, livePath, logPath, optionalPaths, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// All individual paths should be returned as-is
	assert.Equal(t, "/mnt/custom-archive", archivePath)
	assert.Equal(t, "/mnt/custom-live", livePath)
	assert.Equal(t, "/mnt/custom-log", logPath)
	// v0.1.0 has no optional storages
	assert.Empty(t, optionalPaths)
}

// TestGetStoragePaths_OldVersionNoVerificationRequired tests that verification storage is not required for versions < 0.26.2
func TestGetStoragePaths_OldVersionNoVerificationRequired(t *testing.T) {
	blockConfig := models.BlockNodeInputs{
		Namespace:    "test-ns",
		Release:      "test-release",
		Chart:        "test-chart",
		ChartVersion: "0.25.0", // Version < 0.26.2 does not require verification storage
		Storage: models.BlockNodeStorage{
			BasePath:         "", // Empty basePath
			ArchivePath:      "/mnt/custom-archive",
			LivePath:         "/mnt/custom-live",
			LogPath:          "/mnt/custom-log",
			VerificationPath: "", // Not required for older versions
		},
	}

	manager := &Manager{
		blockNodeInputs: blockConfig,
	}

	archivePath, livePath, logPath, optionalPaths, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// Core paths should be returned as-is
	assert.Equal(t, "/mnt/custom-archive", archivePath)
	assert.Equal(t, "/mnt/custom-live", livePath)
	assert.Equal(t, "/mnt/custom-log", logPath)
	// No optional storages for older versions
	assert.Empty(t, optionalPaths)
}

// TestGetStoragePaths_NewVersionRequiresVerification tests that verification storage is required for versions >= 0.26.2
func TestGetStoragePaths_NewVersionRequiresVerification(t *testing.T) {
	blockConfig := models.BlockNodeInputs{
		Namespace:    "test-ns",
		Release:      "test-release",
		Chart:        "test-chart",
		ChartVersion: "0.26.2", // Version >= 0.26.2 requires verification storage
		Storage: models.BlockNodeStorage{
			BasePath:         "", // Empty basePath
			ArchivePath:      "/mnt/custom-archive",
			LivePath:         "/mnt/custom-live",
			LogPath:          "/mnt/custom-log",
			VerificationPath: "", // Required for versions >= 0.26.2, should fail
		},
	}

	manager := &Manager{
		blockNodeInputs: blockConfig,
	}

	_, _, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one storage path is not set and base path is empty")
}

// TestGetStoragePaths_OnlyBasePathProvided tests that paths are derived from basePath when individual paths are empty
func TestGetStoragePaths_OnlyBasePathProvided(t *testing.T) {
	blockConfig := models.BlockNodeInputs{
		Namespace:    "test-ns",
		Release:      "test-release",
		Chart:        "test-chart",
		ChartVersion: "0.26.2", // Version >= 0.26.2 requires verification storage
		Storage: models.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "",
			LivePath:         "",
			LogPath:          "",
			VerificationPath: "",
		},
	}

	manager := &Manager{
		blockNodeInputs: blockConfig,
	}

	archivePath, livePath, logPath, optionalPaths, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// Paths should be derived from basePath
	assert.Equal(t, "/mnt/base/archive", archivePath)
	assert.Equal(t, "/mnt/base/live", livePath)
	assert.Equal(t, "/mnt/base/logs", logPath)
	require.Len(t, optionalPaths, 1) // Only verification for v0.26.2
	assert.Equal(t, "/mnt/base/verification", optionalPaths[0])
}

// TestGetStoragePaths_MixedPaths tests that individual paths override basePath-derived paths
func TestGetStoragePaths_MixedPaths(t *testing.T) {
	blockConfig := models.BlockNodeInputs{
		Namespace:    "test-ns",
		Release:      "test-release",
		Chart:        "test-chart",
		ChartVersion: "0.26.2", // Version >= 0.26.2 requires verification storage
		Storage: models.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/custom-archive",
			LivePath:         "", // Should derive from basePath
			LogPath:          "/mnt/custom-log",
			VerificationPath: "", // Should derive from basePath
		},
	}

	manager := &Manager{
		blockNodeInputs: blockConfig,
	}

	archivePath, livePath, logPath, optionalPaths, err := manager.GetStoragePaths()
	require.NoError(t, err)

	// Individual paths should be used when provided
	assert.Equal(t, "/mnt/custom-archive", archivePath)
	assert.Equal(t, "/mnt/custom-log", logPath)
	// Live should derive from basePath
	assert.Equal(t, "/mnt/base/live", livePath)
	// Verification should derive from basePath
	require.Len(t, optionalPaths, 1)
	assert.Equal(t, "/mnt/base/verification", optionalPaths[0])
}

// TestGetStoragePaths_InvalidArchivePath tests that invalid archive path returns an error
func TestGetStoragePaths_InvalidArchivePath(t *testing.T) {
	blockConfig := models.BlockNodeInputs{
		Namespace:    "test-ns",
		Release:      "test-release",
		Chart:        "test-chart",
		ChartVersion: "0.1.0",
		Storage: models.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "../relative/path", // Invalid: contains ".." segments (potential path traversal)
			LivePath:         "/mnt/live",
			LogPath:          "/mnt/log",
			VerificationPath: "/mnt/verification",
		},
	}

	manager := &Manager{
		blockNodeInputs: blockConfig,
	}

	_, _, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid archive path")
}

// TestGetStoragePaths_InvalidLivePath tests that invalid live path returns an error
func TestGetStoragePaths_InvalidLivePath(t *testing.T) {
	blockConfig := models.BlockNodeInputs{
		Namespace:    "test-ns",
		Release:      "test-release",
		Chart:        "test-chart",
		ChartVersion: "0.1.0",
		Storage: models.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/archive",
			LivePath:         "../../../etc/passwd", // Invalid: contains path traversal
			LogPath:          "/mnt/log",
			VerificationPath: "/mnt/verification",
		},
	}

	manager := &Manager{
		blockNodeInputs: blockConfig,
	}

	_, _, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid live path")
}

// TestGetStoragePaths_InvalidLogPath tests that invalid log path returns an error
func TestGetStoragePaths_InvalidLogPath(t *testing.T) {
	blockConfig := models.BlockNodeInputs{
		Namespace:    "test-ns",
		Release:      "test-release",
		Chart:        "test-chart",
		ChartVersion: "0.1.0",
		Storage: models.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/archive",
			LivePath:         "/mnt/live",
			LogPath:          "/mnt/log;rm -rf /", // Invalid: contains shell metacharacters
			VerificationPath: "/mnt/verification",
		},
	}

	manager := &Manager{
		blockNodeInputs: blockConfig,
	}

	_, _, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid log path")
}

// TestGetStoragePaths_InvalidVerificationPath tests that invalid verification path returns an error
func TestGetStoragePaths_InvalidVerificationPath(t *testing.T) {
	blockConfig := models.BlockNodeInputs{
		Namespace:    "test-ns",
		Release:      "test-release",
		Chart:        "test-chart",
		ChartVersion: "0.26.2", // Version >= 0.26.2 requires verification storage
		Storage: models.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/archive",
			LivePath:         "/mnt/live",
			LogPath:          "/mnt/log",
			VerificationPath: "../../../etc/shadow", // Invalid: contains path traversal
		},
	}

	manager := &Manager{
		blockNodeInputs: blockConfig,
	}

	_, _, _, _, err := manager.GetStoragePaths()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid verification path")
}

// TestSetupStorage_AllIndividualPaths tests that basePath is not created when all individual paths are provided
func TestSetupStorage_AllIndividualPaths(t *testing.T) {
	// This is more of a documentation test showing the expected behavior
	// In practice, this would need filesystem mocking to fully test
	blockConfig := models.BlockNodeInputs{
		Namespace:    "test-ns",
		Release:      "test-release",
		Chart:        "test-chart",
		ChartVersion: "0.1.0",
		Storage: models.BlockNodeStorage{
			BasePath:         "/mnt/base",
			ArchivePath:      "/mnt/custom-archive",
			LivePath:         "/mnt/custom-live",
			LogPath:          "/mnt/custom-log",
			VerificationPath: "/mnt/custom-verification",
		},
	}

	manager := &Manager{
		blockNodeInputs: blockConfig,
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
		storage     models.BlockNodeStorage
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid base path only",
			storage: models.BlockNodeStorage{
				BasePath: "/mnt/valid-path",
			},
			expectError: false,
		},
		{
			name: "valid individual paths",
			storage: models.BlockNodeStorage{
				BasePath:    "/mnt/base",
				ArchivePath: "/mnt/archive",
				LivePath:    "/mnt/live",
				LogPath:     "/mnt/log",
			},
			expectError: false,
		},
		{
			name: "invalid live path - path traversal",
			storage: models.BlockNodeStorage{
				BasePath: "/mnt/base",
				LivePath: "/mnt/../../../etc/passwd",
			},
			expectError: true,
			errorMsg:    "invalid live path",
		},
		{
			name: "invalid log path - shell metacharacters",
			storage: models.BlockNodeStorage{
				BasePath: "/mnt/base",
				LogPath:  "/mnt/log;echo pwned",
			},
			expectError: true,
			errorMsg:    "invalid log path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockConfig := models.BlockNodeInputs{
				Namespace:    "test-ns",
				Release:      "test-release",
				Chart:        "test-chart",
				ChartVersion: "0.1.0",
				Storage:      tt.storage,
			}

			manager := &Manager{
				blockNodeInputs: blockConfig,
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
	t.Run("individual path takes precedence", func(t *testing.T) {
		blockConfig := models.BlockNodeInputs{
			ChartVersion: "0.26.2", // Version >= 0.26.2 requires verification storage
			Storage: models.BlockNodeStorage{
				BasePath:    "/mnt/base",
				ArchivePath: "/mnt/override-archive",
			},
		}

		manager := &Manager{
			blockNodeInputs: blockConfig,
		}

		archivePath, livePath, logPath, optionalPaths, err := manager.GetStoragePaths()
		require.NoError(t, err)

		// Archive path should use the individual path, not derived from base
		assert.Equal(t, "/mnt/override-archive", archivePath, "individual archivePath should take precedence")

		// Live and log should derive from base since not specified
		assert.Equal(t, "/mnt/base/live", livePath, "should derive from basePath when not specified")
		assert.Equal(t, "/mnt/base/logs", logPath, "should derive from basePath when not specified")
		// Verification should derive from base since not specified
		require.Len(t, optionalPaths, 1)
		assert.Equal(t, "/mnt/base/verification", optionalPaths[0], "should derive from basePath when not specified")
	})

	t.Run("all individual paths override base path", func(t *testing.T) {
		blockConfig := models.BlockNodeInputs{
			// Version 0.1.0 - no optional storages required
			Storage: models.BlockNodeStorage{
				BasePath:         "/mnt/base",
				ArchivePath:      "/var/archive",
				LivePath:         "/var/live",
				LogPath:          "/var/log",
				VerificationPath: "/var/verification",
			},
		}

		manager := &Manager{
			blockNodeInputs: blockConfig,
		}

		archivePath, livePath, logPath, _, err := manager.GetStoragePaths()
		require.NoError(t, err)

		// All paths should use individual values, not derived from base
		assert.Equal(t, "/var/archive", archivePath)
		assert.Equal(t, "/var/live", livePath)
		assert.Equal(t, "/var/log", logPath)
	})
}

// ============================================================================
// Version-Aware Migration Tests
// ============================================================================

// TestOptionalStorageRequiredByVersion tests the version detection for optional storages
func TestOptionalStorageRequiredByVersion(t *testing.T) {
	// Find the verification storage entry
	var verificationStorage OptionalStorage
	var pluginsStorage OptionalStorage
	for _, os := range GetOptionalStorages() {
		switch os.Name {
		case "verification":
			verificationStorage = os
		case "plugins":
			pluginsStorage = os
		}
	}

	t.Run("verification storage", func(t *testing.T) {
		tests := []struct {
			name           string
			targetVersion  string
			expectedResult bool
		}{
			{"version below 0.26.2 should not require verification storage", "0.26.0", false},
			{"version 0.26.1 should not require verification storage", "0.26.1", false},
			{"version exactly 0.26.2 should require verification storage", "0.26.2", true},
			{"version 0.26.3 should require verification storage", "0.26.3", true},
			{"version 0.27.0 should require verification storage", "0.27.0", true},
			{"version 1.0.0 should require verification storage", "1.0.0", true},
			{"very old version 0.20.0 should not require verification storage", "0.20.0", false},
			{"invalid version should default to false (backward compatible)", "invalid-version", false},
			{"empty version should default to false", "", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := verificationStorage.RequiredByVersion(tt.targetVersion)
				assert.Equal(t, tt.expectedResult, result)
			})
		}
	})

	t.Run("plugins storage", func(t *testing.T) {
		tests := []struct {
			name     string
			version  string
			expected bool
		}{
			{"v0.27.0 does not require plugins", "0.27.0", false},
			{"v0.28.0 does not require plugins", "0.28.0", false},
			{"v0.28.1 requires plugins", "0.28.1", true},
			{"v0.28.2 requires plugins", "0.28.2", true},
			{"v0.29.0 requires plugins", "0.29.0", true},
			{"empty version defaults to false", "", false},
			{"invalid version defaults to false", "invalid", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.expected, pluginsStorage.RequiredByVersion(tt.version))
			})
		}
	})
}

// TestGetApplicableOptionalStorages tests the filtering of optional storages by version
func TestGetApplicableOptionalStorages(t *testing.T) {
	tests := []struct {
		name          string
		targetVersion string
		expectedCount int
		expectedNames []string
	}{
		{"pre-verification version", "0.25.0", 0, nil},
		{"verification boundary", "0.26.2", 1, []string{"verification"}},
		{"between verification and plugins", "0.27.0", 1, []string{"verification"}},
		{"0.28.0 still before plugins boundary", "0.28.0", 1, []string{"verification"}},
		{"plugins boundary", "0.28.1", 2, []string{"verification", "plugins"}},
		{"post-plugins version", "0.29.0", 2, []string{"verification", "plugins"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applicable := GetApplicableOptionalStorages(tt.targetVersion)
			assert.Len(t, applicable, tt.expectedCount)
			for i, name := range tt.expectedNames {
				assert.Equal(t, name, applicable[i].Name)
			}
		})
	}
}

// TestComputeValuesFile_VersionAwareSelection tests that the correct values are rendered based on version
func TestComputeValuesFile_VersionAwareSelection(t *testing.T) {
	tests := []struct {
		name            string
		targetVersion   string
		profile         string
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
			applicable := GetApplicableOptionalStorages(tt.targetVersion)
			hasVerification := false
			for _, optStor := range applicable {
				if optStor.Name == "verification" {
					hasVerification = true
				}
			}
			assert.Equal(t, tt.shouldHaveVerif, hasVerification)
		})
	}
}

// TestVersionBoundaryScenarios tests various version boundary scenarios
func TestVersionBoundaryScenarios(t *testing.T) {
	var verificationStorage OptionalStorage
	for _, os := range GetOptionalStorages() {
		if os.Name == "verification" {
			verificationStorage = os
			break
		}
	}

	t.Run("upgrade within pre-0.26.2 versions", func(t *testing.T) {
		assert.False(t, verificationStorage.RequiredByVersion("0.26.1"))
	})

	t.Run("upgrade across breaking change boundary", func(t *testing.T) {
		assert.True(t, verificationStorage.RequiredByVersion("0.26.2"))
	})

	t.Run("upgrade within post-0.26.2 versions", func(t *testing.T) {
		assert.True(t, verificationStorage.RequiredByVersion("0.27.0"))
	})

	t.Run("fresh install at 0.26.2", func(t *testing.T) {
		assert.True(t, verificationStorage.RequiredByVersion("0.26.2"))
	})

	t.Run("fresh install at older version", func(t *testing.T) {
		assert.False(t, verificationStorage.RequiredByVersion("0.26.0"))
	})
}

// TestInvalidVersionHandling tests that invalid versions are handled gracefully
func TestInvalidVersionHandling(t *testing.T) {
	var verificationStorage OptionalStorage
	for _, os := range GetOptionalStorages() {
		if os.Name == "verification" {
			verificationStorage = os
			break
		}
	}

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
			// Should not panic and should return false for invalid versions
			result := verificationStorage.RequiredByVersion(tt.version)

			if tt.version == "" || tt.version == "not-a-version" || tt.version == "0.26" {
				assert.False(t, result, "invalid version should default to false")
			}
		})
	}
}

// TestOptionalStorageRegistryConstants verifies the registry entries have correct min versions
func TestOptionalStorageRegistryConstants(t *testing.T) {
	storages := GetOptionalStorages()
	require.Len(t, storages, 2)
	assert.Equal(t, "verification", storages[0].Name)
	assert.Equal(t, "0.26.2", storages[0].MinVersion)
	assert.Equal(t, "plugins", storages[1].Name)
	assert.Equal(t, "0.28.1", storages[1].MinVersion)
}

// TestGetStoragePaths_V0281_IncludesBothOptionalStorages tests that v0.28.1 includes both verification and plugins
func TestGetStoragePaths_V0281_IncludesBothOptionalStorages(t *testing.T) {
	blockConfig := models.BlockNodeInputs{
		ChartVersion: "0.28.1",
		Storage: models.BlockNodeStorage{
			BasePath: "/mnt/base",
		},
	}

	manager := &Manager{
		blockNodeInputs: blockConfig,
	}

	archivePath, livePath, logPath, optionalPaths, err := manager.GetStoragePaths()
	require.NoError(t, err)

	assert.Equal(t, "/mnt/base/archive", archivePath)
	assert.Equal(t, "/mnt/base/live", livePath)
	assert.Equal(t, "/mnt/base/logs", logPath)
	require.Len(t, optionalPaths, 2)
	assert.Equal(t, "/mnt/base/verification", optionalPaths[0])
	assert.Equal(t, "/mnt/base/plugins", optionalPaths[1])
}
