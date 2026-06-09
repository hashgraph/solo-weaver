// SPDX-License-Identifier: Apache-2.0

package models

import (
	"path"
)

type WeaverPaths struct {
	HomeDir              string
	BinDir               string
	LogsDir              string
	UtilsDir             string
	ConfigDir            string
	BackupDir            string
	TempDir              string
	DownloadsDir         string
	DiagnosticsDir       string
	StateDir             string
	DaemonDir            string
	DaemonSockPath       string
	DaemonConfigPath     string
	DaemonKubeconfigPath string // /opt/solo/weaver/config/daemon.kubeconfig

	// InfraVersionsPath is the optional infrastructure-versions.yaml manifest dropped
	// into the config directory by the build.zip deployment package (HIP XXXX0).
	// When present, solo-provisioner validates its declared versions against the
	// embedded catalog and uses its provisioner.daemon section to drive daemon install.
	// Path: /opt/solo/weaver/config/infrastructure-versions.yaml
	InfraVersionsPath string

	// DaemonServiceSandboxPath is the canonical unit file location inside the
	// weaver sandbox: $home/sandbox/usr/lib/systemd/system/solo-provisioner-daemon.service
	// DaemonServiceSymlinkPath is the system-wide symlink that points to it:
	// /usr/lib/systemd/system/solo-provisioner-daemon.service
	DaemonServiceSandboxPath string
	DaemonServiceSymlinkPath string

	DaemonEventsDir string // $home/daemon/events

	// Consensus event subdirectories — scoped so future components (block-node, relay) get sibling dirs.
	DaemonConsensusEventsDir        string // $home/daemon/events/consensus
	DaemonConsensusUpgradeEventsDir string // $home/daemon/events/consensus/upgrade
	DaemonConsensusMigrateEventsDir string // $home/daemon/events/consensus/migrate

	DaemonConsensusMigrateEventsPath string // $home/daemon/events/consensus/migrate/consensus-migrate-events.jsonl

	AllDirectories []string

	// Sandbox directories for isolated binaries

	SandboxDir         string
	SandboxBinDir      string
	SandboxLocalBinDir string
	SandboxDirectories []string // all sandbox related directories
}

