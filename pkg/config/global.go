// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/deps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/spf13/viper"
)

var globalConfig = models.Config{
	Profile: models.ProfileLocal,
	Log: logx.LoggingConfig{
		Level:          "Debug",
		ConsoleLogging: true,
		FileLogging:    false,
	},
	BlockNode: models.BlockNodeConfig{
		Namespace:    deps.BLOCK_NODE_NAMESPACE,
		Release:      deps.BLOCK_NODE_RELEASE,
		Chart:        deps.BLOCK_NODE_CHART,
		ChartVersion: deps.BLOCK_NODE_VERSION,
		Storage: models.BlockNodeStorage{
			BasePath:    deps.BLOCK_NODE_STORAGE_BASE_PATH,
			ArchivePath: "",
			LivePath:    "",
			LogPath:     "",
			LiveSize:    "",
			ArchiveSize: "",
			LogSize:     "",
		},
	},
	Alloy: models.AlloyConfig{
		MonitorBlockNode:   false,
		PrometheusURL:      "",
		PrometheusUsername: "",
		LokiURL:            "",
		LokiUsername:       "",
		ClusterName:        "",
	},
	Teleport: models.TeleportConfig{
		Version:    deps.TELEPORT_VERSION,
		ValuesFile: "",
	},
}

// Initialize loads the configuration from the specified file.
//
// Parameters:
//   - path: The path to the configuration file.
//
// Returns:
//   - An error if the configuration cannot be loaded.
func Initialize(path string) error {
	if path != "" {
		globalConfig = models.Config{}
		viper.Reset()
		viper.SetConfigFile(path)
		viper.SetEnvPrefix("SOLO_PROVISIONER")
		viper.AutomaticEnv()
		viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

		err := viper.ReadInConfig()
		if err != nil {
			return NotFoundError.Wrap(err, "failed to read config file: %s", path).
				WithProperty(errorx.PropertyPayload(), path)
		}

		if err := viper.UnmarshalExact(&globalConfig); err != nil {
			return errorx.IllegalFormat.Wrap(err, "failed to parse configuration (check for unknown fields)").
				WithProperty(errorx.PropertyPayload(), path)
		}
	}

	return nil
}

// Get returns the loaded configuration.
//
// Returns:
//   - The global configuration.
func Get() models.Config {
	return globalConfig
}

func Set(c *models.Config) error {
	globalConfig = *c
	return nil
}

// SetProfile sets the active profile in the global configuration.
func SetProfile(profile string) {
	globalConfig.Profile = profile
}

// OverrideBlockNodeConfig updates the block node configuration with provided overrides.
// Empty string values are ignored (not applied).
func OverrideBlockNodeConfig(overrides models.BlockNodeConfig) {
	if overrides.Namespace != "" {
		globalConfig.BlockNode.Namespace = overrides.Namespace
	}
	if overrides.Release != "" {
		globalConfig.BlockNode.Release = overrides.Release
	}
	if overrides.Chart != "" {
		globalConfig.BlockNode.Chart = overrides.Chart
	}
	if overrides.ChartVersion != "" {
		globalConfig.BlockNode.ChartVersion = overrides.ChartVersion
	}
	if overrides.Storage.BasePath != "" {
		globalConfig.BlockNode.Storage.BasePath = overrides.Storage.BasePath
	}
	if overrides.Storage.ArchivePath != "" {
		globalConfig.BlockNode.Storage.ArchivePath = overrides.Storage.ArchivePath
	}
	if overrides.Storage.LivePath != "" {
		globalConfig.BlockNode.Storage.LivePath = overrides.Storage.LivePath
	}
	if overrides.Storage.LogPath != "" {
		globalConfig.BlockNode.Storage.LogPath = overrides.Storage.LogPath
	}
	if overrides.Storage.VerificationPath != "" {
		globalConfig.BlockNode.Storage.VerificationPath = overrides.Storage.VerificationPath
	}
	if overrides.Storage.LiveSize != "" {
		globalConfig.BlockNode.Storage.LiveSize = overrides.Storage.LiveSize
	}
	if overrides.Storage.ArchiveSize != "" {
		globalConfig.BlockNode.Storage.ArchiveSize = overrides.Storage.ArchiveSize
	}
	if overrides.Storage.LogSize != "" {
		globalConfig.BlockNode.Storage.LogSize = overrides.Storage.LogSize
	}
	if overrides.Storage.VerificationSize != "" {
		globalConfig.BlockNode.Storage.VerificationSize = overrides.Storage.VerificationSize
	}
}

// OverrideAlloyConfig updates the Alloy configuration with provided overrides.
// Empty string values are ignored (not applied).
// Remote arrays are always replaced (declarative semantics) - pass empty arrays to clear remotes.
// Note: Passwords are managed via Vault and External Secrets Operator.
func OverrideAlloyConfig(overrides models.AlloyConfig) {
	globalConfig.Alloy.MonitorBlockNode = overrides.MonitorBlockNode
	if overrides.ClusterName != "" {
		globalConfig.Alloy.ClusterName = overrides.ClusterName
	}
	if overrides.ClusterSecretStoreName != "" {
		globalConfig.Alloy.ClusterSecretStoreName = overrides.ClusterSecretStoreName
	}

	// Handle multi-remote configuration (declarative - always replace, even with empty slices)
	globalConfig.Alloy.PrometheusRemotes = overrides.PrometheusRemotes
	globalConfig.Alloy.LokiRemotes = overrides.LokiRemotes

	// Legacy single-remote flags (for backward compatibility)
	if overrides.PrometheusURL != "" {
		globalConfig.Alloy.PrometheusURL = overrides.PrometheusURL
	}
	if overrides.PrometheusUsername != "" {
		globalConfig.Alloy.PrometheusUsername = overrides.PrometheusUsername
	}
	if overrides.LokiURL != "" {
		globalConfig.Alloy.LokiURL = overrides.LokiURL
	}
	if overrides.LokiUsername != "" {
		globalConfig.Alloy.LokiUsername = overrides.LokiUsername
	}
}

// OverrideTeleportConfig updates the Teleport configuration with provided overrides.
// Empty string values are ignored (not applied).
func OverrideTeleportConfig(overrides models.TeleportConfig) {
	if overrides.Version != "" {
		globalConfig.Teleport.Version = overrides.Version
	}
	if overrides.ValuesFile != "" {
		globalConfig.Teleport.ValuesFile = overrides.ValuesFile
	}
	if overrides.NodeAgentToken != "" {
		globalConfig.Teleport.NodeAgentToken = overrides.NodeAgentToken
	}
	if overrides.NodeAgentProxyAddr != "" {
		globalConfig.Teleport.NodeAgentProxyAddr = overrides.NodeAgentProxyAddr
	}
}
