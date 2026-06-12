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
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	workflowsteps "github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	pkgos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var (
	flagComponents     string // comma-separated: "consensus-node", "block-node"
	flagCNNodeID       string
	flagCNOrbit        string
	flagCNUpgradeDir   string
	flagBNOrbit        string
	flagFromConfig     string
	flagDaemonBin      string
	flagDaemonChecksum string
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the solo-provisioner-daemon systemd service",
	Long: "Bootstrap daemon.yaml (prompting for required fields when not supplied), " +
		"provision RBAC resources, generate the daemon kubeconfig, and install + start the " +
		"solo-provisioner-daemon systemd service. Requires root privileges and a reachable K8s cluster.\n\n" +
		"Use --components to select which components to enable (e.g. \"consensus-node,block-node\"). " +
		"At least one component must be selected — RBAC and kubeconfigs are only provisioned for " +
		"the chosen components.\n\n" +
		"If daemon.yaml already exists its values are used as-is; individual fields can be overridden " +
		"with --cn-node-id, --cn-orbit, --cn-upgrade-dir, and --bn-orbit. Use --from-config to copy a " +
		"pre-built daemon.yaml into place instead of prompting.\n\n" +
		"To add or remove components after initial install, run 'daemon service uninstall' first, " +
		"then re-run 'daemon service install' with the updated --components list.",
	RunE: func(cmd *cobra.Command, args []string) error {
		var rootFlags common.RootFlags
		_ = common.ExtractRootFlags(cmd, args, &rootFlags)

		// ── 0. Fail fast if the service is already running ────────────────────
		// copyBinaryFile uses O_TRUNC which raises "text file busy" on an active
		// executable, but we also refuse to touch daemon.yaml while the service is
		// running to avoid leaving a partial-update state (config changed on disk,
		// service still running with old in-memory config).
		if running, _ := pkgos.IsServiceRunning(cmd.Context(), "solo-provisioner-daemon"); running {
			return errorx.IllegalState.New(
				"daemon service 'solo-provisioner-daemon' is already running — stop it before installing").
				WithProperty(models.ErrPropertyResolution, []string{
					"Stop the service, re-run install, then verify:",
					"  sudo solo-provisioner daemon service stop",
					"  sudo solo-provisioner daemon service install [flags]",
					"  sudo solo-provisioner daemon service check",
				})
		}

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
	common.FlagDaemonComponents().SetVarP(installCmd, &flagComponents, false)
	common.FlagDaemonCNNodeID().SetVarP(installCmd, &flagCNNodeID, false)
	common.FlagDaemonCNOrbit().SetVarP(installCmd, &flagCNOrbit, false)
	common.FlagDaemonCNUpgradeDir().SetVarP(installCmd, &flagCNUpgradeDir, false)
	common.FlagDaemonBNOrbit().SetVarP(installCmd, &flagBNOrbit, false)
	common.FlagDaemonFromConfig().SetVarP(installCmd, &flagFromConfig, false)
	common.FlagDaemonBin().SetVarP(installCmd, &flagDaemonBin, false)
	common.FlagDaemonChecksum().SetVarP(installCmd, &flagDaemonChecksum, false)
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
				"cannot copy config from %s to %s", flagFromConfig, paths.DaemonConfigPath).
				WithProperty(models.ErrPropertyResolution, []string{
					"Verify the source file exists and is readable: ls -la " + flagFromConfig,
					"Ensure the destination directory is writable: ls -la " + filepath.Dir(paths.DaemonConfigPath),
				})
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
		// Apply flag overrides and merge any newly requested components.
		changed := applyFlagOverrides(&cfg)
		changed = mergeRequestedComponents(&cfg, flagComponents) || changed
		if changed {
			if err := daemon.WriteDaemonConfig(paths.DaemonConfigPath, cfg); err != nil {
				return daemon.DaemonConfig{}, err
			}
			logx.As().Info().Str("path", paths.DaemonConfigPath).Msg("daemon config updated with flag overrides")
		}
		return cfg, nil
	}

	// Re-raise anything other than "file not found", attaching resolution hints
	// so the doctor layer can print actionable next steps.
	if !errors.Is(err, os.ErrNotExist) && !daemon.IsConfigNotFound(err) {
		if daemon.IsConfigMalformed(err) {
			if ex := errorx.Cast(err); ex != nil {
				return daemon.DaemonConfig{}, ex.WithProperty(doctor.ErrPropertyResolution,
					configMalformedResolution(paths.DaemonConfigPath))
			}
		}
		return daemon.DaemonConfig{}, err
	}

	// ── Case 3: no daemon.yaml — build it from flags + prompts ───────────────

	// Parse --components into a ComponentSet. When the flag was not set the set
	// is empty and RunDaemonInstallPrompts will ask interactively.
	cs := prompt.ParseComponentsFlag(flagComponents)

	// Pre-populate per-component configs from any explicit flags so that
	// RunDaemonInstallPrompts receives already-filled targets and can skip
	// prompting for fields the operator supplied on the command line.
	var cnCfg *daemon.ConsensusNodeComponentConfig
	if cs.Has(prompt.ComponentConsensusNode) {
		c := daemon.ConsensusNodeComponentConfig{
			Enabled:    true,
			NodeID:     flagCNNodeID,
			Orbit:      flagCNOrbit,
			UpgradeDir: flagCNUpgradeDir,
			Kubeconfig: paths.DaemonCNKubeconfigPath,
			Monitors:   daemon.ConsensusNodeMonitors{Upgrade: true, Migration: true},
		}
		cnCfg = &c
	}

	var bnCfg *daemon.BlockNodeComponentConfig
	if cs.Has(prompt.ComponentBlockNode) {
		// Orbit and Kubeconfig omitted while traffic-shaper is stubbed.
		c := daemon.BlockNodeComponentConfig{
			Enabled:  true,
			Monitors: daemon.BlockNodeMonitors{TrafficShaper: true},
		}
		bnCfg = &c
	}

	cfg = daemon.DaemonConfig{
		Components: daemon.DaemonComponents{ConsensusNode: cnCfg, BlockNode: bnCfg},
	}

	// Prompt for any fields still empty (unless non-interactive / force).
	if prompt.ShouldPrompt(rootFlags.Force) {
		cv := prompt.NewChosenValues()
		targets := prompt.DaemonInstallInputTargets{
			ComponentsRaw: &flagComponents,
		}
		if cnCfg != nil {
			targets.CNNodeID = &cnCfg.NodeID
			targets.CNOrbit = &cnCfg.Orbit
			targets.CNUpgradeDir = &cnCfg.UpgradeDir
		}
		// BNOrbit not prompted while traffic-shaper is stubbed.
		if err := prompt.RunDaemonInstallPrompts(cmd, &cfg, targets, paths, cv); err != nil {
			return daemon.DaemonConfig{}, err
		}
		cv.Print("Daemon configuration")
	}

	// Validate before writing — surface missing required fields with clear errors.
	if err := cfg.Validate(); err != nil {
		return daemon.DaemonConfig{}, errorx.IllegalArgument.Wrap(err,
			"daemon config incomplete: supply --components and the required per-component flags, or run interactively").
			WithProperty(doctor.ErrPropertyResolution, missingComponentsResolution())
	}

	if err := daemon.WriteDaemonConfig(paths.DaemonConfigPath, cfg); err != nil {
		return daemon.DaemonConfig{}, err
	}
	logx.As().Info().Str("path", paths.DaemonConfigPath).Msg("daemon config written")

	return cfg, nil
}

