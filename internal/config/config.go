// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/pkg/deps"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/viper"
)

// Config holds the global configuration for the application.
type Config struct {
	Profile   string             `yaml:"profile" json:"profile"` // Deployment profile (local, perfnet, testnet, mainnet)
	Log       logx.LoggingConfig `yaml:"log" json:"log"`
	BlockNode BlockNodeConfig    `yaml:"blockNode" json:"blockNode"`
	Alloy     AlloyConfig        `yaml:"alloy" json:"alloy"`
	Teleport  TeleportConfig     `yaml:"teleport" json:"teleport"`
}

// BlockNodeStorage represents the `storage` section under `blockNode`.
type BlockNodeStorage struct {
	BasePath         string `yaml:"basePath" json:"basePath"`
	ArchivePath      string `yaml:"archivePath" json:"archivePath"`
	LivePath         string `yaml:"livePath" json:"livePath"`
	LogPath          string `yaml:"logPath" json:"logPath"`
	VerificationPath string `yaml:"verificationPath" json:"verificationPath"`
	LiveSize         string `yaml:"liveSize" json:"liveSize"`
	ArchiveSize      string `yaml:"archiveSize" json:"archiveSize"`
	LogSize          string `yaml:"logSize" json:"logSize"`
	VerificationSize string `yaml:"verificationSize" json:"verificationSize"`
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

	// Validate VerificationPath if provided
	if s.VerificationPath != "" {
		if _, err := sanity.SanitizePath(s.VerificationPath); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid verification path: %s", s.VerificationPath)
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

	if s.VerificationSize != "" {
		if err := sanity.ValidateStorageSize(s.VerificationSize); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid verification storage size: %s", s.VerificationSize)
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
	if err := c.Teleport.Validate(); err != nil {
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
// Note: Passwords are managed via Vault and External Secrets Operator, not in config files.
type AlloyConfig struct {
	MonitorBlockNode   bool   `yaml:"monitorBlockNode" json:"monitorBlockNode"`
	PrometheusURL      string `yaml:"prometheusUrl" json:"prometheusUrl"`
	PrometheusUsername string `yaml:"prometheusUsername" json:"prometheusUsername"`
	LokiURL            string `yaml:"lokiUrl" json:"lokiUrl"`
	LokiUsername       string `yaml:"lokiUsername" json:"lokiUsername"`
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

// TeleportConfig represents the `teleport` configuration block for secure access.
// Teleport configuration for node agent and cluster agent.
// Node agent: Uses NodeAgentToken and NodeAgentProxyAddr
// Cluster agent: Uses Version and ValuesFile (passed directly to Helm)
type TeleportConfig struct {
	Version            string `yaml:"version" json:"version"`                       // Helm chart version for cluster agent
	ValuesFile         string `yaml:"valuesFile" json:"valuesFile"`                 // Path to Helm values file for cluster agent
	NodeAgentToken     string `yaml:"nodeAgentToken" json:"nodeAgentToken"`         // Join token for host-level SSH agent
	NodeAgentProxyAddr string `yaml:"nodeAgentProxyAddr" json:"nodeAgentProxyAddr"` // Teleport proxy address (required when NodeAgentToken is set)
}

// Validate validates Teleport configuration fields that are set.
// This performs basic validation without context-specific requirements.
// Use ValidateClusterAgent() or ValidateNodeAgent() for use-case specific validation.
func (c TeleportConfig) Validate() error {
	// Validate ValuesFile path if provided
	if c.ValuesFile != "" {
		if _, err := sanity.SanitizePath(c.ValuesFile); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid teleport valuesFile path: %s", c.ValuesFile)
		}
	}

	// Validate NodeAgentProxyAddr if provided
	if c.NodeAgentProxyAddr != "" {
		if err := sanity.ValidateHostPort(c.NodeAgentProxyAddr); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid teleport nodeAgentProxyAddr: %s", c.NodeAgentProxyAddr)
		}
	}

	// Note: NodeAgentToken is validated only when actually used (in ValidateNodeAgent)
	// to avoid exposing token validation errors when teleport is not being configured

	return nil
}

// ValidateClusterAgent validates configuration for the cluster agent.
func (c TeleportConfig) ValidateClusterAgent() error {
	// ValuesFile is required for cluster agent
	if c.ValuesFile == "" {
		return errorx.IllegalArgument.New("teleport valuesFile is required for cluster agent installation")
	}

	// Validate ValuesFile path
	if _, err := sanity.SanitizePath(c.ValuesFile); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid teleport valuesFile path: %s", c.ValuesFile)
	}

	return nil
}

// ValidateNodeAgent validates configuration for the node agent.
func (c TeleportConfig) ValidateNodeAgent() error {
	// Validate NodeAgentToken - required for node agent
	if c.NodeAgentToken == "" {
		return errorx.IllegalArgument.New("teleport nodeAgentToken is required for node agent installation")
	}

	if err := sanity.ValidateHexToken(c.NodeAgentToken); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid teleport nodeAgentToken (value redacted)")
	}

	// Validate NodeAgentProxyAddr if provided
	if c.NodeAgentProxyAddr != "" {
		if err := sanity.ValidateHostPort(c.NodeAgentProxyAddr); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid teleport nodeAgentProxyAddr: %s", c.NodeAgentProxyAddr)
		}
	}

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
		MonitorBlockNode:   false,
		PrometheusURL:      "",
		PrometheusUsername: "",
		LokiURL:            "",
		LokiUsername:       "",
		ClusterName:        "",
	},
	Teleport: TeleportConfig{
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
		globalConfig = Config{}
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

		if err := viper.Unmarshal(&globalConfig); err != nil {
			return errorx.IllegalFormat.Wrap(err, "failed to parse configuration").
				WithProperty(errorx.PropertyPayload(), path)
		}
	}

	return nil
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

// SetProfile sets the deployment profile in the global configuration.
func SetProfile(profile string) {
	globalConfig.Profile = profile
}

// IsLocalProfile returns true if the current profile is the local development profile.
func (c Config) IsLocalProfile() bool {
	return c.Profile == core.ProfileLocal
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
// Note: Passwords are managed via Vault and External Secrets Operator.
func OverrideAlloyConfig(overrides AlloyConfig) {
	globalConfig.Alloy.MonitorBlockNode = overrides.MonitorBlockNode
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
	if overrides.ClusterName != "" {
		globalConfig.Alloy.ClusterName = overrides.ClusterName
	}
}

// OverrideTeleportConfig updates the Teleport configuration with provided overrides.
// Empty string values are ignored (not applied).
func OverrideTeleportConfig(overrides TeleportConfig) {
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