func NewWeaverPaths(home string) *WeaverPaths {
	pp := &WeaverPaths{
		HomeDir:        home,
		BinDir:         path.Join(home, "bin"),
		LogsDir:        path.Join(home, "logs"),
		ConfigDir:      path.Join(home, "config"),
		UtilsDir:       path.Join(home, "utils"),
		BackupDir:      path.Join(home, "backup"),
		TempDir:        path.Join(home, "tmp"),
		DownloadsDir:   path.Join(home, "downloads"),
		DiagnosticsDir: path.Join(home, "tmp", "diagnostics"),
		StateDir:       path.Join(home, "state"),
		DaemonDir:      path.Join(home, "daemon"),
	}

	pp.DaemonSockPath = path.Join(pp.DaemonDir, "daemon.sock")
	pp.DaemonConfigPath = path.Join(pp.ConfigDir, "daemon.yaml")
	pp.InfraVersionsPath = path.Join(pp.ConfigDir, "infrastructure-versions.yaml")
	pp.DaemonKubeconfigPath = path.Join(pp.ConfigDir, "daemon.kubeconfig")
	pp.DaemonEventsDir = path.Join(pp.DaemonDir, "events")
	pp.DaemonConsensusEventsDir = path.Join(pp.DaemonEventsDir, "consensus")
	pp.DaemonConsensusUpgradeEventsDir = path.Join(pp.DaemonConsensusEventsDir, "upgrade")
	pp.DaemonConsensusMigrateEventsDir = path.Join(pp.DaemonConsensusEventsDir, "migrate")
	pp.DaemonConsensusMigrateEventsPath = path.Join(pp.DaemonConsensusMigrateEventsDir, "consensus-migrate-events.jsonl")

	pp.SandboxDir = path.Join(pp.HomeDir, "sandbox")
	pp.SandboxBinDir = path.Join(pp.SandboxDir, "bin")
	pp.SandboxLocalBinDir = path.Join(pp.SandboxDir, "usr", "local", "bin")

	pp.DaemonServiceSandboxPath = path.Join(pp.SandboxDir, "usr", "lib", "systemd", "system", "solo-provisioner-daemon.service")
	pp.DaemonServiceSymlinkPath = "/usr/lib/systemd/system/solo-provisioner-daemon.service"

	pp.SandboxDirectories = []string{
		pp.SandboxDir,
		pp.SandboxBinDir,
		pp.SandboxLocalBinDir,
		path.Join(pp.SandboxDir, "etc/crio/keys"),
		path.Join(pp.SandboxDir, "etc/default"),
		path.Join(pp.SandboxDir, "etc/sysconfig"),
		path.Join(pp.SandboxDir, "etc/weaver"),
		path.Join(pp.SandboxDir, "etc/containers/registries.conf.d"),
		path.Join(pp.SandboxDir, "etc/cni/net.d"),
		path.Join(pp.SandboxDir, "etc/nri/conf.d"),
		path.Join(pp.SandboxDir, "etc/kubernetes/pki"),
		path.Join(pp.SandboxDir, "var/lib/etcd"),
		path.Join(pp.SandboxDir, "var/lib/containers/storage"),
		path.Join(pp.SandboxDir, "var/lib/kubelet"),
		path.Join(pp.SandboxDir, "var/lib/crio"),
		path.Join(pp.SandboxDir, "var/run/cilium"),
		path.Join(pp.SandboxDir, "var/run/nri"),
		path.Join(pp.SandboxDir, "var/run/containers/storage"),
		path.Join(pp.SandboxDir, "var/run/crio/exits"),
		path.Join(pp.SandboxDir, "var/logs/crio/pods"),
		path.Join(pp.SandboxDir, "run/runc"),
		path.Join(pp.SandboxDir, "usr/libexec/crio"),
		path.Join(pp.SandboxDir, "usr/lib/systemd/system"),
		path.Join(pp.SandboxDir, "usr/lib/systemd/system/kubelet.service.d"),
		path.Join(pp.SandboxDir, "usr/local/share/man"),
		path.Join(pp.SandboxDir, "usr/local/share/oci-umount/oci-umount.d"),
		path.Join(pp.SandboxDir, "usr/local/share/bash-completion/completions"),
		path.Join(pp.SandboxDir, "usr/local/share/fish/completions"),
		path.Join(pp.SandboxDir, "usr/local/share/zsh/site-functions"),
		path.Join(pp.SandboxDir, "opt/cni/bin"),
		path.Join(pp.SandboxDir, "opt/nri/plugins"),
	}

	// populate AllDirectories
	pp.AllDirectories = []string{
		pp.HomeDir,
		pp.BinDir,
		pp.LogsDir,
		pp.UtilsDir,
		pp.ConfigDir,
		pp.BackupDir,
		pp.TempDir,
		pp.DownloadsDir,
		pp.DiagnosticsDir,
		pp.StateDir,
		pp.DaemonDir,
		pp.DaemonEventsDir,
		pp.DaemonConsensusEventsDir,
		pp.DaemonConsensusUpgradeEventsDir,
		pp.DaemonConsensusMigrateEventsDir,
	}
	pp.AllDirectories = append(pp.AllDirectories, pp.SandboxDirectories...)

	return pp
}

// Clone returns a deep copy of the WeaverPaths.
// It is nil-safe and copies slice contents to avoid shared backing arrays.
func (w WeaverPaths) Clone() *WeaverPaths {
	// shallow copy of struct fields (strings are value-copied)
	cp := w

	// deep-copy slices to avoid sharing backing arrays
	if w.AllDirectories != nil {
		cp.AllDirectories = make([]string, len(w.AllDirectories))
		copy(cp.AllDirectories, w.AllDirectories)
	}
	if w.SandboxDirectories != nil {
		cp.SandboxDirectories = make([]string, len(w.SandboxDirectories))
		copy(cp.SandboxDirectories, w.SandboxDirectories)
	}
	return &cp
}
