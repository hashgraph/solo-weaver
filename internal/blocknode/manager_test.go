// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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
			{"version 0.35.1 (current default chart) requires verification storage", "0.35.1", true},
			{"version 0.36.0 requires verification storage", "0.36.0", true},
			{"version 0.36.5 requires verification storage", "0.36.5", true},
			{"version exactly 0.37.0 NOT required (retirement boundary)", "0.37.0", false},
			{"version 0.37.5 should NOT require verification storage (retired)", "0.37.5", false},
			{"version 1.0.0 should NOT require verification storage (retired)", "1.0.0", false},
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
		{"pre-application-state at 0.35.0 has verification + plugins only", "0.35.0", 2, []string{"verification", "plugins"}},
		{"0.35.1 still has verification + plugins only (app-state not yet)", "0.35.1", 2, []string{"verification", "plugins"}},
		{"0.36.0 still has verification + plugins only", "0.36.0", 2, []string{"verification", "plugins"}},
		{"0.36.5 still has verification + plugins only", "0.36.5", 2, []string{"verification", "plugins"}},
		{"0.37.0 retires verification, introduces application-state", "0.37.0", 2, []string{"plugins", "application-state"}},
		{"1.0.0", "1.0.0", 2, []string{"plugins", "application-state"}},
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
		{
			name:            "v0.36.0 still has verification (retires only at 0.37.0)",
			targetVersion:   "0.36.0",
			profile:         "full",
			shouldHaveVerif: true,
		},
		{
			name:            "v0.37.0 retires verification (application-state replaces it)",
			targetVersion:   "0.37.0",
			profile:         "full",
			shouldHaveVerif: false,
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

	t.Run("verification retired at MaxVersion boundary", func(t *testing.T) {
		assert.False(t, verificationStorage.RequiredByVersion(BlockNodeVerificationRetirementVersion))
	})

	t.Run("verification still required across the entire 0.36.x branch", func(t *testing.T) {
		assert.True(t, verificationStorage.RequiredByVersion("0.36.0-rc.0"))
		assert.True(t, verificationStorage.RequiredByVersion("0.36.0-rc.2"))
		assert.True(t, verificationStorage.RequiredByVersion("0.36.0"))
		assert.True(t, verificationStorage.RequiredByVersion("0.36.5"))
	})

	t.Run("verification retired for any version >= 0.37.0", func(t *testing.T) {
		assert.False(t, verificationStorage.RequiredByVersion("0.37.0"))
		assert.False(t, verificationStorage.RequiredByVersion("0.37.5"))
		assert.False(t, verificationStorage.RequiredByVersion("1.0.0"))
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

// TestOptionalStorageRegistryConstants verifies the registry entries have correct min/max versions
func TestOptionalStorageRegistryConstants(t *testing.T) {
	storages := GetOptionalStorages()
	require.Len(t, storages, 3)
	assert.Equal(t, "verification", storages[0].Name)
	assert.Equal(t, "0.26.2", storages[0].MinVersion)
	assert.Equal(t, BlockNodeVerificationRetirementVersion, storages[0].MaxVersion,
		"verification is retired at 0.37.0")
	assert.Equal(t, "plugins", storages[1].Name)
	assert.Equal(t, "0.28.1", storages[1].MinVersion)
	assert.Empty(t, storages[1].MaxVersion, "plugins has no upper bound")
	assert.Equal(t, "application-state", storages[2].Name)
	assert.Equal(t, "applicationState", storages[2].PersistenceKey,
		"application-state's chart persistence key is camelCase 'applicationState', not the kebab-case Name")
	assert.Empty(t, storages[0].PersistenceKey, "verification chart key matches Name (no override needed)")
	assert.Empty(t, storages[1].PersistenceKey, "plugins chart key matches Name (no override needed)")
	assert.Equal(t, BlockNodeApplicationStateRequiredVersion, storages[2].MinVersion,
		"application-state is introduced at 0.37.0 in lockstep with verification retirement")
	assert.Empty(t, storages[2].MaxVersion, "application-state has no upper bound")
}

// TestPrereleaseCutoverBoundary pins the 0.37.0 cutover behaviour across release
// candidates. The "-0" prerelease floor on the registry constants is what makes
// a prerelease like 0.37.0-rc1 satisfy the >= 0.37.0 boundary — semver ranks
// a prerelease BELOW its final tag, so a bare "0.37.0" min would wrongly exclude
// every 0.37.0-rcN, skipping application-state and keeping verification alive.
func TestPrereleaseCutoverBoundary(t *testing.T) {
	var appState, verification OptionalStorage
	for _, os := range GetOptionalStorages() {
		switch os.Name {
		case "application-state":
			appState = os
		case "verification":
			verification = os
		}
	}

	// At and beyond the cutover (including its release candidates): application
	// state ON, verification retired — identical to the final 0.37.0 tag.
	for _, v := range []string{"0.37.0-rc1", "0.37.0-rc2", "0.37.0", "0.37.1-rc1", "0.37.1"} {
		assert.Truef(t, appState.RequiredByVersion(v), "application-state must be required at %s", v)
		assert.Falsef(t, verification.RequiredByVersion(v), "verification must be retired at %s", v)

		names := make([]string, 0)
		for _, o := range GetApplicableOptionalStorages(v) {
			names = append(names, o.Name)
		}
		assert.Equalf(t, []string{"plugins", "application-state"}, names, "applicable storages at %s", v)
	}

	// Just below the cutover (including the last 0.36 release candidate): the
	// pre-0.37 layout still holds — verification ON, application-state OFF.
	for _, v := range []string{"0.36.0-rc1", "0.36.0", "0.36.5"} {
		assert.Falsef(t, appState.RequiredByVersion(v), "application-state must NOT be required at %s", v)
		assert.Truef(t, verification.RequiredByVersion(v), "verification must still be required at %s", v)
	}
}

// TestInjectPersistenceOverrides_ApplicationStateUsesChartKey guards the bug
// where weaver wrote blockNode.persistence under the kebab-case Name
// ("application-state") or the retired "applicationStateFacility" key. The chart
// reads neither, so its StatefulSet fell back to a volumeClaimTemplate and the
// generated PVC stayed Pending forever, timing out the atomic helm install.
func TestInjectPersistenceOverrides_ApplicationStateUsesChartKey(t *testing.T) {
	m := &Manager{
		blockNodeInputs: models.BlockNodeInputs{ChartVersion: "0.37.0"},
		logger:          testLogger(),
	}

	out, err := m.injectPersistenceOverrides([]byte("blockNode:\n  persistence: {}\n"))
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(out, &vals))
	blockNode, ok := vals["blockNode"].(map[string]interface{})
	require.True(t, ok)
	persistence, ok := blockNode["persistence"].(map[string]interface{})
	require.True(t, ok)

	appState, ok := persistence["applicationState"].(map[string]interface{})
	require.True(t, ok, "persistence must use the chart key 'applicationState'")
	assert.Equal(t, "application-state-storage-pvc", appState["existingClaim"])
	assert.Equal(t, false, appState["create"])

	_, hasKebab := persistence["application-state"]
	assert.False(t, hasKebab, "must not key persistence by the kebab-case Name 'application-state'")
	_, hasFacility := persistence["applicationStateFacility"]
	assert.False(t, hasFacility, "must not key persistence by the retired 'applicationStateFacility'")
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

// TestInjectRetentionConfig_BothValues tests that both retention thresholds are injected.
func TestInjectRetentionConfig_BothValues(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			HistoricRetention: "500000",
			RecentRetention:   "48000",
		},
		logger: testLogger(),
	}

	input := []byte(`blockNode:
  config:
    BLOCK_NODE_EARLIEST_MANAGED_BLOCK: "100000000"
`)

	result, err := manager.injectRetentionConfig(input)
	require.NoError(t, err)

	resultStr := string(result)
	assert.Contains(t, resultStr, "FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD")
	assert.Contains(t, resultStr, "500000")
	assert.Contains(t, resultStr, "FILES_RECENT_BLOCK_RETENTION_THRESHOLD")
	assert.Contains(t, resultStr, "48000")
	// Original key should be preserved
	assert.Contains(t, resultStr, "BLOCK_NODE_EARLIEST_MANAGED_BLOCK")
}

// TestInjectRetentionConfig_OnlyHistoric tests that only historic retention is injected.
func TestInjectRetentionConfig_OnlyHistoric(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			HistoricRetention: "1000000",
		},
		logger: testLogger(),
	}

	input := []byte(`blockNode:
  config:
    BLOCK_NODE_EARLIEST_MANAGED_BLOCK: "100000000"
`)

	result, err := manager.injectRetentionConfig(input)
	require.NoError(t, err)

	resultStr := string(result)
	assert.Contains(t, resultStr, "FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD")
	assert.Contains(t, resultStr, "1000000")
	assert.NotContains(t, resultStr, "FILES_RECENT_BLOCK_RETENTION_THRESHOLD")
}

