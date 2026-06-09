// SPDX-License-Identifier: Apache-2.0

package service

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	daemon "github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	workflowsteps "github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var (
	flagNodeID         string
	flagOrbit          string
	flagUpgradeDir     string
	flagFromConfig     string
	flagDaemonBin      string
	flagDaemonChecksum string
	flagDaemonCommit   string
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the solo-provisioner-daemon systemd service",
	Long: "Bootstrap daemon.yaml (prompting for required fields when not supplied), " +
		"provision RBAC resources, generate the daemon kubeconfig, and install + start the " +
		"solo-provisioner-daemon systemd service. Requires root privileges and a reachable K8s cluster.\n\n" +
		"If daemon.yaml already exists its values are used as-is; individual fields can be overridden " +
		"with --node-id, --orbit, and --upgrade-dir. Use --from-config to copy a pre-built daemon.yaml " +
		"into place instead of prompting.",
	RunE: func(cmd *cobra.Command, args []string) error {
		var rootFlags common.RootFlags
		_ = common.ExtractRootFlags(cmd, args, &rootFlags)

		paths := models.Paths()

		// ── 1. Determine the effective daemon config ──────────────────────────
		cfg, err := resolveDaemonConfig(cmd, args, paths, rootFlags)
		if err != nil {
			return err
		}

		// ── 2. Run the install workflow ───────────────────────────────────────
		wf, err := workflows.NewDaemonServiceInstallWorkflow(cfg, workflowsteps.DaemonBinarySource{
			BinPath:  flagDaemonBin,
			Checksum: flagDaemonChecksum,
			Commit:   flagDaemonCommit,
		})
		if err != nil {
			return err
		}
		if err := common.RunWorkflowBuilder(cmd.Context(), wf); err != nil {
			return err
		}

		logx.As().Info().Msg("solo-provisioner-daemon service installed, enabled, and started")
		return nil
	},
}

func init() {
	common.FlagDaemonNodeID().SetVarP(installCmd, &flagNodeID, false)
	common.FlagDaemonOrbit().SetVarP(installCmd, &flagOrbit, false)
	common.FlagDaemonUpgradeDir().SetVarP(installCmd, &flagUpgradeDir, false)
	common.FlagDaemonFromConfig().SetVarP(installCmd, &flagFromConfig, false)
	common.FlagDaemonBin().SetVarP(installCmd, &flagDaemonBin, false)
	common.FlagDaemonChecksum().SetVarP(installCmd, &flagDaemonChecksum, false)
	common.FlagDaemonCommit().SetVarP(installCmd, &flagDaemonCommit, false)
}

// resolveDaemonConfig returns the DaemonConfig to use for the install, writing
// daemon.yaml to disk if it does not already exist (or if --from-config was
// supplied). The resolution order is:
//
//  1. --from-config: copy the given file to paths.DaemonConfigPath as-is, then load it.
//  2. Existing daemon.yaml: load it and apply any flag overrides.
//  3. No daemon.yaml: collect values via flags + interactive prompts, then write it.
func resolveDaemonConfig(
	cmd *cobra.Command,
	args []string,
	paths models.WeaverPaths,
	rootFlags common.RootFlags,
) (daemon.DaemonConfig, error) {
	// ── Case 1: --from-config ─────────────────────────────────────────────────
	if flagFromConfig != "" {
		if err := copyFile(flagFromConfig, paths.DaemonConfigPath); err != nil {
			return daemon.DaemonConfig{}, errorx.ExternalError.Wrap(err,
				"cannot copy config from %s to %s", flagFromConfig, paths.DaemonConfigPath)
		}
		logx.As().Info().Str("src", flagFromConfig).Str("dst", paths.DaemonConfigPath).
			Msg("daemon config copied from --from-config")
		cfg, err := daemon.LoadDaemonConfig(paths.DaemonConfigPath)
		if err != nil {
			return daemon.DaemonConfig{}, err
		}
		return cfg, nil
	}

	// ── Case 2: daemon.yaml already exists ───────────────────────────────────
	cfg, err := daemon.LoadDaemonConfig(paths.DaemonConfigPath)
	if err == nil {
		// File exists and is valid; apply any explicit flag overrides.
		changed := applyFlagOverrides(&cfg)
		if changed {
			if err := daemon.WriteDaemonConfig(paths.DaemonConfigPath, cfg); err != nil {
				return daemon.DaemonConfig{}, err
			}
			logx.As().Info().Str("path", paths.DaemonConfigPath).Msg("daemon config updated with flag overrides")
		}
		return cfg, nil
	}

	// Re-raise anything other than "file not found".
	if !errors.Is(err, os.ErrNotExist) && !daemon.IsConfigNotFound(err) {
		return daemon.DaemonConfig{}, err
	}

	// ── Case 3: no daemon.yaml — build it from flags + prompts ───────────────
	// Pre-fill from explicit flags. Kubeconfig will be written by
	// WriteConsensusNodeKubeconfigStep; record the well-known path so the daemon can
	// find it on startup.
	cn := daemon.ConsensusNodeComponentConfig{
		Enabled:    true,
		NodeID:     flagNodeID,
		Orbit:      flagOrbit,
		UpgradeDir: flagUpgradeDir,
		Kubeconfig: paths.DaemonCNKubeconfigPath,
		Monitors:   daemon.ConsensusNodeMonitors{Upgrade: true, Migration: true},
	}
	cfg = daemon.DaemonConfig{
		Components: daemon.DaemonComponents{ConsensusNode: &cn},
	}

	// Prompt for any fields still empty (unless non-interactive / force).
	if prompt.ShouldPrompt(rootFlags.Force) {
		cv := prompt.NewChosenValues()
		targets := prompt.DaemonInstallInputTargets{
			NodeID:     &cfg.Components.ConsensusNode.NodeID,
			Orbit:      &cfg.Components.ConsensusNode.Orbit,
			UpgradeDir: &cfg.Components.ConsensusNode.UpgradeDir,
		}
		if err := prompt.RunDaemonInstallPrompts(cmd, targets, cv); err != nil {
			return daemon.DaemonConfig{}, err
		}
		cv.Print("Daemon configuration")
	}

	// Validate before writing — surface missing required fields with clear errors.
	if err := cfg.Validate(); err != nil {
		return daemon.DaemonConfig{}, errorx.IllegalArgument.Wrap(err,
			"daemon config incomplete: supply --node-id and --orbit flags, or run interactively")
	}

	if err := daemon.WriteDaemonConfig(paths.DaemonConfigPath, cfg); err != nil {
		return daemon.DaemonConfig{}, err
	}
	logx.As().Info().Str("path", paths.DaemonConfigPath).Msg("daemon config written")

	return cfg, nil
}

// applyFlagOverrides writes any explicitly-set flag values into cfg.
// Returns true if at least one field was changed.
func applyFlagOverrides(cfg *daemon.DaemonConfig) bool {
	if cfg.Components.ConsensusNode == nil {
		return false
	}
	cn := cfg.Components.ConsensusNode
	changed := false
	if flagNodeID != "" {
		cn.NodeID = flagNodeID
		changed = true
	}
	if flagOrbit != "" {
		cn.Orbit = flagOrbit
		changed = true
	}
	if flagUpgradeDir != "" {
		cn.UpgradeDir = flagUpgradeDir
		changed = true
	}
	return changed
}

// copyFile copies src to dst, creating any missing parent directories.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src) //nolint:gosec // path comes from a trusted CLI flag
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
