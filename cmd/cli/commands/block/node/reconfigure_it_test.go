// SPDX-License-Identifier: Apache-2.0

//go:build integration

package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseReconfigureConfig is a minimal valid config shared across reconfigure tests.
const baseReconfigureConfig = `
blockNode:
  namespace: "test-ns"
  release: "test-release"
  chart: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
  version: "0.30.2"
  storage:
    basePath: "/mnt/storage"
`

// TestReconfigure_ChartVersionFlagNotAccepted verifies that --chart-version is not
// a recognised flag on the reconfigure sub-command. Passing it must produce an
// "unknown flag" error, not silently re-deploy with a different chart version.
func TestReconfigure_ChartVersionFlagNotAccepted(t *testing.T) {
	serial(t)
	testutil.Reset(t)

	tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
	require.NoError(t, os.WriteFile(tmpConfigFile, []byte(baseReconfigureConfig), 0644))
	require.NoError(t, config.Initialize(tmpConfigFile))

	cmd := testutil.PrepareSubCmdForTest(GetCmd())
	resetFlags(cmd)

	cmd.SetArgs([]string{
		"node",
		"reconfigure",
		"--profile=local",
		"--chart-version=0.99.0",
	})

	err := cmd.Execute()
	require.Error(t, err, "expected error when passing --chart-version to reconfigure")
	assert.Contains(t, err.Error(), "unknown flag")
}

// TestReconfigure_BlockNodeNotInstalled verifies that reconfigure fails with a
// clear, actionable error when the block node has not yet been deployed.
func TestReconfigure_BlockNodeNotInstalled(t *testing.T) {
	serial(t)
	testutil.Reset(t)

	tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
	require.NoError(t, os.WriteFile(tmpConfigFile, []byte(baseReconfigureConfig), 0644))
	require.NoError(t, config.Initialize(tmpConfigFile))

	cmd := testutil.PrepareSubCmdForTest(GetCmd())
	resetFlags(cmd)

	cmd.SetArgs([]string{
		"node",
		"reconfigure",
		"--profile=local",
	})

	err := cmd.Execute()
	require.Error(t, err, "expected error when reconfiguring an uninstalled block node")
	assert.Contains(t, err.Error(), "block node is not installed")
}

// TestReconfigure_InvalidValuesFilePath verifies that an invalid --values path is
// rejected before any workflow step runs.
func TestReconfigure_InvalidValuesFilePath(t *testing.T) {
	serial(t)

	testCases := []struct {
		name          string
		valuesFile    string
		expectedError string
	}{
		{
			name:          "path_traversal",
			valuesFile:    "/tmp/../etc/passwd",
			expectedError: "path cannot contain '..' segments",
		},
		{
			name:          "shell_metacharacters",
			valuesFile:    "/tmp/values; rm -rf /",
			expectedError: "path contains shell metacharacters",
		},
		{
			name:          "non_existent_file",
			valuesFile:    "/tmp/no-such-file-reconfigure.yaml",
			expectedError: "file does not exist",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Reset(t)

			tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
			require.NoError(t, os.WriteFile(tmpConfigFile, []byte(baseReconfigureConfig), 0644))
			require.NoError(t, config.Initialize(tmpConfigFile))

			cmd := testutil.PrepareSubCmdForTest(GetCmd())
			resetFlags(cmd)

			cmd.SetArgs([]string{
				"node",
				"reconfigure",
				"--profile=local",
				"--values=" + tc.valuesFile,
			})

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.expectedError)
		})
	}
}

// TestReconfigure_InvalidStoragePaths verifies that malicious or malformed storage
// path flags are rejected before any workflow step runs.
func TestReconfigure_InvalidStoragePaths(t *testing.T) {
	serial(t)

	testCases := []struct {
		name          string
		flagName      string
		flagValue     string
		expectedError string
	}{
		{
			name:          "base_path_shell_injection",
			flagName:      "--base-path",
			flagValue:     "/mnt/storage; echo hacked",
			expectedError: "path contains shell metacharacters",
		},
		{
			name:          "archive_path_traversal",
			flagName:      "--archive-path",
			flagValue:     "/mnt/../etc/passwd",
			expectedError: "path cannot contain '..' segments",
		},
		{
			name:          "live_path_backtick",
			flagName:      "--live-path",
			flagValue:     "/mnt/`whoami`",
			expectedError: "path contains shell metacharacters",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Reset(t)

			tmpConfigFile := filepath.Join(t.TempDir(), "test-config.yaml")
			require.NoError(t, os.WriteFile(tmpConfigFile, []byte(baseReconfigureConfig), 0644))
			require.NoError(t, config.Initialize(tmpConfigFile))

			cmd := testutil.PrepareSubCmdForTest(GetCmd())
			resetFlags(cmd)

			cmd.SetArgs([]string{
				"node",
				"reconfigure",
				"--profile=local",
				tc.flagName + "=" + tc.flagValue,
			})

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.expectedError)
		})
	}
}
