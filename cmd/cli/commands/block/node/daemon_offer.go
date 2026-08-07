// SPDX-License-Identifier: Apache-2.0

package node

import (
	"os"
	"path/filepath"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	workflowsteps "github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	pkgos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/hashgraph/solo-weaver/pkg/semver"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// daemonServiceName is the systemd unit for the solo-provisioner daemon; matched
// against systemd's active state to decide whether the daemon is already running.
const daemonServiceName = "solo-provisioner-daemon"

// ensureBlockNodeDaemon closes the gap between installing a block node and
// getting its traffic-shaper daemon running. Daemon activation is not a
// separate decision from the operator: it is part of the traffic-shaping
// bundle. install.go calls this only when TrafficShapingEnabled is true — a
// block node with the policy plane and tc shaping in place, but no daemon,
// would otherwise have no ingress prioritization (the veth HTB is never
// installed) and nothing reconciling the daemon-owned nft sets from statusz.
//
// When the daemon is already active there is nothing to do; otherwise it is
// installed and provisioned (RBAC + daemon-bn.kubeconfig + systemd unit)
// scoped to this block node's namespace, unconditionally — no prompt, no flag.
// source controls where the daemon binary itself comes from (see
// resolveDaemonBinarySource).
func ensureBlockNodeDaemon(cmd *cobra.Command, namespace string, source workflowsteps.DaemonBinarySource) error {
	ctx := cmd.Context()

	// A running daemon needs no further action (the check is a no-op on hosts
	// where the daemon is already up). A systemd/DBus error is non-fatal — treat
	// the daemon as not running — but log it so an operator can tell a genuine
	// "not installed" from a failed state query.
	running, err := pkgos.IsServiceRunning(ctx, daemonServiceName)
	if err != nil {
		logx.As().Warn().Err(err).Msg(
			"could not determine solo-provisioner-daemon service state; treating it as not running")
	}
	if running {
		return nil
	}

	return provisionBlockNodeDaemon(cmd, namespace, source)
}

// envDaemonBin is the environment-variable equivalent of --daemon-bin, for dev
// loops that would otherwise repeat the flag on every invocation.
const envDaemonBin = "SOLO_PROVISIONER_DAEMON_BIN"

// resolveDaemonBinarySource determines where ensureBlockNodeDaemon should get the
// daemon binary from, in descending precedence:
//
//  1. --daemon-bin — an explicit operator override; bypasses the catalog entirely.
//  2. SOLO_PROVISIONER_DAEMON_BIN — the same override without repeating the flag.
//  3. A daemon binary already installed at paths.BinDir. CLI self-install puts the
//     co-built binary there (see steps.InstallColocatedDaemonBinary), so a host
//     provisioned from a local build already has a matching daemon to install.
//  4. Auto-download from the infrastructure catalog, at --daemon-version (which
//     defaults to this CLI's own version — the CLI and daemon are co-released
//     under one tag, so a released build always resolves to a real artifact).
//
// An exhausted list is an error, never a prompt — which is what lets `upgrade`
// share this resolver despite its silent-convergence contract.
func resolveDaemonBinarySource(cmd *cobra.Command) (workflowsteps.DaemonBinarySource, error) {
	source := workflowsteps.DaemonBinarySource{BinPath: flagDaemonBin, Version: flagDaemonVersion}
	if source.BinPath != "" {
		return source, nil
	}

	// ensureBlockNodeDaemon returns early when the service is up, so any source
	// resolved here would be discarded. Don't fail for the want of one.
	if running, err := pkgos.IsServiceRunning(cmd.Context(), daemonServiceName); err == nil && running {
		return source, nil
	}

	if envPath := os.Getenv(envDaemonBin); envPath != "" {
		logx.As().Info().Str("path", envPath).Str("env", envDaemonBin).
			Msg("Using the daemon binary from the environment")
		source.BinPath = envPath
		return source, nil
	}

	if installed := installedDaemonBinaryPath(); installed != "" {
		logx.As().Info().Str("path", installed).
			Msg("Using the daemon binary already present on this host")
		source.BinPath = installed
		return source, nil
	}

	// An empty BinPath selects the catalog download.
	if isDownloadableDaemonVersion(cmd, source.Version) {
		return source, nil
	}

	return source, unresolvedDaemonBinaryError()
}

// unresolvedDaemonBinaryError reports that every source in resolveDaemonBinarySource's
// precedence list came up empty, and lists the ways an operator can supply one.
func unresolvedDaemonBinaryError() error {
	return errorx.IllegalArgument.New(
		"no solo-provisioner-daemon binary could be resolved: this build's version (%s) has no "+
			"downloadable release and no binary is installed at %s",
		flagDaemonVersion, models.Paths().BinDir).
		WithProperty(models.ErrPropertyResolution, []string{
			"Build the daemon locally: task build:daemon GOOS=linux GOARCH=<arch>",
			"Then either re-run the self-install so it is picked up automatically:",
			"  sudo bin/solo-provisioner-linux-<arch> install",
			"or pass the path explicitly: --daemon-bin=<path-to-binary>",
			"or set " + envDaemonBin + "=<path-to-binary>",
			"For a released build, pass a published version: --daemon-version=<x.y.z>",
		})
}

// installedDaemonBinaryPath returns the path of an executable daemon binary
// already installed at paths.BinDir, or "" if there is none. Only stat'ed: this
// path is the install destination, so InstallDaemonBinaryStep short-circuits
// ahead of both its platform probe and the copy.
//
// This deliberately does not go through software.Installer.IsInstalled(): that
// gates on a recorded install-manifest entry, which a binary placed by CLI
// self-install does not have.
func installedDaemonBinaryPath() string {
	p := filepath.Join(models.Paths().BinDir, software.DaemonBinaryName)
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return ""
	}
	return p
}

