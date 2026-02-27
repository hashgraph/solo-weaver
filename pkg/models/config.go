// SPDX-License-Identifier: Apache-2.0

package models

import (
	"strings"

	"github.com/automa-saga/logx"

	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
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
	PluginsPath      string `yaml:"pluginsPath" json:"pluginsPath"`
	LiveSize         string `yaml:"liveSize" json:"liveSize"`
	ArchiveSize      string `yaml:"archiveSize" json:"archiveSize"`
	LogSize          string `yaml:"logSize" json:"logSize"`
	VerificationSize string `yaml:"verificationSize" json:"verificationSize"`
	PluginsSize      string `yaml:"pluginsSize" json:"pluginsSize"`
}

func (b *BlockNodeStorage) ToMap() map[string]string {
	return map[string]string{
		"basePath":         b.BasePath,
		"archivePath":      b.ArchivePath,
		"livePath":         b.LivePath,
		"logPath":          b.LogPath,
		"verificationPath": b.VerificationPath,
		"liveSize":         b.LiveSize,
		"archiveSize":      b.ArchiveSize,
		"logSize":          b.LogSize,
		"verificationSize": b.VerificationSize,
	}
}

// IsEmpty returns true when all BlockNodeStorage fields are empty (after trimming).
func (b *BlockNodeStorage) IsEmpty() bool {
	return strings.TrimSpace(b.BasePath) == "" &&
		strings.TrimSpace(b.ArchivePath) == "" &&
		strings.TrimSpace(b.LivePath) == "" &&
		strings.TrimSpace(b.LogPath) == "" &&
		strings.TrimSpace(b.LiveSize) == "" &&
		strings.TrimSpace(b.ArchiveSize) == "" &&
		strings.TrimSpace(b.LogSize) == ""
}

