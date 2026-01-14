// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/deps"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/viper"
)

// Config holds the global configuration for the application.
type Config struct {
	Log       logx.LoggingConfig `yaml:"log" json:"log"`
	BlockNode BlockNodeConfig    `yaml:"blockNode" json:"blockNode"`
	Alloy     AlloyConfig        `yaml:"alloy" json:"alloy"`
}

// BlockNodeStorage represents the `storage` section under `blockNode`.
type BlockNodeStorage struct {
	BasePath    string `yaml:"basePath" json:"basePath"`
	ArchivePath string `yaml:"archivePath" json:"archivePath"`
	LivePath    string `yaml:"livePath" json:"livePath"`
	LogPath     string `yaml:"logPath" json:"logPath"`
	LiveSize    string `yaml:"liveSize" json:"liveSize"`
	ArchiveSize string `yaml:"archiveSize" json:"archiveSize"`
	LogSize     string `yaml:"logSize" json:"logSize"`
}

// Validate validates all storage paths to ensure they are safe and secure.
// This performs early validation of user-provided paths to catch security issues
// before workflow execution begins.
func (s *BlockNodeStorage) Validate() error {
	// Validate BasePath if provided
	if s.BasePath != "" {
		if _, err := sanity.SanitizePath(s.BasePath); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid base path: %s", s.BasePath)
		}
	}

	// Validate ArchivePath if provided
	if s.ArchivePath != "" {
		if _, err := sanity.SanitizePath(s.ArchivePath); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid archive path: %s", s.ArchivePath)
		}
	}

	// Validate LivePath if provided
	if s.LivePath != "" {
		if _, err := sanity.SanitizePath(s.LivePath); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid live path: %s", s.LivePath)
		}
	}

	// Validate LogPath if provided
	if s.LogPath != "" {
		if _, err := sanity.SanitizePath(s.LogPath); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid log path: %s", s.LogPath)
		}
	}

	// Validate storage sizes if provided
	if s.LiveSize != "" {
		if err := sanity.ValidateStorageSize(s.LiveSize); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid live storage size: %s", s.LiveSize)
		}
	}

	if s.ArchiveSize != "" {
		if err := sanity.ValidateStorageSize(s.ArchiveSize); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid archive storage size: %s", s.ArchiveSize)
		}
	}

	if s.LogSize != "" {
		if err := sanity.ValidateStorageSize(s.LogSize); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid log storage size: %s", s.LogSize)
		}
	}

	return nil
}

// Validate validates all configuration fields to ensure they are safe and secure.
func (c Config) Validate() error {
	if err := c.BlockNode.Validate(); err != nil {
		return err
	}
	if err := c.Alloy.Validate(); err != nil {
		return err
	}
	return nil
}

// BlockNodeConfig represents the `blockNode` configuration block.
type BlockNodeConfig struct {
	Namespace string           `yaml:"namespace" json:"namespace"`
	Release   string           `yaml:"release" json:"release"`
	Chart     string           `yaml:"chart" json:"chart"`
	Version   string           `yaml:"version" json:"version"`
	Storage   BlockNodeStorage `yaml:"storage" json:"storage"`
}

// AlloyConfig represents the `alloy` configuration block for observability.
type AlloyConfig struct {
	Enabled            bool   `yaml:"enabled" json:"enabled"`
	MonitorBlockNode   bool   `yaml:"monitorBlockNode" json:"monitorBlockNode"`
	PrometheusURL      string `yaml:"prometheusUrl" json:"prometheusUrl"`
	PrometheusUsername string `yaml:"prometheusUsername" json:"prometheusUsername"`
	PrometheusPassword string `yaml:"prometheusPassword" json:"prometheusPassword"`
	LokiURL            string `yaml:"lokiUrl" json:"lokiUrl"`
	LokiUsername       string `yaml:"lokiUsername" json:"lokiUsername"`
	LokiPassword       string `yaml:"lokiPassword" json:"lokiPassword"`
	ClusterName        string `yaml:"clusterName" json:"clusterName"`
}

// Validate validates all block node configuration fields to ensure they are safe and secure.
// This performs early validation of user-provided configuration to catch security issues
// before workflow execution begins.
func (c *BlockNodeConfig) Validate() error {
	// Validate namespace if provided (must be a valid Kubernetes identifier)
	if c.Namespace != "" {
		if err := sanity.ValidateIdentifier(c.Namespace); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid namespace: %s", c.Namespace)
		}
	}

	// Validate release name if provided (must be a valid Helm release identifier)
	if c.Release != "" {
		if err := sanity.ValidateIdentifier(c.Release); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid release name: %s", c.Release)
		}
	}

	// Validate chart if provided (Helm chart reference: OCI, URL, or repo/chart)
	if c.Chart != "" {
		if err := sanity.ValidateChartReference(c.Chart); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid chart reference: %s", c.Chart)
		}
	}

	// Validate version if provided (semantic version)
	if c.Version != "" {
		if err := sanity.ValidateVersion(c.Version); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid version: %s", c.Version)
		}
	}

	// Validate storage paths
	if err := c.Storage.Validate(); err != nil {
		return err
	}

	return nil
}