// TestInjectRetentionConfig_OnlyRecent tests that only recent retention is injected.
func TestInjectRetentionConfig_OnlyRecent(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			RecentRetention: "48000",
		},
		logger: testLogger(),
	}

	input := []byte(`blockNode:
  config:
    BLOCK_NODE_EARLIEST_MANAGED_BLOCK: "100000000"
`)

	result, err := manager.injectRetentionConfig(input)
	require.NoError(t, err)

	resultStr := string(result)
	assert.NotContains(t, resultStr, "FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD")
	assert.Contains(t, resultStr, "FILES_RECENT_BLOCK_RETENTION_THRESHOLD")
	assert.Contains(t, resultStr, "48000")
}

// TestInjectRetentionConfig_NoRetention tests that nothing is injected when no retention is set.
func TestInjectRetentionConfig_NoRetention(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{},
		logger:          testLogger(),
	}

	input := []byte(`blockNode:
  config:
    BLOCK_NODE_EARLIEST_MANAGED_BLOCK: "100000000"
`)

	result, err := manager.injectRetentionConfig(input)
	require.NoError(t, err)

	// Should be returned as-is (byte-identical)
	assert.Equal(t, input, result)
}

// TestInjectRetentionConfig_CreatesConfigSection tests injection when blockNode.config is absent.
func TestInjectRetentionConfig_CreatesConfigSection(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			HistoricRetention: "0",
			RecentRetention:   "96000",
		},
		logger: testLogger(),
	}

	input := []byte(`service:
  type: LoadBalancer
`)

	result, err := manager.injectRetentionConfig(input)
	require.NoError(t, err)

	resultStr := string(result)
	assert.Contains(t, resultStr, "FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD")
	assert.Contains(t, resultStr, "FILES_RECENT_BLOCK_RETENTION_THRESHOLD")
	assert.Contains(t, resultStr, "blockNode")
}