// Validate validates all storage paths to ensure they are safe and secure.
// This performs early validation of user-provided paths to catch security issues
// before workflow execution begins.
func (b *BlockNodeStorage) Validate() error {
	// Validate BasePath if provided
	if b.BasePath != "" {
		if _, err := sanity.SanitizePath(b.BasePath); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid base path: %s", b.BasePath)
		}
	}

	// Validate ArchivePath if provided
	if b.ArchivePath != "" {
		if _, err := sanity.SanitizePath(b.ArchivePath); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid archive path: %s", b.ArchivePath)
		}
	}

	// Validate LivePath if provided
	if b.LivePath != "" {
		if _, err := sanity.SanitizePath(b.LivePath); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid live path: %s", b.LivePath)
		}
	}

	// Validate LogPath if provided
	if b.LogPath != "" {
		if _, err := sanity.SanitizePath(b.LogPath); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid log path: %s", b.LogPath)
		}
	}

	// Validate VerificationPath if provided
	if b.VerificationPath != "" {
		if _, err := sanity.SanitizePath(b.VerificationPath); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid verification path: %s", b.VerificationPath)
		}
	}

	// Validate storage sizes if provided
	if b.LiveSize != "" {
		if err := sanity.ValidateStorageSize(b.LiveSize); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid live storage size: %s", b.LiveSize)
		}
	}

	if b.ArchiveSize != "" {
		if err := sanity.ValidateStorageSize(b.ArchiveSize); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid archive storage size: %s", b.ArchiveSize)
		}
	}

	if b.LogSize != "" {
		if err := sanity.ValidateStorageSize(b.LogSize); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid log storage size: %s", b.LogSize)
		}
	}

	if b.VerificationSize != "" {
		if err := sanity.ValidateStorageSize(b.VerificationSize); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid verification storage size: %s", b.VerificationSize)
		}
	}

	if s.PluginsSize != "" {
		if err := sanity.ValidateStorageSize(s.PluginsSize); err != nil {
			return errorx.IllegalArgument.Wrap(err, "invalid plugins storage size: %s", s.PluginsSize)
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
	Namespace    string           `yaml:"namespace" json:"namespace"`
	Release      string           `yaml:"release" json:"release"`
	Chart        string           `yaml:"chart" json:"chart"`
	ChartName    string           `yaml:"chartName" json:"chartName"`
	ChartVersion string           `yaml:"chartVersion" json:"chartVersion"`
	Version      string           `yaml:"version" json:"version"`
	Storage      BlockNodeStorage `yaml:"storage" json:"storage"`
}

// AlloyRemoteConfig represents a single remote endpoint for metrics or logs.
// Passwords are expected in K8s Secret "grafana-alloy-secrets" under conventional keys:
//   - Prometheus: PROMETHEUS_PASSWORD_<NAME>
//   - Loki: LOKI_PASSWORD_<NAME>
type AlloyRemoteConfig struct {
	Name     string `yaml:"name" json:"name"`         // Unique identifier for this remote
	URL      string `yaml:"url" json:"url"`           // Remote write URL
	Username string `yaml:"username" json:"username"` // Basic auth username
}

// AlloyConfig represents the `alloy` configuration block for observability.
// Note: Passwords are managed via Vault and External Secrets Operator, not in config files.
type AlloyConfig struct {
	MonitorBlockNode       bool                `yaml:"monitorBlockNode" json:"monitorBlockNode"`
	ClusterName            string              `yaml:"clusterName" json:"clusterName"`
	ClusterSecretStoreName string              `yaml:"clusterSecretStoreName" json:"clusterSecretStoreName"` // Name of the ClusterSecretStore for ESO
	PrometheusRemotes      []AlloyRemoteConfig `yaml:"prometheusRemotes" json:"prometheusRemotes"`
	LokiRemotes            []AlloyRemoteConfig `yaml:"lokiRemotes" json:"lokiRemotes"`
	// Deprecated: Use PrometheusRemotes instead. Kept for backward compatibility.
	PrometheusURL      string `yaml:"prometheusUrl" json:"prometheusUrl"`
	PrometheusUsername string `yaml:"prometheusUsername" json:"prometheusUsername"`
	// Deprecated: Use LokiRemotes instead. Kept for backward compatibility.
	LokiURL      string `yaml:"lokiUrl" json:"lokiUrl"`
	LokiUsername string `yaml:"lokiUsername" json:"lokiUsername"`
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

	// Validate Prometheus remotes
	prometheusNames := make(map[string]bool)
	for i, remote := range c.PrometheusRemotes {
		if remote.Name == "" {
			return errorx.IllegalArgument.New("prometheus remote[%d]: name is required", i)
		}
		if err := sanity.ValidateIdentifier(remote.Name); err != nil {
			return errorx.IllegalArgument.Wrap(err, "prometheus remote[%d]: invalid name: %s", i, remote.Name)
		}
		if prometheusNames[remote.Name] {
			return errorx.IllegalArgument.New("prometheus remote[%d]: duplicate name: %s", i, remote.Name)
		}
		prometheusNames[remote.Name] = true
		if remote.URL == "" {
			return errorx.IllegalArgument.New("prometheus remote[%d] (%s): url is required", i, remote.Name)
		}
		if err := sanity.ValidateURL(remote.URL, &sanity.ValidateURLOptions{AllowHTTP: true}); err != nil {
			return errorx.IllegalArgument.Wrap(err, "prometheus remote[%d] (%s): invalid url", i, remote.Name)
		}
		if remote.Username != "" {
			if err := sanity.ValidateIdentifier(remote.Username); err != nil {
				return errorx.IllegalArgument.Wrap(err, "prometheus remote[%d] (%s): invalid username", i, remote.Name)
			}
		}
	}

	// Validate Loki remotes
	lokiNames := make(map[string]bool)
	for i, remote := range c.LokiRemotes {
		if remote.Name == "" {
			return errorx.IllegalArgument.New("loki remote[%d]: name is required", i)
		}
		if err := sanity.ValidateIdentifier(remote.Name); err != nil {
			return errorx.IllegalArgument.Wrap(err, "loki remote[%d]: invalid name: %s", i, remote.Name)
		}
		if lokiNames[remote.Name] {
			return errorx.IllegalArgument.New("loki remote[%d]: duplicate name: %s", i, remote.Name)
		}
		lokiNames[remote.Name] = true
		if remote.URL == "" {
			return errorx.IllegalArgument.New("loki remote[%d] (%s): url is required", i, remote.Name)
		}
		if err := sanity.ValidateURL(remote.URL, &sanity.ValidateURLOptions{AllowHTTP: true}); err != nil {
			return errorx.IllegalArgument.Wrap(err, "loki remote[%d] (%s): invalid url", i, remote.Name)
		}
		if remote.Username != "" {
			if err := sanity.ValidateIdentifier(remote.Username); err != nil {
				return errorx.IllegalArgument.Wrap(err, "loki remote[%d] (%s): invalid username", i, remote.Name)
			}
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

// IsLocalProfile returns true if the current profile is the local development profile.
func (c Config) IsLocalProfile() bool {
	return c.Profile == ProfileLocal
}
