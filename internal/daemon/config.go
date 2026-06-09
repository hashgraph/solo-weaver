// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const DefaultDaemonConfigPath = "/opt/solo/weaver/config/daemon.yaml"

// DaemonConfig is parsed from daemon.yaml at startup. Written by solo-provisioner
// at cluster install time after RBAC for the daemon is provisioned.
//
// Example daemon.yaml:
//
//	node_id:     0.0.3
//	kubeconfig:  /opt/solo/weaver/sandbox/etc/weaver/kubeconfig
//	orbit:       hedera-network
//	upgrade_dir: /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
type DaemonConfig struct {
	// NodeID is the Hedera node identifier for the consensus node managed by this
	// daemon (e.g. "0.0.3"). Used as nodeId in all JSONL event log entries.
	NodeID string `yaml:"node_id"`

	// Kubeconfig is the path to the scoped kubeconfig used by the daemon for all
	// Kubernetes API calls. Written by solo-provisioner; distinct from the CLI's
	// cluster-admin kubeconfig at ~/.kube/config.
	Kubeconfig string `yaml:"kubeconfig"`

	// Orbit is the Kubernetes namespace where NetworkUpgradeExecute CRs are watched.
	// Corresponds to the orbit that owns the consensus node managed by this daemon.
	Orbit string `yaml:"orbit"`

	// UpgradeDir is the path to the CN's upgrade staging directory, where the
	// consensus node Java process downloads and extracts the build.zip package.
	// The daemon reads manifests (infrastructure-versions.yaml, etc.) from this
	// location and may also move or place files into the broader
	// /opt/hgcapp/services-hedera/HapiApp2.0/ directory tree during infra
	// upgrades. The weaver/daemon user has write access to that tree.
	// The daemon does not download or extract the build.zip package itself.
	// Defaults to /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current if empty.
	UpgradeDir string `yaml:"upgrade_dir"`
}

// upgradeDir returns the effective upgrade staging directory: the configured
// value if non-empty, otherwise the CN Java process's well-known default path.
func (c DaemonConfig) upgradeDir() string {
	if c.UpgradeDir != "" {
		return c.UpgradeDir
	}
	return "/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current"
}

// Validate checks that all required fields are present. Called by
// LoadDaemonConfig and by cmd/daemon/main.go after applying CLI flag overrides.
func (c DaemonConfig) Validate() error {
	if c.NodeID == "" {
		return ErrConfigMalformed.New("node_id is required")
	}
	if c.Kubeconfig == "" {
		return ErrConfigMalformed.New("kubeconfig is required")
	}
	if c.Orbit == "" {
		return ErrConfigMalformed.New("orbit is required")
	}
	return nil
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