// TestInjectRetentionConfig_OverridesExistingValues tests that existing retention values are overridden.
func TestInjectRetentionConfig_OverridesExistingValues(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			HistoricRetention: "250000",
			RecentRetention:   "50000",
		},
		logger: testLogger(),
	}

	input := []byte(`blockNode:
  config:
    FILES_HISTORIC_BLOCK_RETENTION_THRESHOLD: "0"
    FILES_RECENT_BLOCK_RETENTION_THRESHOLD: "96000"
`)

	result, err := manager.injectRetentionConfig(input)
	require.NoError(t, err)

	resultStr := string(result)
	assert.Contains(t, resultStr, "250000")
	assert.Contains(t, resultStr, "50000")
	assert.NotContains(t, resultStr, `"0"`)
	assert.NotContains(t, resultStr, `"96000"`)
}

// ── injectServiceAnnotations ──────────────────────────────────────────────────

// TestInjectServiceAnnotations_Disabled tests that nothing is injected when LoadBalancerEnabled is false.
func TestInjectServiceAnnotations_Disabled(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: false,
		},
		logger: testLogger(),
	}

	input := []byte(`blockNode:
  config: {}
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	// Content must be returned byte-identical — nothing was parsed or rewritten.
	assert.Equal(t, input, result)
	assert.NotContains(t, string(result), "metallb.io/address-pool")
}

// TestInjectServiceAnnotations_InjectsWhenAbsent tests that the annotation is injected
// when LoadBalancerEnabled is true and no annotation is present in the values file.
func TestInjectServiceAnnotations_InjectsWhenAbsent(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: true,
		},
		logger: testLogger(),
	}

	input := []byte(`blockNode:
  config: {}
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	resultStr := string(result)
	assert.Contains(t, resultStr, "metallb.io/address-pool")
	assert.Contains(t, resultStr, "public-address-pool")
}