// Validate validates all Alloy configuration fields.
func (c *AlloyConfig) Validate() error {
	// Only validate if Alloy is enabled
	if !c.Enabled {
		return nil
	}

	// Validate cluster name if provided
	if c.ClusterName != "" {
		if err := sanity.ValidateIdentifier(c.ClusterName); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid cluster name: %s", c.ClusterName)
		}
	}

	// Note: URLs and credentials are validated at runtime when they're used
	// to avoid overly restrictive validation here

	return nil
}

var globalConfig = Config{
	Log: logx.LoggingConfig{
		Level:          "Debug",
		ConsoleLogging: true,
		FileLogging:    false,
	},
	BlockNode: BlockNodeConfig{
		Namespace: deps.BLOCK_NODE_NAMESPACE,
		Release:   deps.BLOCK_NODE_RELEASE,
		Chart:     deps.BLOCK_NODE_CHART,
		Version:   deps.BLOCK_NODE_VERSION,
		Storage: BlockNodeStorage{
			BasePath:    deps.BLOCK_NODE_STORAGE_BASE_PATH,
			ArchivePath: "",
			LivePath:    "",
			LogPath:     "",
			LiveSize:    "",
			ArchiveSize: "",
			LogSize:     "",
		},
	},
	Alloy: AlloyConfig{
		Enabled:            false,
		MonitorBlockNode:   false,
		PrometheusURL:      "",
		PrometheusUsername: "",
		PrometheusPassword: "",
		LokiURL:            "",
		LokiUsername:       "",
		LokiPassword:       "",
		ClusterName:        "",
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
		globalConfig = Config{}
		viper.Reset()
		viper.SetConfigFile(path)
		viper.SetEnvPrefix("weaver")
		viper.AutomaticEnv()
		viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

		err := viper.ReadInConfig()
		if err != nil {
			return NotFoundError.Wrap(err, "failed to read config file: %s", path).
				WithProperty(errorx.PropertyPayload(), path)
		}

		if err := viper.Unmarshal(&globalConfig); err != nil {
			return errorx.IllegalFormat.Wrap(err, "failed to parse configuration").
				WithProperty(errorx.PropertyPayload(), path)
		}
	}

	return nil
}

// overrideWithEnv overrides configuration values with environment variables.
func overrideWithEnv(value string) string {
	if envValue := os.Getenv(value); envValue != "" {
		return envValue
	}
	return value
}

// Get returns the loaded configuration.
//
// Returns:
//   - The global configuration.
func Get() Config {
	return globalConfig
}

func Set(c *Config) error {
	globalConfig = *c
	return nil
}

// OverrideBlockNodeConfig updates the block node configuration with provided overrides.
// Empty string values are ignored (not applied).
func OverrideBlockNodeConfig(overrides BlockNodeConfig) {
	if overrides.Namespace != "" {
		globalConfig.BlockNode.Namespace = overrides.Namespace
	}
	if overrides.Release != "" {
		globalConfig.BlockNode.Release = overrides.Release
	}
	if overrides.Chart != "" {
		globalConfig.BlockNode.Chart = overrides.Chart
	}
	if overrides.Version != "" {
		globalConfig.BlockNode.Version = overrides.Version
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
	if overrides.Storage.LiveSize != "" {
		globalConfig.BlockNode.Storage.LiveSize = overrides.Storage.LiveSize
	}
	if overrides.Storage.ArchiveSize != "" {
		globalConfig.BlockNode.Storage.ArchiveSize = overrides.Storage.ArchiveSize
	}
	if overrides.Storage.LogSize != "" {
		globalConfig.BlockNode.Storage.LogSize = overrides.Storage.LogSize
	}
}

// OverrideAlloyConfig updates the Alloy configuration with provided overrides.
// Empty string values are ignored (not applied), except for Enabled which is always applied.
func OverrideAlloyConfig(overrides AlloyConfig) {
	globalConfig.Alloy.Enabled = overrides.Enabled
	globalConfig.Alloy.MonitorBlockNode = overrides.MonitorBlockNode
	if overrides.PrometheusURL != "" {
		globalConfig.Alloy.PrometheusURL = overrides.PrometheusURL
	}
	if overrides.PrometheusUsername != "" {
		globalConfig.Alloy.PrometheusUsername = overrides.PrometheusUsername
	}
	if overrides.PrometheusPassword != "" {
		globalConfig.Alloy.PrometheusPassword = overrides.PrometheusPassword
	}
	if overrides.LokiURL != "" {
		globalConfig.Alloy.LokiURL = overrides.LokiURL
	}
	if overrides.LokiUsername != "" {
		globalConfig.Alloy.LokiUsername = overrides.LokiUsername
	}
	if overrides.LokiPassword != "" {
		globalConfig.Alloy.LokiPassword = overrides.LokiPassword
	}
	if overrides.ClusterName != "" {
		globalConfig.Alloy.ClusterName = overrides.ClusterName
	}
}
