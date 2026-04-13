// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"

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
// Environment variable overrides (SOLO_PROVISIONER_*) are intentionally NOT applied here.
// They are handled by the RSL layer via WithEnv(EnvConfig()) so that precedence is tracked
// correctly per-field (env > config file, but not > CLI flags).
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
		// AutomaticEnv is intentionally omitted: env var merging now happens in the RSL
		// layer (BlockNodeRuntimeResolver.WithEnv) so that StrategyEnv has the correct
		// precedence position between StrategyUserInput and StrategyConfig.

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

// DefaultsConfig returns a models.Config populated with hardcoded compile-time
// defaults from the deps package. This is passed to Resolver.WithDefaults so that
// the RSL layer has correct StrategyDefault values — always the deps constants,
// never the config file values.
func DefaultsConfig() models.Config {
	return models.Config{
		BlockNode: models.BlockNodeConfig{
			Namespace:    deps.BLOCK_NODE_NAMESPACE,
			Release:      deps.BLOCK_NODE_RELEASE,
			Chart:        deps.BLOCK_NODE_CHART,
			ChartVersion: deps.BLOCK_NODE_VERSION,
			Storage: models.BlockNodeStorage{
				BasePath: deps.BLOCK_NODE_STORAGE_BASE_PATH,
			},
		},
		Teleport: models.TeleportConfig{
			Version: deps.TELEPORT_VERSION,
		},
	}
}

// EnvConfig returns a models.Config populated exclusively from SOLO_PROVISIONER_*
// environment variables. Fields with no matching env var are left at their zero value.
//
// This is intended to be passed to Resolver.WithEnv so that the RSL layer can register
// env vars as StrategyEnv — above StrategyConfig (config file) but below StrategyUserInput
// (CLI flags).
//
// The env key naming follows the same convention previously used by Viper:
// prefix "SOLO_PROVISIONER" + uppercase field path with dots replaced by underscores.
// Note: ChartVersion uses the yaml tag "version", so its env key is
// SOLO_PROVISIONER_BLOCKNODE_VERSION (not BLOCKNODE_CHARTVERSION).
func EnvConfig() models.Config {
	return models.Config{
		BlockNode: models.BlockNodeConfig{
			Namespace:    os.Getenv("SOLO_PROVISIONER_BLOCKNODE_NAMESPACE"),
			Release:      os.Getenv("SOLO_PROVISIONER_BLOCKNODE_RELEASE"),
			Chart:        os.Getenv("SOLO_PROVISIONER_BLOCKNODE_CHART"),
			ChartVersion: os.Getenv("SOLO_PROVISIONER_BLOCKNODE_VERSION"),
			ChartName:    os.Getenv("SOLO_PROVISIONER_BLOCKNODE_CHARTNAME"),
			Storage: models.BlockNodeStorage{
				BasePath:         os.Getenv("SOLO_PROVISIONER_BLOCKNODE_STORAGE_BASEPATH"),
				ArchivePath:      os.Getenv("SOLO_PROVISIONER_BLOCKNODE_STORAGE_ARCHIVEPATH"),
				LivePath:         os.Getenv("SOLO_PROVISIONER_BLOCKNODE_STORAGE_LIVEPATH"),
				LogPath:          os.Getenv("SOLO_PROVISIONER_BLOCKNODE_STORAGE_LOGPATH"),
				VerificationPath: os.Getenv("SOLO_PROVISIONER_BLOCKNODE_STORAGE_VERIFICATIONPATH"),
				PluginsPath:      os.Getenv("SOLO_PROVISIONER_BLOCKNODE_STORAGE_PLUGINSPATH"),
				LiveSize:         os.Getenv("SOLO_PROVISIONER_BLOCKNODE_STORAGE_LIVESIZE"),
				ArchiveSize:      os.Getenv("SOLO_PROVISIONER_BLOCKNODE_STORAGE_ARCHIVESIZE"),
				LogSize:          os.Getenv("SOLO_PROVISIONER_BLOCKNODE_STORAGE_LOGSIZE"),
				VerificationSize: os.Getenv("SOLO_PROVISIONER_BLOCKNODE_STORAGE_VERIFICATIONSIZE"),
				PluginsSize:      os.Getenv("SOLO_PROVISIONER_BLOCKNODE_STORAGE_PLUGINSSIZE"),
			},
		},
		Teleport: models.TeleportConfig{
			Version:            os.Getenv("SOLO_PROVISIONER_TELEPORT_VERSION"),
			ValuesFile:         os.Getenv("SOLO_PROVISIONER_TELEPORT_VALUESFILE"),
			NodeAgentToken:     os.Getenv("SOLO_PROVISIONER_TELEPORT_NODEAGENTTOKEN"),
			NodeAgentProxyAddr: os.Getenv("SOLO_PROVISIONER_TELEPORT_NODEAGENTPROXYADDR"),
		},
	}
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

// SetProxy updates the global proxy configuration.
func SetProxy(cfg models.ProxyConfig) {
	globalConfig.Proxy = cfg
}

// IsProxyEnabled returns whether proxy mode is currently active.
func IsProxyEnabled() bool {
	return globalConfig.Proxy.Enabled
}
