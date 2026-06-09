// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const DefaultDaemonConfigPath = "/opt/solo/weaver/config/daemon.yaml"

// DaemonConfig is parsed from daemon.yaml at startup. Written by solo-provisioner
// at cluster install time after RBAC for each component is provisioned.
//
// Example daemon.yaml:
//
//	components:
//	  consensus_node:
//	    enabled: true
//	    kubeconfig: /opt/solo/weaver/config/daemon-cn.kubeconfig
//	    node_id: 0.0.3
//	    orbit: hedera-network
//	    upgrade_dir: /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
//	    monitors:
//	      upgrade: true
//	      migration: true
type DaemonConfig struct {
	Components DaemonComponents `yaml:"components"`
}

// DaemonComponents holds the per-component configuration blocks. Only
// consensus_node is implemented; block_node and ofac_filter are reserved for
// future stories (S7+).
type DaemonComponents struct {
	ConsensusNode *ConsensusNodeComponentConfig `yaml:"consensus_node,omitempty"`
}

// ConsensusNodeComponentConfig is the configuration block for the consensus-node
// component. It carries its own kubeconfig so its RBAC is independent of any
// future block-node or host-only component.
type ConsensusNodeComponentConfig struct {
	// Enabled controls whether this component and its monitors are started.
	Enabled bool `yaml:"enabled"`

	// Kubeconfig is the path to the scoped kubeconfig for this component's SA.
	// Written by solo-provisioner during daemon install (WriteConsensusNodeKubeconfigStep).
	Kubeconfig string `yaml:"kubeconfig"`

	// NodeID is the Hedera node identifier (e.g. "0.0.3"). Used as nodeId in
	// all JSONL event log entries.
	NodeID string `yaml:"node_id"`

	// Orbit is the Kubernetes namespace where NetworkUpgradeExecute CRs are watched.
	Orbit string `yaml:"orbit"`

	// UpgradeDir is the path to the CN's upgrade staging directory.
	// Defaults to /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current.
	UpgradeDir string `yaml:"upgrade_dir,omitempty"`

	Monitors ConsensusNodeMonitors `yaml:"monitors"`
}

// ConsensusNodeMonitors toggles individual monitors for the consensus-node component.
type ConsensusNodeMonitors struct {
	Upgrade   bool `yaml:"upgrade"`
	Migration bool `yaml:"migration"`
}

// EffectiveUpgradeDir returns UpgradeDir if set, otherwise the CN default path.
func (cn ConsensusNodeComponentConfig) EffectiveUpgradeDir() string {
	if cn.UpgradeDir != "" {
		return cn.UpgradeDir
	}
	return "/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current"
}

// Validate checks that all required fields within the consensus-node block are present.
func (cn ConsensusNodeComponentConfig) Validate() error {
	if cn.NodeID == "" {
		return ErrConfigMalformed.New("components.consensus_node.node_id is required")
	}
	if cn.Kubeconfig == "" {
		return ErrConfigMalformed.New("components.consensus_node.kubeconfig is required")
	}
	if cn.Orbit == "" {
		return ErrConfigMalformed.New("components.consensus_node.orbit is required")
	}
	return nil
}

// Validate checks that the config is structurally valid. Called by
// LoadDaemonConfig and by cmd/daemon/main.go after applying CLI flag overrides.
func (c DaemonConfig) Validate() error {
	if c.Components.ConsensusNode == nil {
		return ErrConfigMalformed.New("components.consensus_node is required")
	}
	return c.Components.ConsensusNode.Validate()
}

// WriteDaemonConfig serialises cfg to YAML and writes it to path, creating any
// missing parent directories. It does not validate cfg — callers should call
// cfg.Validate() before writing.
func WriteDaemonConfig(path string, cfg DaemonConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ErrConfig.Wrap(err, "cannot create config directory %s", filepath.Dir(path))
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return ErrConfigMalformed.Wrap(err, "cannot serialise daemon config")
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return ErrConfig.Wrap(err, "cannot write daemon config to %s", path)
	}
	return nil
}

// LoadDaemonConfig reads and parses the daemon config file at path.
// Returns an error if the file is missing or malformed — the daemon must not
// start without a valid config. CLI flag overrides are applied after this call
// in cmd/daemon/main.go; call Validate() again after overrides are applied.
func LoadDaemonConfig(path string) (DaemonConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DaemonConfig{}, ErrConfigNotFound.Wrap(err, "daemon config not found at %s", path)
	}

	var cfg DaemonConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return DaemonConfig{}, ErrConfigMalformed.Wrap(err, "invalid daemon config at %s", path)
	}

	if err := cfg.Validate(); err != nil {
		return DaemonConfig{}, ErrConfigMalformed.Wrap(err, "daemon config %s", path)
	}

	return cfg, nil
}
