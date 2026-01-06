// SPDX-License-Identifier: Apache-2.0

//go:build integration

package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
