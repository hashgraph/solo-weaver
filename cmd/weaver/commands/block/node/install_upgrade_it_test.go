// SPDX-License-Identifier: Apache-2.0

//go:build integration

package node

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFlagPrecedence_ChartConfiguration tests that command-line flags override config file values
// for chart-related settings (version, repo, namespace, release name)
func TestFlagPrecedence_ChartConfiguration(t *testing.T) {
	serial(t) // Enforce sequential execution due to shared flag variables

	cmd := testutil.PrepareSubCmdForTest(GetCmd())
	resetFlags(cmd)

	testutil.Reset(t)

	// Create a test config file with specific values
	configContent := `
log:
  level: debug
  consoleLogging: true
  fileLogging: false
blockNode:
  namespace: "config-namespace"
  release: "config-release"
  chart: "oci://config.example.com/chart"
  version: "0.22.1"
  storage:
    basePath: "/mnt/config-storage"
`
	tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
	err := os.WriteFile(tmpConfigFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Initialize config from file
	err = config.Initialize(tmpConfigFile)
	require.NoError(t, err)

	// Verify config file values are loaded
	cfg := config.Get()
	assert.Equal(t, "config-namespace", cfg.BlockNode.Namespace)
	assert.Equal(t, "config-release", cfg.BlockNode.Release)
	assert.Equal(t, "oci://config.example.com/chart", cfg.BlockNode.ChartUrl)
	assert.Equal(t, "0.22.1", cfg.BlockNode.Version)
	assert.Equal(t, "/mnt/config-storage", cfg.BlockNode.Storage.BasePath)

	// Simulate flag values
	flagProfile = "local"
	flagNamespace = "flag-namespace"
	flagReleaseName = "flag-release"
	flagChartRepo = "oci://flag.example.com/chart"
	flagChartVersion = "0.24.0"
	flagBasePath = "/mnt/flag-storage"

	// Verify flags override config file values
	inputs, err := prepareUserInputs(cmd, []string{})
	require.NoError(t, err)
	blockNodeInputs := inputs.Custom

	assert.Equal(t, "flag-namespace", blockNodeInputs.Namespace, "flag should override config namespace")
	assert.Equal(t, "flag-release", blockNodeInputs.Release, "flag should override config release")
	assert.Equal(t, "oci://flag.example.com/chart", blockNodeInputs.ChartUrl, "flag should override config chart")
	assert.Equal(t, "0.24.0", blockNodeInputs.ChartVersion, "flag should override config version")
	assert.Equal(t, "/mnt/flag-storage", blockNodeInputs.Storage.BasePath, "flag should override config basePath")
}

// TestFlagPrecedence_PartialOverride tests that only specified flags override config values
// while unspecified flags leave config values intact
func TestFlagPrecedence_PartialOverride(t *testing.T) {
	serial(t) // Enforce sequential execution due to shared flag variables

	cmd := testutil.PrepareSubCmdForTest(GetCmd())
	resetFlags(cmd)

	testutil.Reset(t)

	// Create a test config file
	configContent := `
blockNode:
  namespace: "config-namespace"
  release: "config-release"
  chart: "oci://config.example.com/chart"
  version: "0.20.0"
  storage:
    basePath: "/mnt/config-storage"
`
	tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
	err := os.WriteFile(tmpConfigFile, []byte(configContent), 0644)
	require.NoError(t, err)

	err = config.Initialize(tmpConfigFile)
	require.NoError(t, err)

	// Only set some flags (not all)
	flagProfile = "local"
	flagNamespace = "flag-namespace"
	flagChartVersion = "0.35.0"
	// Leave other flags empty

	inputs, err := prepareUserInputs(cmd, []string{})
	require.NoError(t, err)
	blockNodeInputs := inputs.Custom

	// Overridden values
	assert.Equal(t, "flag-namespace", blockNodeInputs.Namespace, "specified flag should override")
	assert.Equal(t, "0.35.0", blockNodeInputs.ChartVersion, "specified flag should override")

	// Unchanged values (flags were empty, so config values remain)
	assert.Equal(t, "config-release", blockNodeInputs.Release, "unspecified flag should keep config value")
	assert.Equal(t, "oci://config.example.com/chart", blockNodeInputs.ChartUrl, "unspecified flag should keep config value")
	assert.Equal(t, "/mnt/config-storage", blockNodeInputs.Storage.BasePath, "unspecified flag should keep config value")
}

// TestFlagPrecedence_StoragePaths tests precedence for storage path configurations
func TestFlagPrecedence_StoragePaths(t *testing.T) {
	serial(t) // Enforce sequential execution due to shared flag variables

	cmd := testutil.PrepareSubCmdForTest(GetCmd())
	resetFlags(cmd)

	testutil.Reset(t)

	// Create a test config file
	configContent := `
blockNode:
  namespace: "block-node"
  release: "block-node"
  chart: "oci://example.com/chart"
  version: "0.20.0"
  storage:
    basePath: "/mnt/config-base"
    archivePath: "/mnt/config-archive"
    livePath: "/mnt/config-live"
    logPath: "/mnt/config-log"
`
	tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
	err := os.WriteFile(tmpConfigFile, []byte(configContent), 0644)
	require.NoError(t, err)

	err = config.Initialize(tmpConfigFile)
	require.NoError(t, err)

	// Set flag values
	flagProfile = "local"
	flagBasePath = "/mnt/flag-base"
	flagArchivePath = "/mnt/flag-archive"
	flagLivePath = "/mnt/flag-live"
	flagLogPath = "/mnt/flag-log"

	inputs, err := prepareUserInputs(installCmd, []string{})
	require.NoError(t, err)
	blockNodeInputs := inputs.Custom

	assert.Equal(t, "/mnt/flag-base", blockNodeInputs.Storage.BasePath, "flag should override config basePath")
	assert.Equal(t, "/mnt/flag-archive", blockNodeInputs.Storage.ArchivePath, "flag should override config archivePath")
	assert.Equal(t, "/mnt/flag-live", blockNodeInputs.Storage.LivePath, "flag should override config livePath")
	assert.Equal(t, "/mnt/flag-log", blockNodeInputs.Storage.LogPath, "flag should override config logPath")
}

// TestFlagPrecedence_IndividualPathsOverBasePath tests that individual paths take precedence over basePath
func TestFlagPrecedence_IndividualPathsOverBasePath(t *testing.T) {
	serial(t) // Enforce sequential execution due to shared flag variables

	cmd := testutil.PrepareSubCmdForTest(GetCmd())
	resetFlags(cmd)

	testutil.Reset(t)

	// Create a test config file with only basePath
	configContent := `
blockNode:
  namespace: "block-node"
  release: "block-node"
  chart: "oci://example.com/chart"
  version: "0.20.0"
  storage:
    basePath: "/mnt/base-storage"
`
	tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
	err := os.WriteFile(tmpConfigFile, []byte(configContent), 0644)
	require.NoError(t, err)

	err = config.Initialize(tmpConfigFile)
	require.NoError(t, err)

	// Set individual path flags (these should override derived paths from basePath)
	flagProfile = "local"
	flagArchivePath = "/mnt/custom-archive"
	flagLivePath = "/mnt/custom-live"
	// Don't set logPath - it should derive from basePath

	inputs, err := prepareUserInputs(installCmd, []string{})
	require.NoError(t, err)
	blockNodeInputs := inputs.Custom

	assert.Equal(t, "/mnt/base-storage", blockNodeInputs.Storage.BasePath)
	assert.Equal(t, "/mnt/custom-archive", blockNodeInputs.Storage.ArchivePath, "individual archive path should be set")
	assert.Equal(t, "/mnt/custom-live", blockNodeInputs.Storage.LivePath, "individual live path should be set")
	assert.Equal(t, "", blockNodeInputs.Storage.LogPath, "logPath not set by flag, should be empty in config")
}

// TestHelmLifecycle_InstallAndUpgradeWithValueReuse tests a complete installation flow with flag overrides
// and multiple upgrade scenarios with different value reuse behaviors
func TestHelmLifecycle_InstallAndUpgradeWithValueReuse(t *testing.T) {
	serial(t) // Enforce sequential execution due to shared flag variables

	testutil.Reset(t)

	// Sets up registry proxy for CRI-O
	err := testutil.InstallCrioRegistriesConf()
	require.NoError(t, err)

	// Create a test config file
	configContent := `
blockNode:
  namespace: "default-ns"
  release: "default-release"
  chart: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
  version: "0.22.1"
  storage:
    basePath: "/mnt/fast-storage"
`
	tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
	err = os.WriteFile(tmpConfigFile, []byte(configContent), 0644)
	require.NoError(t, err)

	err = config.Initialize(tmpConfigFile)
	require.NoError(t, err)

	const namespace = "custom-namespace"
	const releaseName = "custom-release"

	basePath := filepath.Join(t.TempDir(), "mnt", "base-storage")
	logPath := filepath.Join(basePath, "logs")
	livePath := filepath.Join(basePath, "live")
	archivePath := filepath.Join(t.TempDir(), "mnt", "special-archive")

	t.Run("install_with_flag_overrides_and_mixed_paths", func(t *testing.T) {
		cmd := testutil.PrepareSubCmdForTest(GetCmd())

		resetFlags(cmd)

		// absolute path for blocknode_values.yaml
		relativeValuesPath := "../../../../../test/config/blocknode_values.yaml"
		absValuesPath, err := filepath.Abs(relativeValuesPath)
		require.NoError(t, err, "failed to get absolute path for values file")

		// Test with flag overrides
		cmd.SetArgs([]string{
			"node",
			"install",
			"--profile=local",
			"--values=" + absValuesPath,
			"--namespace=" + namespace,
			"--release-name=" + releaseName,
			"--chart-version=0.24.0",
			"--base-path=" + basePath,
			"--archive-path=" + archivePath,
			"--archive-size=6Gi",
		})

		err = cmd.Execute()
		require.NoError(t, err)

		// Verify runtime state was updated
		//err = runtime.BlockNode().RefreshState(context.Background())
		//require.NoError(t, err, "failed to refresh block node runtime state")
		//current := runtime.BlockNode().CurrentState()
		//assert.Equal(t, namespace, current.Namespace)
		//assert.Equal(t, releaseName, current.Release)
		//assert.Equal(t, "0.24.0", current.ChartVersion)
		//assert.Equal(t, basePath, current.Storage.BasePath)
		//assert.Empty(t, current.Storage.LivePath)
		//assert.Empty(t, current.Storage.LogPath)
		//assert.Equal(t, archivePath, current.Storage.ArchivePath)

		// Check existence of storage directories
		assert.DirExists(t, archivePath, "archivePath directory should exist")
		assert.DirExists(t, livePath, "livePath directory should exist")
		assert.DirExists(t, logPath, "logPath directory should exist")

		// Verify PV and PVC storage sizes
		// Live storage should be 5Gi (default)
		assert.Equal(t, "5Gi", getPVStorageSize(t, "live-storage-pv"), "live-storage-pv should have 5Gi capacity")
		assert.Equal(t, "5Gi", getPVCStorageSize(t, "live-storage-pvc", namespace), "live-storage-pvc should request 5Gi")

		// Archive storage should be 6Gi (overridden by flag)
		assert.Equal(t, "6Gi", getPVStorageSize(t, "archive-storage-pv"), "archive-storage-pv should have 6Gi capacity")
		assert.Equal(t, "6Gi", getPVCStorageSize(t, "archive-storage-pvc", namespace), "archive-storage-pvc should request 6Gi")

		// Logging storage should be 5Gi (default)
		assert.Equal(t, "5Gi", getPVStorageSize(t, "logging-storage-pv"), "logging-storage-pv should have 5Gi capacity")
		assert.Equal(t, "5Gi", getPVCStorageSize(t, "logging-storage-pvc", namespace), "logging-storage-pvc should request 5Gi")

		// Get manifest from revision 1 (initial installation)
		manifest := getHelmManifest(t, releaseName, namespace, 1)
		require.NotEmpty(t, manifest, "Initial install manifest should not be empty")

		// Values used on the first manifest during installation (from blocknode_values.yaml)
		//  logs:
		//    level: "INFO"
		//    loggingProperties:
		//      org.hiero.block.level: "INFO"
		//      java.util.logging.ConsoleHandler.level: "INFO"
		//      java.util.logging.FileHandler.level: "INFO"
		assert.Contains(t, manifest, "level= INFO")
		assert.Contains(t, manifest, "org.hiero.block.level=INFO")
		assert.Contains(t, manifest, "java.util.logging.ConsoleHandler.level=INFO")
		assert.Contains(t, manifest, "java.util.logging.FileHandler.level=INFO")
	})

	t.Run("upgrade_with_no_reuse_values", func(t *testing.T) {
		cmd := testutil.PrepareSubCmdForTest(nodeCmd)

		resetFlags(cmd)

		relativeValuesPath := "../../../../../test/config/blocknode_other_values.yaml"
		absValuesPath, err := filepath.Abs(relativeValuesPath)
		require.NoError(t, err, "failed to get absolute path for values file")

		cmd.SetArgs([]string{
			"node",
			"upgrade",
			"--profile=local",
			"--values=" + absValuesPath,
			"--no-reuse-values",
		})
		err = cmd.Execute()
		require.NoError(t, err)

		// Get manifest from revision 2 (after upgrade with --no-reuse-values)
		manifest := getHelmManifest(t, releaseName, namespace, 2)
		require.NotEmpty(t, manifest, "Upgrade manifest should not be empty")

		// With --no-reuse-values and blocknode_other_values.yaml (which omits logging config),
		// values should revert to chart defaults from version 0.24.0
		// according to https://github.com/hiero-ledger/hiero-block-node/blob/v0.24.0/charts/block-node-server/values.yaml
		//  logs:
		//    level: "INFO"
		//    loggingProperties:
		//      org.hiero.block.level: "FINEST"
		//      java.util.logging.ConsoleHandler.level: "FINEST"
		//      java.util.logging.FileHandler.level: "FINEST"
		assert.Contains(t, manifest, "level= INFO")
		assert.Contains(t, manifest, "org.hiero.block.level=FINEST")
		assert.Contains(t, manifest, "java.util.logging.ConsoleHandler.level=FINEST")
		assert.Contains(t, manifest, "java.util.logging.FileHandler.level=FINEST")
	})

	t.Run("upgrade_with_reuse_values", func(t *testing.T) {
		cmd := testutil.PrepareSubCmdForTest(nodeCmd)

		resetFlags(cmd)

		relativeValuesPath := "../../../../../test/config/blocknode_values_for_reuse_test.yaml"
		absValuesPath, err := filepath.Abs(relativeValuesPath)
		require.NoError(t, err, "failed to get absolute path for values file")

		cmd.SetArgs([]string{
			"node",
			"upgrade",
			"--profile=local",
			"--values=" + absValuesPath,
			// Note: no --no-reuse-values, so it should reuse values by default
		})
		err = cmd.Execute()
		require.NoError(t, err, "upgrade with reuse values should succeed")

		// Get manifest from revision 3
		manifest := getHelmManifest(t, releaseName, namespace, 3)
		require.NotEmpty(t, manifest, "Manifest should not be empty")

		assert.Contains(t, manifest, "level= INFO")
		assert.Contains(t, manifest, "org.hiero.block.level=FINEST")
		assert.Contains(t, manifest, "java.util.logging.ConsoleHandler.level=FINEST")
		assert.Contains(t, manifest, "java.util.logging.FileHandler.level=FINEST")
		assert.Contains(t, manifest, "memory: 2Gi")
	})

	t.Run("upgrade_without_values_file_reuse_values", func(t *testing.T) {
		cmd := testutil.PrepareSubCmdForTest(nodeCmd)

		resetFlags(cmd)

		cmd.SetArgs([]string{
			"node",
			"upgrade",
			"--profile=local",
			// No --values flag - will reuse nano values set for local profile
		})
		err = cmd.Execute()
		require.NoError(t, err, "upgrade without values file should succeed")

		// Get manifest from revision 4
		manifest := getHelmManifest(t, releaseName, namespace, 4)
		require.NotEmpty(t, manifest, "Manifest should not be empty")

		assert.Contains(t, manifest, "level= INFO")
		assert.Contains(t, manifest, "org.hiero.block.level=FINEST")
		assert.Contains(t, manifest, "java.util.logging.ConsoleHandler.level=FINEST")
		assert.Contains(t, manifest, "java.util.logging.FileHandler.level=FINEST")
		assert.Contains(t, manifest, "memory: 1Gi")
		assert.Contains(t, manifest, "memory: 1.5Gi")
	})

	t.Run("upgrade_with_flag_overrides", func(t *testing.T) {
		cmd := testutil.PrepareSubCmdForTest(nodeCmd)

		resetFlags(cmd)

		relativeValuesPath := "../../../../../test/config/blocknode_values.yaml"
		absValuesPath, err := filepath.Abs(relativeValuesPath)
		require.NoError(t, err, "failed to get absolute path for values file")

		cmd.SetArgs([]string{
			"node",
			"upgrade",
			"--profile=local",
			"--values=" + absValuesPath,
			"--chart-version=0.22.1",
			"--archive-path=/mnt/custom-archive",
		})
		err = cmd.Execute()
		require.NoError(t, err, "upgrade with flag overrides should succeed")

		// Verify config was updated with flag overrides
		cfg := config.Get()
		assert.Equal(t, "0.22.1", cfg.BlockNode.Version, "version should be updated")
		assert.Equal(t, "/mnt/custom-archive", cfg.BlockNode.Storage.ArchivePath, "archive path should be updated")

		// Get manifest from revision 5
		manifest := getHelmManifest(t, releaseName, namespace, 5)
		require.NotEmpty(t, manifest, "Manifest should not be empty")

		// Should have INFO levels from blocknode_values.yaml
		assert.Contains(t, manifest, "level= INFO")
		assert.Contains(t, manifest, "org.hiero.block.level=INFO")
		assert.Contains(t, manifest, "java.util.logging.ConsoleHandler.level=INFO")
		assert.Contains(t, manifest, "java.util.logging.FileHandler.level=INFO")
	})
}

// serialMu enforces sequential execution of tests that manipulate shared package-level flag variables.
// Tests acquire this lock at the start and release it via t.Cleanup, ensuring mutual exclusion.
var serialMu sync.Mutex

// serial ensures a test runs sequentially, never in parallel with other tests using this function.
// It acquires a mutex that is released when the test completes (via t.Cleanup).
// This protects shared package-level flag variables from concurrent access.
func serial(t *testing.T) {
	t.Helper()
	serialMu.Lock()
	t.Cleanup(serialMu.Unlock)
}

// resetFlags resets all flag variables to empty strings
func resetFlags(cmd *cobra.Command) {
	flagProfile = "local"
	flagNamespace = ""
	flagReleaseName = ""
	flagChartRepo = ""
	flagChartVersion = ""
	flagBasePath = ""
	flagArchivePath = ""
	flagLivePath = ""
	flagLogPath = ""
	flagValuesFile = ""
	flagNoReuseValues = false

	if cmd != nil {
		cmd.ResetFlags()

		// inject the shared profile flag into the test command so prepareUserInputs sees it
		common.FlagProfile.SetVarP(cmd, &flagProfile, false)
	}
}

// Returns the raw manifest string
func getHelmManifest(t *testing.T, releaseName, namespace string, revision int) string {
	t.Helper()

	args := []string{"get", "manifest", releaseName, "-n", namespace}
	if revision > 0 {
		args = append(args, "--revision", fmt.Sprintf("%d", revision))
	}

	cmd := exec.Command("helm", args...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to get helm manifest: %s", string(output))

	return string(output)
}

// getPVStorageSize retrieves the storage capacity of a PersistentVolume
func getPVStorageSize(t *testing.T, pvName string) string {
	t.Helper()

	cmd := exec.Command("kubectl", "get", "pv", pvName, "-o", "jsonpath={.spec.capacity.storage}")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to get PV storage size: %s", string(output))

	return string(output)
}

// getPVCStorageSize retrieves the requested storage size of a PersistentVolumeClaim
func getPVCStorageSize(t *testing.T, pvcName, namespace string) string {
	t.Helper()

	cmd := exec.Command("kubectl", "get", "pvc", pvcName, "-n", namespace, "-o", "jsonpath={.spec.resources.requests.storage}")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to get PVC storage size: %s", string(output))

	return string(output)
}

// TestNegative_InvalidValuesFilePath tests that invalid values file paths are rejected
func TestNegative_InvalidValuesFilePath(t *testing.T) {
	serial(t)

	testCases := []struct {
		name          string
		valuesFile    string
		expectedError string
	}{
		{
			name:          "path_traversal_attack_absolute",
			valuesFile:    "/tmp/../etc/passwd",
			expectedError: "path cannot contain '..' segments",
		},
		{
			name:          "path_traversal_attack_relative",
			valuesFile:    "../../../etc/passwd",
			expectedError: "path cannot contain '..' segments",
		},
		{
			name:          "shell_metacharacters",
			valuesFile:    "/tmp/test; rm -rf /",
			expectedError: "path contains shell metacharacters",
		},
		{
			name:          "invalid_characters",
			valuesFile:    "/tmp/test\x00file.yaml",
			expectedError: "path contains invalid characters",
		},
		{
			name:          "non_existent_file",
			valuesFile:    "/tmp/non-existent-file-12345.yaml",
			expectedError: "file does not exist",
		},
		{
			name:          "path_with_multiple_traversals",
			valuesFile:    "/opt/../../etc/hosts",
			expectedError: "path cannot contain '..' segments",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Reset(t)

			configContent := `
blockNode:
  namespace: "test-ns"
  release: "test-release"
  chart: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
  version: "0.24.0"
  storage:
    basePath: "/mnt/storage"
`
			tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
			err := os.WriteFile(tmpConfigFile, []byte(configContent), 0644)
			require.NoError(t, err)

			err = config.Initialize(tmpConfigFile)
			require.NoError(t, err)

			cmd := testutil.PrepareSubCmdForTest(GetCmd())
			resetFlags(cmd)

			// Attempt to install with invalid values file
			cmd.SetArgs([]string{
				"node",
				"install",
				"--profile=local",
				"--values=" + tc.valuesFile,
			})

			err = cmd.Execute()
			require.Error(t, err, "Expected error for invalid values file path")
			assert.Contains(t, err.Error(), tc.expectedError)
		})
	}
}

// TestNegative_InvalidStoragePaths tests that invalid storage paths are rejected
// when they are actually validated in GetStoragePaths() during the workflow execution
func TestNegative_InvalidStoragePaths(t *testing.T) {
	serial(t)

	testCases := []struct {
		name          string
		flagName      string
		flagValue     string
		expectedError string
	}{
		{
			name:          "base_path_with_shell_metacharacters",
			flagName:      "--base-path",
			flagValue:     "/mnt/storage; echo hacked",
			expectedError: "path contains shell metacharacters",
		},
		{
			name:          "archive_path_with_path_traversal",
			flagName:      "--archive-path",
			flagValue:     "/mnt/../etc/passwd",
			expectedError: "path cannot contain '..' segments",
		},
		{
			name:          "live_path_with_backticks",
			flagName:      "--live-path",
			flagValue:     "/mnt/`whoami`",
			expectedError: "path contains shell metacharacters",
		},
		{
			name:          "log_path_with_pipe",
			flagName:      "--log-path",
			flagValue:     "/mnt/logs | cat",
			expectedError: "path contains shell metacharacters",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Reset(t)

			configContent := `
blockNode:
  namespace: "test-ns"
  release: "test-release"
  chart: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
  version: "0.24.0"
`
			tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
			err := os.WriteFile(tmpConfigFile, []byte(configContent), 0644)
			require.NoError(t, err)

			err = config.Initialize(tmpConfigFile)
			require.NoError(t, err)

			cmd := testutil.PrepareSubCmdForTest(GetCmd())
			resetFlags(cmd)

			valuesFile := filepath.Join(t.TempDir(), "test-values.yaml")
			err = os.WriteFile(valuesFile, []byte("# test values"), 0644)
			require.NoError(t, err)

			// Attempt to install with invalid storage path
			// The validation happens in GetStoragePaths() when the workflow executes,
			// not at CLI flag parsing level
			cmd.SetArgs([]string{
				"node",
				"install",
				"--profile=local",
				"--values=" + valuesFile,
				tc.flagName + "=" + tc.flagValue,
			})

			err = cmd.Execute()
			// Storage paths are validated via sanity.SanitizePath() when GetStoragePaths() is called
			// during workflow execution, so these should fail with appropriate errors
			require.Error(t, err, "Expected error for invalid storage path")
			assert.Contains(t, err.Error(), tc.expectedError)
		})
	}
}

