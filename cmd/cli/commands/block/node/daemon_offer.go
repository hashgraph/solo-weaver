// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	workflowsteps "github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	pkgos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
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

// resolveDaemonBinarySource determines where ensureBlockNodeDaemon should get
// the daemon binary from: --daemon-bin (bypasses the catalog entirely) or
// --daemon-version (selects which catalog version to auto-download; defaults
// to this CLI's own version — meaningful once CLI and daemon are co-released
// under one version scheme).
//
// --profile=local is a special case: local/dev builds report an unstamped
// version (e.g. "dev") that has no corresponding catalog entry, so
// auto-download can never succeed there. When traffic shaping is enabled and
// --daemon-bin wasn't supplied, this becomes a required value — prompted for
// interactively, or a fail-fast error non-interactively.
func resolveDaemonBinarySource(cmd *cobra.Command, args []string, profile string, cv *prompt.ChosenValues) (workflowsteps.DaemonBinarySource, error) {
	source := workflowsteps.DaemonBinarySource{BinPath: flagDaemonBin, Version: flagDaemonVersion}

	if profile != models.ProfileLocal || source.BinPath != "" {
		return source, nil
	}

	var rootFlags common.RootFlags
	_ = common.ExtractRootFlags(cmd, args, &rootFlags)
	if !prompt.ShouldPrompt(rootFlags.Force) {
		return source, errorx.IllegalArgument.New(
			"--daemon-bin is required for --profile=local: local/dev builds have no downloadable "+
				"solo-provisioner-daemon release to auto-download").
			WithProperty(models.ErrPropertyResolution, []string{
				"Build the daemon locally: task build:daemon GOOS=linux GOARCH=<arch>",
				"Then pass its path: --daemon-bin=<path-to-binary>",
			})
	}

	localCV := cv
	if localCV == nil {
		localCV = prompt.NewChosenValues()
	}
	if err := prompt.RunInputPrompts(cmd, []prompt.InputPrompt{
		daemonBinInputPrompt(source.BinPath, &source.BinPath),
	}, localCV); err != nil {
		return source, err
	}
	if source.BinPath == "" {
		return source, errorx.IllegalArgument.New("--daemon-bin is required for --profile=local").
			WithProperty(models.ErrPropertyResolution, []string{
				"Build the daemon locally: task build:daemon GOOS=linux GOARCH=<arch>",
				"Then pass its path: --daemon-bin=<path-to-binary>",
			})
	}
	return source, nil
}

// daemonBinInputPrompt returns the interactive prompt for --daemon-bin, shown
// only when --profile=local requires a local daemon binary path (see
// resolveDaemonBinarySource).
func daemonBinInputPrompt(eff string, target *string) prompt.InputPrompt {
	return prompt.InputPrompt{
		FlagName:       "daemon-bin",
		Title:          "Daemon Binary Path (required for --profile=local)",
		Description:    "Path to a locally-built solo-provisioner-daemon binary. Local/dev builds have no downloadable release to auto-download.",
		Placeholder:    "bin/solo-provisioner-daemon-linux-amd64",
		EffectiveValue: eff,
		Target:         target,
		Validate: func(s string) error {
			if s == "" {
				return errorx.IllegalArgument.New("daemon binary path cannot be empty for --profile=local")
			}
			_, err := sanity.SanitizePath(s)
			return err
		},
	}
}

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
