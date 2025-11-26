// SPDX-License-Identifier: Apache-2.0

package core

import (
	"path"

	"github.com/hashgraph/solo-weaver/pkg/security"
)

const (
	// File and directory permissions
	DefaultDirOrExecPerm = 0755 // for directories and executable files
	DefaultFilePerm      = 0644 // for regular data/config files

	// Weaver paths
	DefaultWeaverHome       = "/opt/solo/weaver"
	DefaultUnpackFolderName = "unpack"
	SystemBinDir            = "/usr/local/bin"
	SystemdUnitFilesDir     = "/usr/lib/systemd/system"

	// Node types
	NodeTypeLocal     = "local"
	NodeTypeBlock     = "block"
	NodeTypeConsensus = "consensus"
	NodeTypeMirror    = "mirror"
	NodeTypeRelay     = "relay"

	// Deployment profiles
	ProfileLocal   = "local"
	ProfilePerfnet = "perfnet"
	ProfileTestnet = "testnet"
	ProfileMainnet = "mainnet"
)

var allProfiles = []string{
	ProfileLocal,
	ProfilePerfnet,
	ProfileTestnet,
	ProfileMainnet,
}

func AllProfiles() []string {
	// return a copy to prevent modification
	profilesCopy := make([]string, len(allProfiles))
	copy(profilesCopy, allProfiles)
	return profilesCopy
}

var (
	pp     = NewWeaverPaths(DefaultWeaverHome)
	svcAcc = security.ServiceAccount{
		UserName:  "weaver",
		UserId:    "2500",
		GroupName: "weaver",
		GroupId:   "2500",
	}
)

func init() {
	security.SetServiceAccount(svcAcc)
}

type WeaverPaths struct {
	HomeDir        string
	BinDir         string
	LogsDir        string
	UtilsDir       string
	ConfigDir      string
	BackupDir      string
	TempDir        string
	DownloadsDir   string
	DiagnosticsDir string
	StateDir       string

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
	}

	pp.SandboxDir = path.Join(pp.HomeDir, "sandbox")
	pp.SandboxBinDir = path.Join(pp.SandboxDir, "bin")
	pp.SandboxLocalBinDir = path.Join(pp.SandboxDir, "usr", "local", "bin")

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
	}
	pp.AllDirectories = append(pp.AllDirectories, pp.SandboxDirectories...)

	return pp
}

// Clone returns a deep copy of the WeaverPaths.
// It is nil-safe and copies slice contents to avoid shared backing arrays.
func (w *WeaverPaths) Clone() *WeaverPaths {
	if w == nil {
		return nil
	}
	// shallow copy of struct fields (strings are value-copied)
	cp := *w

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

func Paths() *WeaverPaths {
	return pp
}

func ServiceAccount() security.ServiceAccount {
	return svcAcc
}