// TestNegative_DirectoryAsValuesFile tests that directories are rejected as values files
func TestNegative_DirectoryAsValuesFile(t *testing.T) {
	serial(t)

	testutil.Reset(t)

	configContent := `
blockNode:
  namespace: "test-ns"
  release: "test-release"
  chart: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
  version: "0.24.0"
`
	tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
	err := os.WriteFile(tmpConfigFile, []byte(configContent), 0644)
	require.NoError(t, err)

	err = config.Initialize(tmpConfigFile)
	require.NoError(t, err)

	cmd := testutil.PrepareSubCmdForTest(GetCmd())
	resetFlags(cmd)

	// Try to use a directory as a values file
	dirPath := t.TempDir()

	cmd.SetArgs([]string{
		"node",
		"install",
		"--profile=local",
		"--values=" + dirPath,
	})

	err = cmd.Execute()
	require.Error(t, err, "Expected error when using directory as values file")
	assert.Contains(t, err.Error(), "not a regular file")
}

// TestNegative_InvalidChartVersion tests various invalid chart version formats
func TestNegative_InvalidChartVersion(t *testing.T) {
	serial(t)

	cmd := testutil.PrepareSubCmdForTest(GetCmd())
	resetFlags(cmd)
	flagProfile = "local"

	testutil.Reset(t)

	// Create a test config file
	configContent := `
blockNode:
  namespace: "test-ns"
  release: "test-release"
  chart: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
  version: "0.24.0"
`
	tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
	err := os.WriteFile(tmpConfigFile, []byte(configContent), 0644)
	require.NoError(t, err)

	err = config.Initialize(tmpConfigFile)
	require.NoError(t, err)

	testCases := []struct {
		name          string
		chartVersion  string
		expectError   bool
		errorContains string
	}{
		{
			name:          "version_with_shell_injection",
			chartVersion:  "0.24.0; rm -rf /",
			expectError:   true,
			errorContains: "version contains invalid characters",
		},
		{
			name:          "version_with_path_traversal",
			chartVersion:  "../../../etc/passwd",
			expectError:   true,
			errorContains: "version contains invalid characters",
		},
		{
			name:          "version_with_special_chars",
			chartVersion:  "0.24.0@evil",
			expectError:   true,
			errorContains: "version contains invalid characters",
		},
		{
			name:          "version_with_spaces",
			chartVersion:  "0.24.0 malicious",
			expectError:   true,
			errorContains: "version contains invalid characters",
		},
		{
			name:          "version_with_backticks",
			chartVersion:  "0.24.0`rm -rf /`",
			expectError:   true,
			errorContains: "version contains invalid characters",
		},
		{
			name:          "version_with_invalid_format",
			chartVersion:  "not-a-version",
			expectError:   true,
			errorContains: "version contains invalid characters",
		},
		{
			name:          "valid_version_simple",
			chartVersion:  "1.0.0",
			expectError:   false,
			errorContains: "",
		},
		{
			name:          "valid_version_with_prerelease",
			chartVersion:  "1.0.0-alpha",
			expectError:   false,
			errorContains: "",
		},
		{
			name:          "valid_version_with_prerelease_number",
			chartVersion:  "1.0.0-beta.1",
			expectError:   false,
			errorContains: "",
		},
		{
			name:          "valid_version_with_build_metadata",
			chartVersion:  "1.0.0+build.123",
			expectError:   false,
			errorContains: "",
		},
		{
			name:          "valid_version_with_prerelease_and_build",
			chartVersion:  "1.0.0-rc.1+build.456",
			expectError:   false,
			errorContains: "",
		},
		{
			name:          "default_version_used_from_config",
			chartVersion:  "0.24.0",
			expectError:   false, // valid configured/default chart version should not cause a validation error
			errorContains: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Reset config for each test case
			err := config.Initialize(tmpConfigFile)
			require.NoError(t, err)

			// Set the chart version flag
			flagChartVersion = tc.chartVersion

			// Now validate the configuration to ensure invalid versions are rejected
			inputs, validateErr := prepareUserInputs(cmd, []string{})
			if tc.expectError {
				require.Error(t, validateErr, "expected validation error for chart version %q", tc.chartVersion)
				if tc.errorContains != "" {
					assert.Contains(t, validateErr.Error(), tc.errorContains)
				}
			} else {
				// Verify the version was set in the config
				assert.Equal(t, tc.chartVersion, inputs.Custom.ChartVersion)
				require.NoError(t, validateErr, "did not expect validation error for chart version %q", tc.chartVersion)
			}
		})
	}
}

// TestNegative_InvalidYAMLInConfigFile tests that invalid YAML in config file is handled
func TestNegative_InvalidYAMLInConfigFile(t *testing.T) {
	serial(t)

	testutil.Reset(t)

	// Create a config file with invalid YAML
	invalidYAML := `
blockNode:
  namespace: "test-ns"
  release: [this is not valid yaml structure
  chart: "oci://example.com/chart"
`
	tmpConfigFile := filepath.Join(t.TempDir(), "invalid-config.yaml")
	err := os.WriteFile(tmpConfigFile, []byte(invalidYAML), 0644)
	require.NoError(t, err)

	err = config.Initialize(tmpConfigFile)
	// Should fail to parse invalid YAML
	require.Error(t, err, "Expected error when parsing invalid YAML config")
}