// mergeRequestedComponents adds any component blocks requested via --components
// that are absent in cfg. Returns true if cfg was modified.
func mergeRequestedComponents(cfg *daemon.DaemonConfig, componentsFlag string) bool {
	cs := prompt.ParseComponentsFlag(componentsFlag)
	changed := false
	if cs.Has(prompt.ComponentBlockNode) && cfg.Components.BlockNode == nil {
		cfg.Components.BlockNode = &daemon.BlockNodeComponentConfig{
			Enabled:  true,
			Monitors: daemon.BlockNodeMonitors{TrafficShaper: true},
		}
		changed = true
	}
	return changed
}

// applyFlagOverrides writes any explicitly-set flag values into cfg.
// Returns true if at least one field was changed.
func applyFlagOverrides(cfg *daemon.DaemonConfig) bool {
	changed := false
	if cn := cfg.Components.ConsensusNode; cn != nil {
		if flagCNNodeID != "" {
			cn.NodeID = flagCNNodeID
			changed = true
		}
		if flagCNOrbit != "" {
			cn.Orbit = flagCNOrbit
			changed = true
		}
		if flagCNUpgradeDir != "" {
			cn.UpgradeDir = flagCNUpgradeDir
			changed = true
		}
	}
	if bn := cfg.Components.BlockNode; bn != nil {
		if flagBNOrbit != "" {
			bn.Orbit = flagBNOrbit
			changed = true
		}
	}
	return changed
}

// configMalformedResolution returns resolution steps shown when an existing
// daemon.yaml fails validation (missing fields, unknown schema, etc.).
func configMalformedResolution(configPath string) string {
	return "The existing daemon config is incomplete or invalid. To fix:\n" +
		"\n" +
		"  Option A — re-run interactively (recommended):\n" +
		"    1. Remove the broken config:  sudo rm " + configPath + "\n" +
		"    2. Re-run the install and follow the prompts:\n" +
		"         sudo solo-provisioner daemon service install\n" +
		"\n" +
		"  Option B — supply all values via flags (non-interactive):\n" +
		"    sudo solo-provisioner daemon service install \\\n" +
		"      --components consensus-node \\\n" +
		"      --cn-node-id 0.0.3 \\\n" +
		"      --cn-orbit <namespace>\n" +
		"\n" +
		"  Option C — copy a pre-built daemon.yaml into place:\n" +
		"    sudo solo-provisioner daemon service install --from-config /path/to/daemon.yaml"
}

// missingComponentsResolution returns resolution steps shown when no components
// are configured (--components flag was omitted in non-interactive mode).
func missingComponentsResolution() string {
	return "No daemon components were selected. To fix:\n" +
		"\n" +
		"  Option A — re-run interactively (recommended):\n" +
		"    sudo solo-provisioner daemon service install\n" +
		"\n" +
		"  Option B — pass --components explicitly:\n" +
		"    sudo solo-provisioner daemon service install \\\n" +
		"      --components consensus-node \\\n" +
		"      --cn-node-id 0.0.3 \\\n" +
		"      --cn-orbit <namespace>\n" +
		"\n" +
		"  Valid component names: \"consensus-node\", \"block-node\", or both comma-separated."
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