// isDownloadableDaemonVersion reports whether v can plausibly resolve to a
// published release, i.e. whether the catalog auto-download path is worth trying.
//
// An explicitly-supplied --daemon-version is always trusted: the operator named a
// version, so let the download attempt produce the error rather than second-guessing
// it here. Otherwise v is this build's own stamped version, and the placeholders
// that unstamped and locally-built binaries carry — "dev" from the version package's
// default and 0.0.0 from the Taskfile's VERSION default — have no release to fetch.
func isDownloadableDaemonVersion(cmd *cobra.Command, v string) bool {
	// cmd.Flag resolves through the persistent set too — SetVarP registers these on
	// PersistentFlags, which cmd.Flags() only reflects after cobra's pre-Execute merge.
	if f := cmd.Flag(common.FlagDaemonVersion().Name); f != nil && f.Changed {
		return true
	}
	if v == "" || v == devVersionPlaceholder {
		return false
	}
	parsed, err := semver.NewSemver(v)
	if err != nil {
		return false
	}
	return !parsed.EqualTo(zeroVersion)
}

// devVersionPlaceholder is what github.com/automa-saga/version reports for a build
// that was not stamped via -ldflags (`go run`, a plain `go build`).
const devVersionPlaceholder = "dev"

// zeroVersion is the Taskfile's default VERSION, carried by every local `task build`
// binary. No v0.0.0 release exists or ever will.
var zeroVersion = func() semver.Semver {
	v, _ := semver.NewSemver("0.0.0")
	return v
}()

// provisionBlockNodeDaemon runs the daemon install + provisioning workflow for
// the block-node component. daemon.yaml has already been written by the install
// workflow's Traffic-shaper Monitor phase, so it is loaded here as the source of
// truth; the block-node component's orbit is defaulted to this install's
// namespace if not already set (never re-prompted).
func provisionBlockNodeDaemon(cmd *cobra.Command, namespace string, source workflowsteps.DaemonBinarySource) error {
	paths := models.Paths()

	cfg, err := daemon.LoadDaemonConfig(paths.DaemonConfigPath)
	if err != nil {
		// A missing daemon.yaml is expected on some paths — fall back to a fresh
		// config and provision for this namespace. A malformed (or otherwise
		// unreadable) daemon.yaml is fatal: swallowing it would provision the
		// daemon while the operator's broken config silently goes unnoticed.
		if !daemon.IsConfigNotFound(err) {
			return err
		}
		cfg = daemon.DaemonConfig{}
	}
	// This path is an explicit request to install/provision the block-node
	// daemon, so force the block-node component and its traffic-shaper monitor
	// on regardless of any pre-existing disabled state — otherwise
	// NewDaemonServiceInstallWorkflow would skip BN RBAC/kubeconfig and the
	// monitor would never run.
	if cfg.Components.BlockNode == nil {
		cfg.Components.BlockNode = &daemon.BlockNodeComponentConfig{}
	}
	cfg.Components.BlockNode.Enabled = true
	cfg.Components.BlockNode.Monitors.TrafficShaper = true
	if cfg.Components.BlockNode.Kubeconfig == "" {
		cfg.Components.BlockNode.Kubeconfig = paths.DaemonBNKubeconfigPath
	}
	if cfg.Components.BlockNode.Orbit == "" {
		cfg.Components.BlockNode.Orbit = namespace
	}

	wf, err := workflows.NewDaemonServiceInstallWorkflow(cfg, source)
	if err != nil {
		return err
	}
	if err := common.RunWorkflowBuilder(cmd.Context(), wf); err != nil {
		return err
	}
	logx.As().Info().Msg("solo-provisioner-daemon service installed, enabled, and started")
	return nil
}