// TestInjectServiceAnnotations_CreatesServiceAndAnnotationsPath tests that the service and
// annotations keys are created from scratch when absent.
func TestInjectServiceAnnotations_CreatesServiceAndAnnotationsPath(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: true,
		},
		logger: testLogger(),
	}

	// Minimal values file with no service section at all.
	input := []byte(`blockNode:
  config: {}
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))

	service, ok := vals["service"].(map[string]interface{})
	require.True(t, ok, "service key should be created")

	annotations, ok := service["annotations"].(map[string]interface{})
	require.True(t, ok, "service.annotations key should be created")

	assert.Equal(t, "public-address-pool", annotations["metallb.io/address-pool"])
}

// TestInjectServiceAnnotations_PreservesExistingAnnotation tests that a pre-existing
// metallb.io/address-pool value in the operator's values file is not clobbered. The
// input also pre-sets service.type so the byte-identical assertion is meaningful —
// nothing needs to mutate.
func TestInjectServiceAnnotations_PreservesExistingAnnotation(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: true,
		},
		logger: testLogger(),
	}

	input := []byte(`service:
  type: LoadBalancer
  annotations:
    metallb.io/address-pool: my-custom-pool
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	// Both fields already correct → byte-identical short-circuit, no rewrite.
	assert.Equal(t, input, result)
	assert.Contains(t, string(result), "my-custom-pool")
	assert.NotContains(t, string(result), "public-address-pool")
}

// TestInjectServiceAnnotations_PreservesOtherAnnotations tests that sibling annotations
// in the operator's values file are left untouched when the metallb key is absent.
func TestInjectServiceAnnotations_PreservesOtherAnnotations(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: true,
		},
		logger: testLogger(),
	}

	input := []byte(`service:
  type: LoadBalancer
  annotations:
    custom.io/tag: my-tag
    another.io/key: some-value
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))

	service := vals["service"].(map[string]interface{})
	annotations := service["annotations"].(map[string]interface{})

	// MetalLB annotation injected.
	assert.Equal(t, "public-address-pool", annotations["metallb.io/address-pool"])
	// Sibling annotations preserved.
	assert.Equal(t, "my-tag", annotations["custom.io/tag"])
	assert.Equal(t, "some-value", annotations["another.io/key"])
}

// TestInjectServiceAnnotations_SetsLoadBalancerType tests that service.type: LoadBalancer
// is injected when LoadBalancerEnabled is true and the operator left the type unset.
// This is the defense-in-depth fix for #702: even if the values pipeline ever produced
// a values document missing service.type, this step would restore it.
func TestInjectServiceAnnotations_SetsLoadBalancerType(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: true,
		},
		logger: testLogger(),
	}

	input := []byte(`service:
  port: 40840
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))

	service := vals["service"].(map[string]interface{})
	assert.Equal(t, "LoadBalancer", service["type"])
	// Port preserved.
	assert.EqualValues(t, 40840, service["port"])
}

// TestInjectServiceAnnotations_PreservesExistingType tests that an explicit non-LoadBalancer
// service.type set by the operator is not clobbered. Weaver warns about the mismatch but
// honors the operator's choice — clobbering would silently override intent.
func TestInjectServiceAnnotations_PreservesExistingType(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: true,
		},
		logger: testLogger(),
	}

	input := []byte(`service:
  type: ClusterIP
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))

	service := vals["service"].(map[string]interface{})
	assert.Equal(t, "ClusterIP", service["type"], "operator-set service.type must not be clobbered")
}

// TestInjectServiceAnnotations_ChartOwnedLoadBalancerDefers is issue #900, case 3 (the
// "split topology"): service.type: ClusterIP + the chart's own loadBalancer.enabled: true.
// The chart owns the "-external" LoadBalancer Service and its annotations, so weaver must
// defer entirely — no warning, no MetalLB annotation injected onto the ClusterIP service —
// and return the values byte-identical.
func TestInjectServiceAnnotations_ChartOwnedLoadBalancerDefers(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: true,
		},
		logger: testLogger(),
	}

	input := []byte(`service:
  type: ClusterIP
  port: 40840
loadBalancer:
  enabled: true
  annotations:
    metallb.io/address-pool: "public-address-pool"
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	// Deferred to the chart → byte-identical, no service.* mutation.
	assert.Equal(t, input, result)

	// The MetalLB annotation stays under loadBalancer.annotations (chart-owned); weaver must
	// not have injected one onto service.annotations.
	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))
	service := vals["service"].(map[string]interface{})
	_, hasServiceAnnotations := service["annotations"]
	assert.False(t, hasServiceAnnotations, "weaver must not inject service.annotations when the chart owns the LoadBalancer")
	assert.Equal(t, "ClusterIP", service["type"], "operator service.type must be left untouched")
}

