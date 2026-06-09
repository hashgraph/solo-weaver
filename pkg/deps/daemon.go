// SPDX-License-Identifier: Apache-2.0

package deps

import (
	"embed"
	"sync"

	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

//go:embed daemon_config.yaml
var daemonConfigFS embed.FS

// DaemonReleaseSpec is the release metadata for the solo-provisioner-daemon binary.
// It is embedded at build time from pkg/deps/daemon_config.yaml and is the single
// source of truth for auto-download and integrity verification.
type DaemonReleaseSpec struct {
	// Version is the semver of the daemon binary, matching the daemon-vX.Y.Z release tag.
	Version string `yaml:"version"`
	// Algorithm is the checksum algorithm used to verify the binary (e.g. "sha256").
	Algorithm string `yaml:"algorithm"`
	// Checksum is the expected hex digest of the binary at DownloadURL.
	Checksum string `yaml:"checksum"`
	// Commit is the git commit SHA embedded in the binary (reported by `--version`).
	Commit string `yaml:"commit"`
	// DownloadURL is the direct asset URL on GitHub Releases.
	DownloadURL string `yaml:"download_url"`
}

type daemonConfigFile struct {
	Daemon DaemonReleaseSpec `yaml:"daemon"`
}

var (
	daemonSpecOnce  sync.Once
	daemonSpecCache *DaemonReleaseSpec
	daemonSpecErr   error
)

// LoadDaemonReleaseSpec returns the embedded daemon release metadata.
// The result is cached after the first call; the embedded YAML is never
// re-read from disk at runtime.
func LoadDaemonReleaseSpec() (*DaemonReleaseSpec, error) {
	daemonSpecOnce.Do(func() {
		data, err := daemonConfigFS.ReadFile("daemon_config.yaml")
		if err != nil {
			daemonSpecErr = errorx.InternalError.Wrap(err, "failed to read embedded daemon config")
			return
		}
		var cfg daemonConfigFile
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			daemonSpecErr = errorx.InternalError.Wrap(err, "failed to parse embedded daemon config")
			return
		}
		if cfg.Daemon.Version == "" {
			daemonSpecErr = errorx.IllegalFormat.New("embedded daemon config: version must not be empty")
			return
		}
		daemonSpecCache = &cfg.Daemon
	})
	return daemonSpecCache, daemonSpecErr
}
