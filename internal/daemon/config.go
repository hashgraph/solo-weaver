// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"os"

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

// LoadDaemonConfig reads and parses the daemon config file at path.
// Returns an error if the file is missing or malformed — the daemon must not
// start without a valid config.
func LoadDaemonConfig(path string) (DaemonConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DaemonConfig{}, ErrConfigNotFound.Wrap(err, "daemon config not found at %s", path)
	}

	var cfg DaemonConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return DaemonConfig{}, ErrConfigMalformed.Wrap(err, "invalid daemon config at %s", path)
	}

	if cfg.NodeID == "" {
		return DaemonConfig{}, ErrConfigMalformed.New("daemon config %s: node_id is required", path)
	}
	if cfg.Kubeconfig == "" {
		return DaemonConfig{}, ErrConfigMalformed.New("daemon config %s: kubeconfig is required", path)
	}
	if cfg.Orbit == "" {
		return DaemonConfig{}, ErrConfigMalformed.New("daemon config %s: orbit is required", path)
	}

	return cfg, nil
}