// TestInjectServiceAnnotations_ChartOwnedLoadBalancerNoOperatorAnnotation covers case 3 when
// the operator enables loadBalancer but has not (yet) set loadBalancer.annotations. Weaver
// still defers — it must not "helpfully" inject onto service.annotations, since that tag would
// be inert on the ClusterIP service and the real tag belongs under loadBalancer.annotations.
func TestInjectServiceAnnotations_ChartOwnedLoadBalancerNoOperatorAnnotation(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: true,
		},
		logger: testLogger(),
	}

	input := []byte(`service:
  type: ClusterIP
loadBalancer:
  enabled: true
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	assert.Equal(t, input, result)
	assert.NotContains(t, string(result), "metallb.io/address-pool")
}

// TestInjectServiceAnnotations_ChartOwnedLoadBalancerQuotedBoolDefers covers issue #900 case 3
// when the operator quotes the flag (loadBalancer.enabled: "true"). Helm treats that quoted
// string as enabled, so weaver must too — a bool-only gate would misread it as disabled and
// re-introduce the misleading warning + inert service.annotations injection. Weaver defers and
// returns the values byte-identical, exactly as for the unquoted bool.
func TestInjectServiceAnnotations_ChartOwnedLoadBalancerQuotedBoolDefers(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: true,
		},
		logger: testLogger(),
	}

	input := []byte(`service:
  type: ClusterIP
  port: 40840
loadBalancer:
  enabled: "true"
  annotations:
    metallb.io/address-pool: "public-address-pool"
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	// Deferred to the chart → byte-identical, no service.* mutation.
	assert.Equal(t, input, result)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))
	service := vals["service"].(map[string]interface{})
	_, hasServiceAnnotations := service["annotations"]
	assert.False(t, hasServiceAnnotations, "quoted loadBalancer.enabled must still defer to the chart")
	assert.Equal(t, "ClusterIP", service["type"], "operator service.type must be left untouched")
}

