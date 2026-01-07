// SPDX-License-Identifier: Apache-2.0

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
	flagForce = false
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
		common.FlagForce.SetVarP(cmd, &flagForce, false)
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
  chartRepo: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
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
  chartRepo: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
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
  chartRepo: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
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
	flagForce = false

	testutil.Reset(t)

	// Create a test config file
	configContent := `
blockNode:
  namespace: "test-ns"
  release: "test-release"
  chartRepo: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
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
  chartRepo: "oci://example.com/chart"
`
	tmpConfigFile := filepath.Join(t.TempDir(), "invalid-config.yaml")
	err := os.WriteFile(tmpConfigFile, []byte(invalidYAML), 0644)
	require.NoError(t, err)

	err = config.Initialize(tmpConfigFile)
	// Should fail to parse invalid YAML
	require.Error(t, err, "Expected error when parsing invalid YAML config")
}

func TestPrepareUserInputs(t *testing.T) {
	// prepare a command with flags that common.Flag* helpers will read
	cmd := &cobra.Command{}
	cmd.Flags().String("profile", "", "profile")
	cmd.Flags().Bool("force", false, "force")
	if err := cmd.Flags().Set("profile", "local"); err != nil {
		t.Fatalf("failed to set profile flag: %v", err)
	}
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("failed to set force flag: %v", err)
	}

	// set package-level flag variables that prepareUserInputs reads directly
	flagValuesFile = ""
	flagChartVersion = "1.2.3"
	flagChartRepo = "https://charts.example"
	flagNamespace = "myns"
	flagReleaseName = "myrel"
	flagBasePath = "/base"
	flagArchivePath = "/archive"
	flagLivePath = "/live"
	flagLogPath = "/log"
	flagLiveSize = "10Gi"
	flagArchiveSize = "20Gi"
	flagLogSize = "5Gi"
	flagNoReuseValues = false

	// set execution-related booleans (only one true to avoid ambiguity)
	flagContinueOnError = true
	flagStopOnError = false
	flagRollbackOnError = false

	// call the function under test
	ui, err := prepareUserInputs(cmd, []string{})
	if err != nil {
		t.Fatalf("prepareUserInputs returned error: %v", err)
	}

	// assertions
	if ui.Custom.Profile != "local" {
		t.Fatalf("unexpected Profile: got %q want %q", ui.Custom.Profile, "local")
	}
	if ui.Custom.Namespace != flagNamespace {
		t.Fatalf("unexpected Namespace: got %q want %q", ui.Custom.Namespace, flagNamespace)
	}
	if ui.Custom.ReleaseName != flagReleaseName {
		t.Fatalf("unexpected ReleaseName: got %q want %q", ui.Custom.ReleaseName, flagReleaseName)
	}
	if ui.Custom.ChartRepo != flagChartRepo {
		t.Fatalf("unexpected ChartRepo: got %q want %q", ui.Custom.ChartRepo, flagChartRepo)
	}
	if ui.Custom.ChartVersion != flagChartVersion {
		t.Fatalf("unexpected ChartVersion: got %q want %q", ui.Custom.ChartVersion, flagChartVersion)
	}
	if ui.Common.Force != true {
		t.Fatalf("unexpected Common.Force: got %v want %v", ui.Common.Force, true)
	}
	if ui.Custom.Storage.BasePath != flagBasePath {
		t.Fatalf("unexpected Storage.BasePath: got %q want %q", ui.Custom.Storage.BasePath, flagBasePath)
	}
	// ReuseValues should be the inverse of flagNoReuseValues
	if ui.Custom.ReuseValues != !flagNoReuseValues {
		t.Fatalf("unexpected ReuseValues: got %v want %v", ui.Custom.ReuseValues, !flagNoReuseValues)
	}
}
