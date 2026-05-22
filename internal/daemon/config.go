// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"os"

	"gopkg.in/yaml.v3"
)

const DefaultDaemonConfigPath = "/opt/solo/weaver/config/daemon.yaml"

// DaemonConfig is parsed from daemon.yaml at startup. Written by solo-provisioner
// at cluster install time after RBAC for the daemon is provisioned.
type DaemonConfig struct {
	// Kubeconfig is the path to the scoped kubeconfig used by the daemon for all
	// Kubernetes API calls. Written by solo-provisioner; distinct from the CLI's
	// cluster-admin kubeconfig at ~/.kube/config.
	Kubeconfig string `yaml:"kubeconfig"`

	// Orbit is the Kubernetes namespace where NetworkUpgradeExecute CRs are watched.
	// Corresponds to the orbit that owns the consensus node managed by this daemon.
	Orbit string `yaml:"orbit"`
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

	if cfg.Kubeconfig == "" {
		return DaemonConfig{}, ErrConfigMalformed.New("daemon config %s: kubeconfig is required", path)
	}
	if cfg.Orbit == "" {
		return DaemonConfig{}, ErrConfigMalformed.New("daemon config %s: orbit is required", path)
	}

	return cfg, nil
}