// TestInjectServiceAnnotations_QuotedFalseNotChartOwned pins the other end of the string parse:
// loadBalancer.enabled: "false" reads operator intent (off), not Helm's "any non-empty string is
// truthy" quirk. That is not the chart-owned split topology, so weaver keeps its default behavior
// and still injects the MetalLB annotation onto the main service.
func TestInjectServiceAnnotations_QuotedFalseNotChartOwned(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: true,
		},
		logger: testLogger(),
	}

	input := []byte(`service:
  type: LoadBalancer
loadBalancer:
  enabled: "false"
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))
	service := vals["service"].(map[string]interface{})
	annotations := service["annotations"].(map[string]interface{})
	assert.Equal(t, "public-address-pool", annotations["metallb.io/address-pool"],
		`loadBalancer.enabled: "false" reads as off; weaver must still inject the MetalLB annotation`)
}

// TestInjectServiceAnnotations_LoadBalancerDisabledStillInjects is the non-regression guard
// for cases 1/2/4 (issue #900): loadBalancer.enabled: false is NOT the chart-owned split
// topology, so weaver keeps its default behavior and injects the MetalLB annotation onto the
// main service. This pins that the new gate keys off loadBalancer.enabled being true, not
// merely present.
func TestInjectServiceAnnotations_LoadBalancerDisabledStillInjects(t *testing.T) {
	manager := &Manager{
		blockNodeInputs: models.BlockNodeInputs{
			LoadBalancerEnabled: true,
		},
		logger: testLogger(),
	}

	input := []byte(`service:
  type: LoadBalancer
loadBalancer:
  enabled: false
`)

	result, err := manager.injectServiceAnnotations(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))
	service := vals["service"].(map[string]interface{})
	annotations := service["annotations"].(map[string]interface{})
	assert.Equal(t, "public-address-pool", annotations["metallb.io/address-pool"],
		"loadBalancer.enabled=false is not chart-owned; weaver must still inject the MetalLB annotation")
}

// TestMergeValues_OperatorWinsOnScalar verifies the deep-merge contract: operator scalars
// override base scalars at the same path.
func TestMergeValues_OperatorWinsOnScalar(t *testing.T) {
	base := []byte(`service:
  type: LoadBalancer
  port: 40840
`)
	operator := []byte(`service:
  port: 12345
`)

	merged, err := mergeValues(base, operator)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(merged, &vals))

	service := vals["service"].(map[string]interface{})
	assert.EqualValues(t, 12345, service["port"], "operator port should win")
	assert.Equal(t, "LoadBalancer", service["type"], "base service.type should survive — operator didn't set it")
}

// TestMergeValues_BaseSurvivesWhereOperatorSilent verifies the central #702 invariant:
// when an operator file omits service.type entirely, the base's service.type: LoadBalancer
// survives the merge. Pre-fix this is the value that disappeared and caused
// verify-block-node-reachable to fail.
func TestMergeValues_BaseSurvivesWhereOperatorSilent(t *testing.T) {
	base := []byte(`service:
  type: LoadBalancer
  port: 40840
blockNode:
  config:
    BLOCK_NODE_EARLIEST_MANAGED_BLOCK: "100000000"
`)
	operator := []byte(`service:
  annotations:
    metallb.io/allow-shared-ip: shared
`)

	merged, err := mergeValues(base, operator)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(merged, &vals))

	service := vals["service"].(map[string]interface{})
	assert.Equal(t, "LoadBalancer", service["type"], "base service.type: LoadBalancer must survive — this is the #702 bug")
	assert.EqualValues(t, 40840, service["port"], "base service.port must survive")

	annotations := service["annotations"].(map[string]interface{})
	assert.Equal(t, "shared", annotations["metallb.io/allow-shared-ip"], "operator annotation merged in")

	blockNode := vals["blockNode"].(map[string]interface{})
	config := blockNode["config"].(map[string]interface{})
	assert.Equal(t, "100000000", config["BLOCK_NODE_EARLIEST_MANAGED_BLOCK"], "base blockNode.config must survive")
}

// TestMergeValues_DeepMergeNestedMaps verifies that nested maps deep-merge (not replace):
// adding a sibling key under blockNode.config keeps the base's existing keys at the same path.
func TestMergeValues_DeepMergeNestedMaps(t *testing.T) {
	base := []byte(`blockNode:
  config:
    BLOCK_NODE_EARLIEST_MANAGED_BLOCK: "100000000"
    MESSAGING_BLOCK_NOTIFICATION_QUEUE_SIZE: "512"
`)
	operator := []byte(`blockNode:
  config:
    CUSTOM_OPERATOR_KEY: "custom-value"
`)

	merged, err := mergeValues(base, operator)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(merged, &vals))

	config := vals["blockNode"].(map[string]interface{})["config"].(map[string]interface{})
	assert.Equal(t, "100000000", config["BLOCK_NODE_EARLIEST_MANAGED_BLOCK"], "base config keys preserved")
	assert.Equal(t, "512", config["MESSAGING_BLOCK_NOTIFICATION_QUEUE_SIZE"], "base config keys preserved")
	assert.Equal(t, "custom-value", config["CUSTOM_OPERATOR_KEY"], "operator config keys merged in")
}

// TestMergeValues_ListsReplaceNotConcatenate pins down helm's documented merge contract:
// sequences are replaced wholesale by the operator (no element-wise concatenation). The
// merge primitive's behavior on lists is the part most likely to surprise operators and
// the part most likely to drift silently if the merge implementation is ever swapped, so
// it deserves its own regression test.
func TestMergeValues_ListsReplaceNotConcatenate(t *testing.T) {
	base := []byte(`blockNode:
  initContainers:
    - name: init-storage-dirs
      image: docker.io/library/busybox:latest
    - name: init-config
      image: docker.io/library/busybox:latest
`)
	operator := []byte(`blockNode:
  initContainers:
    - name: operator-init
      image: my.registry/operator:latest
`)

	merged, err := mergeValues(base, operator)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(merged, &vals))

	blockNode := vals["blockNode"].(map[string]interface{})
	initContainers, ok := blockNode["initContainers"].([]interface{})
	require.True(t, ok, "blockNode.initContainers should still be a sequence")

	require.Len(t, initContainers, 1, "operator's sequence must replace the base's wholesale (no concat)")
	first := initContainers[0].(map[string]interface{})
	assert.Equal(t, "operator-init", first["name"], "operator's element survives; base's elements are dropped")
}

// TestMergeValues_EmptyOperator returns the base verbatim (semantically — keys all present).
func TestMergeValues_EmptyOperator(t *testing.T) {
	base := []byte(`service:
  type: LoadBalancer
  port: 40840
`)
	operator := []byte(``)

	merged, err := mergeValues(base, operator)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(merged, &vals))

	service := vals["service"].(map[string]interface{})
	assert.Equal(t, "LoadBalancer", service["type"])
	assert.EqualValues(t, 40840, service["port"])
}
