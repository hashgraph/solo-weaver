// SPDX-License-Identifier: Apache-2.0

//go:build integration

package node

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		// Add the profile flag since it's required by the parent block command
		cmd.PersistentFlags().String(common.FlagProfile().Name, "", "profile to use for block commands")
		cmd.PersistentFlags().String(common.FlagConfig().Name, "", "config file for commands")
		cmd.PersistentFlags().Bool(common.FlagForce().Name, false, "force continue even if checks fail")
		cmd.PersistentFlags().String(common.FlagLogLevel().Name, "debug", "logrus log level (trace, debug, info, warn, error, fatal, panic)")
		// Re-add skip-hardware-checks since it's registered on the root command
		cmd.PersistentFlags().Bool(common.FlagSkipHardwareChecks().Name, false, "Skip hardware validation checks")
	}
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

	resetFlags(nil)
	testutil.Reset(t)

	// Create a test config file
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

			cfg := config.Get()
			cfg.BlockNode.ChartVersion = tc.chartVersion
			// Now validate the configuration to ensure invalid versions are rejected
			validateErr := cfg.Validate()
			if tc.expectError {
				require.Error(t, validateErr, "expected validation error for chart version %q", tc.chartVersion)
				if tc.errorContains != "" {
					assert.Contains(t, validateErr.Error(), tc.errorContains)
				}
			} else {
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
