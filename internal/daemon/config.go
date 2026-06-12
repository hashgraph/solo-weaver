// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DefaultDaemonConfigPath = "/opt/solo/weaver/config/daemon.yaml"

	// CurrentSchemaVersion is the schema version written by this build.
	// Increment this constant whenever a breaking structural change is made to
	// DaemonConfig so that LoadDaemonConfig can detect and migrate old files.
	CurrentSchemaVersion = 1
)

// DaemonConfig is parsed from daemon.yaml at startup.
//
// Example daemon.yaml:
//
//	schema_version: 1
//	components:
//	  consensus_node:
//	    enabled: true
//	    kubeconfig: /opt/solo/weaver/config/daemon-cn.kubeconfig
//	    node_id: 0
//	    orbit: hedera-network
//	    upgrade_dir: /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
//	    monitors:
//	      upgrade: true
//	      migration: true
//	  block_node:
//	    enabled: true
//	    kubeconfig: /opt/solo/weaver/config/daemon-bn.kubeconfig
//	    orbit: hedera-block-node
//	    monitors:
//	      upgrade: true
type DaemonConfig struct {
	// SchemaVersion identifies the config file format. Always written as
	// CurrentSchemaVersion by WriteDaemonConfig. A value of 0 means the file
	// predates schema versioning and is treated as version 1 for compatibility.
	SchemaVersion int `yaml:"schema_version"`

	Components DaemonComponents `yaml:"components"`
}

// DaemonComponents holds the per-component configuration blocks.
type DaemonComponents struct {
	ConsensusNode *ConsensusNodeComponentConfig `yaml:"consensus_node,omitempty"`
	BlockNode     *BlockNodeComponentConfig     `yaml:"block_node,omitempty"`
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

	// NodeID is the numeric node identifier (e.g. "0", "1", "2"). Used as nodeId in
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

// BlockNodeComponentConfig is the configuration block for the block-node component.
// It carries its own kubeconfig so its RBAC is independent of the consensus-node.
type BlockNodeComponentConfig struct {
	// Enabled controls whether this component and its monitors are started.
	Enabled bool `yaml:"enabled"`

	// Kubeconfig is the path to the scoped kubeconfig for this component's SA.
	// Written by solo-provisioner during daemon install.
	Kubeconfig string `yaml:"kubeconfig"`

	// Orbit is the Kubernetes namespace where block-node CRs are watched.
	Orbit string `yaml:"orbit"`

	Monitors BlockNodeMonitors `yaml:"monitors"`
}

// BlockNodeMonitors toggles individual monitors for the block-node component.
type BlockNodeMonitors struct {
	TrafficShaper bool `yaml:"traffic_shaper"`
}

// Validate checks that all required fields within the block-node block are present.
// Kubeconfig and Orbit are optional while the traffic-shaper monitor is stubbed
// (it polls a remote API and does not watch K8s resources).
func (bn BlockNodeComponentConfig) Validate() error {
	return nil
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
// At least one component (consensus_node or block_node) must be present.
func (c DaemonConfig) Validate() error {
	if c.Components.ConsensusNode == nil && c.Components.BlockNode == nil {
		return ErrConfigMalformed.New("at least one component (consensus_node or block_node) is required")
	}
	if cn := c.Components.ConsensusNode; cn != nil {
		if err := cn.Validate(); err != nil {
			return err
		}
	}
	if bn := c.Components.BlockNode; bn != nil {
		if err := bn.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// WriteDaemonConfig serialises cfg to YAML and writes it to path, creating any
// missing parent directories. It stamps SchemaVersion = CurrentSchemaVersion
// before writing. Callers should call cfg.Validate() before writing.
func WriteDaemonConfig(path string, cfg DaemonConfig) error {
	cfg.SchemaVersion = CurrentSchemaVersion
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ErrConfig.Wrap(err, "cannot create config directory %s", filepath.Dir(path))
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return ErrConfigMalformed.Wrap(err, "cannot serialise daemon config")
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ErrConfig.Wrap(err, "cannot write daemon config to %s", path)
	}
	return nil
}

// LoadDaemonConfig reads and parses the daemon config file at path.
// Returns an error if the file is missing or malformed — the daemon must not
// start without a valid config. CLI flag overrides are applied after this call
// in cmd/daemon/main.go; call Validate() again after overrides are applied.
//
// Loading uses a two-phase approach:
//  1. Probe: unmarshal only schema_version to determine the on-disk format.
//  2. Parse + migrate: unmarshal into the versioned struct for that version,
//     then walk the migration chain (vN.migrateToLatest()) to produce the
//     current DaemonConfig. Each step in the chain is a pure field transform
//     that knows only about one version transition.
//
// Version rules:
//   - 0 (absent): pre-versioning file; treated as version 1.
//   - 1..CurrentSchemaVersion: accepted; migrated to current if needed.
//   - > CurrentSchemaVersion: rejected — written by a newer binary.
func LoadDaemonConfig(path string) (DaemonConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DaemonConfig{}, ErrConfigNotFound.Wrap(err, "daemon config not found at %s", path)
	}

	// Phase 1: probe the schema version only.
	var probe struct {
		SchemaVersion int `yaml:"schema_version"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return DaemonConfig{}, ErrConfigMalformed.Wrap(err, "invalid daemon config at %s", path)
	}
	version := probe.SchemaVersion
	if version == 0 {
		version = 1 // pre-versioning file; treat as v1
	}
	if version > CurrentSchemaVersion {
		return DaemonConfig{}, ErrConfigMalformed.New(
			"daemon config %s was written by a newer binary (schema_version %d > supported %d); "+
				"upgrade solo-provisioner-daemon to a compatible version",
			path, version, CurrentSchemaVersion)
	}

	// Phase 2: unmarshal into the versioned struct and walk the migration chain.
	// To add support for a new version N:
	//   1. Add daemonConfigVN in config_vN.go with migrateToLatest() and migrate().
	//   2. Add case N here.
	//   3. Bump CurrentSchemaVersion.
	var cfg DaemonConfig
	switch version {
	case 1:
		var raw daemonConfigV1
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return DaemonConfig{}, ErrConfigMalformed.Wrap(err, "invalid v1 daemon config at %s", path)
		}
		cfg = raw.migrateToLatest()
	default:
		// Should never reach here given the version > CurrentSchemaVersion guard above.
		return DaemonConfig{}, ErrConfigMalformed.New(
			"unsupported daemon config schema_version %d at %s", version, path)
	}

	if err := cfg.Validate(); err != nil {
		return DaemonConfig{}, ErrConfigMalformed.Wrap(err, "daemon config %s", path)
	}

	return cfg, nil
}
