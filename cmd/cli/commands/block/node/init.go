// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll/blocknode"
	bnpkg "github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// blockNodeHandler is the per-command instance of blocknode.Handler, wired in
// initializeDependencies. It replaces the old package-level bll singleton.
var blockNodeHandler *blocknode.Handlers

func initializeDependencies() error {
	sr, err := common.Setup()
	if err != nil {
		return err
	}

	// Set values from other sources (other than config and state) as required
	// The order of initialization doesn't make any difference since each value can have its own value resolver to
	// set the correct precedence (e.g. env vars can override defaults, but not user inputs).
	defaults := config.DefaultsConfig()
	envVals := config.EnvConfig()
	logx.As().Debug().Any("defaults_config", config.DefaultsConfig()).Msg("Setting defaults")
	sr.Runtime.BlockNodeRuntime.WithDefaults(defaults)
	sr.Runtime.MachineRuntime.WithDefaults(defaults)
	logx.As().Debug().Any("env_config", config.EnvConfig()).Msg("Setting env config")
	sr.Runtime.BlockNodeRuntime.WithEnv(envVals)
	sr.Runtime.MachineRuntime.WithEnv(envVals)

	// Build the BLL handler, injecting the registry instead of relying on singletons.
	blockNodeHandler, err = blocknode.NewHandlerFactory(sr.Runtime)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to initialise block-node intent handler")
	}

	return nil
}

func extractBlockNodeParentFlags(cmd *cobra.Command, args []string, flags *BlockNodeFlags) error {
	return extractBlockNodeParentFlagsWithOpts(cmd, args, flags, true)
}

func extractBlockNodeParentFlagsWithOpts(cmd *cobra.Command, args []string, flags *BlockNodeFlags, requireProfile bool) error {
	// extract root-level flags and the profile flag
	if err := common.ExtractRootFlags(cmd, args, &flags.RootFlags); err != nil {
		return err
	}

	var err error
	flags.Profile, err = common.FlagProfile().Value(cmd, args)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
	}

	// validate profile — extraction is done, validation is the caller's responsibility
	if requireProfile && flags.Profile == "" {
		return errorx.IllegalArgument.New("profile flag is required")
	}

	if flags.Profile != "" && !hardware.IsValidProfile(flags.Profile) {
		return errorx.IllegalArgument.New("unsupported profile: %q. Supported profiles: %v",
			flags.Profile, models.SupportedProfiles())
	}

	return nil
}

// promptForMissingFlags presents interactive prompts for any block node flags
// not supplied on the command line. Returns the ChosenValues collector so
// callers can fold in additional prompt sections (e.g. host firewall) before
// printing the unified summary. Returns nil when the session is
// non-interactive or for commands (uninstall) that infer all values from
// existing state.
func promptForMissingFlags(cmd *cobra.Command, args []string) (*prompt.ChosenValues, error) {
	// Destructive commands that operate on an existing deployment should not
	// prompt — they infer everything from the current state/config.
	if cmd.Name() == "uninstall" {
		return nil, nil
	}

	var rootFlags common.RootFlags
	_ = common.ExtractRootFlags(cmd, args, &rootFlags) // best-effort to get Force

	if !prompt.ShouldPrompt(rootFlags.Force) {
		return nil, nil
	}

	cv := prompt.NewChosenValues()

	// Read all prompt-relevant fields from the on-disk state file in one pass.
	defaults, err := state.ReadPromptDefaultsFromDisk()
	if err != nil {
		logx.As().Debug().Err(err).Msg("Could not read prompt defaults from state file; using config/defaults only")
	}

	// reconfigure omits --chart-version (it is an immutable field); fall back to
	// the deployed version from state so plugin preset resolution uses the correct
	// version-gated list rather than defaulting to the latest release.
	if cmd.Name() == "reconfigure" && flagChartVersion == "" {
		flagChartVersion = defaults.BlockNode.ChartVersion
	}

	// Build a single wizard so every prompt page is navigable back and forward
	// (Shift+Tab / Tab) instead of committing each stage in its own form. Pages are
	// added in order; each Add* is a no-op when its flag(s) were already supplied on
	// the command line. The storage and plugin builders take &flagChartVersion (by
	// pointer), so their version-dependent pages react to edits made on the earlier
	// chart-version input page as the operator navigates forward.
	w := prompt.NewWizard()

	// profileTarget is a local copy the profile prompt writes to; after the wizard
	// completes we propagate it into Cobra's persistent flag set so that
	// common.FlagProfile().Value(cmd, args) returns the prompted value.
	var profileTarget string
	prompt.AddSelectPrompts(w, cmd, prompt.BlockNodeSelectPrompts(defaults, &profileTarget), cv)

	// Text-input prompts. Reconfigure uses a tailored set that omits immutable
	// fields (namespace, release-name, chart-version).
	var inputPrompts []prompt.InputPrompt
	if cmd.Name() == "reconfigure" {
		inputPrompts = prompt.BlockNodeReconfigureInputPrompts(
			defaults,
			&flagHistoricRetention, &flagRecentRetention,
		)
	} else {
		inputPrompts = prompt.BlockNodeInputPrompts(defaults, &flagNamespace, &flagReleaseName, &flagChartVersion, &flagHistoricRetention, &flagRecentRetention)
	}
	prompt.AddInputPrompts(w, cmd, inputPrompts, cv)

	// Storage path prompts: mode select → conditional path inputs (single base
	// path vs individual paths). Applied to all block node commands that configure
	// storage (install, upgrade, reconfigure).
	prompt.AddStoragePathPrompts(w, cmd, defaults, &flagChartVersion, prompt.StoragePathTargets{
		BasePath:             &flagBasePath,
		ArchivePath:          &flagArchivePath,
		LivePath:             &flagLivePath,
		LogPath:              &flagLogPath,
		VerificationPath:     &flagVerificationPath,
		PluginsPath:          &flagPluginsPath,
		ApplicationStatePath: &flagApplicationStatePath,
	}, cv)

	// Plugin preset prompt: preset select → conditional custom multi-select.
	prompt.AddPluginPresetPrompts(w, cmd, defaults, &flagPluginPreset, &flagPlugins, &flagChartVersion, flagValuesFile, cv)

	// Run all accumulated pages as a single navigable form.
	if err := w.Run(); err != nil {
		return nil, err
	}

	// Propagate the prompted profile value into Cobra's inherited persistent
	// flag set so extractBlockNodeParentFlags sees it.
	if profileTarget != "" {
		if f := cmd.InheritedFlags().Lookup("profile"); f != nil && !f.Changed {
			_ = f.Value.Set(profileTarget)
			f.Changed = true
		}
	}

	return cv, nil
}

// prepareBlocknodeInputs prepares and validates user inputs from command flags.
// When running interactively (TTY, no --force, no --non-interactive), it presents
// huh prompts for any required flags that were not supplied on the command line.
// The returned ChosenValues collector is nil when the session is non-interactive;
// callers are responsible for printing the summary (via cv.Print) at the right
// point — after any additional prompt sections (e.g. host firewall) have added
// their entries.
func prepareBlocknodeInputs(cmd *cobra.Command, args []string) (*models.UserInputs[models.BlockNodeInputs], *prompt.ChosenValues, error) {
	var err error

	// ── Interactive prompts ──────────────────────────────────────────────
	// Run prompts for missing flags before extracting/validating them.
	cv, err := promptForMissingFlags(cmd, args)
	if err != nil {
		return nil, nil, err
	}

	// ── Extract & validate flags ─────────────────────────────────────────
	// extract shared flags set in the parent commands
	var parentFlags BlockNodeFlags
	// Uninstall infers profile from existing state/config, so it is not required on the CLI.
	requireProfile := cmd.Name() != "uninstall"
	err = extractBlockNodeParentFlagsWithOpts(cmd, args, &parentFlags, requireProfile)
	if err != nil {
		return nil, nil, err
	}

	// Validate the value file path if provided
	// This is the primary security validation point for user-supplied file paths.
	var validatedValuesFile string
	if flagValuesFile != "" {
		validatedValuesFile, err = sanity.ValidateInputFile(flagValuesFile)
		if err != nil {
			return nil, nil, err
		}
	}

	// Determine execution mode based on flags
	execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
	if err != nil {
		return nil, nil, errorx.Decorate(err, "failed to determine execution mode")
	}
	execOpts := workflows.DefaultWorkflowExecutionOptions()
	execOpts.ExecutionMode = execMode

	// Resolve the effective plugin list.
	// --plugins overrides --plugin-preset when both are set.
	// When only --plugin-preset is set, look up its canonical list.
	pluginPreset := flagPluginPreset
	pluginList := flagPlugins
	if f := cmd.Flag("plugins"); f != nil && f.Changed {
		if err := models.ValidatePluginList(flagPlugins); err != nil {
			return nil, nil, errorx.IllegalArgument.Wrap(err, "invalid --plugins value")
		}
	}
	if pluginList == "" && bnpkg.IsKnownPreset(pluginPreset) {
		pluginList = bnpkg.PluginListForPreset(pluginPreset, flagChartVersion)
	}
	// For non-interactive upgrades (scripted runs where the TUI is skipped),
	// --plugin-preset is not prompted, so flagPluginPreset stays empty. Re-resolve
	// from the stored preset to ensure plugin names are updated when crossing chart
	// version boundaries (e.g. the 0.35 s3-archive → cloud-storage-* rename).
	// Skip this when the operator manages plugins via their --values file (it defines
	// plugins.names): re-resolving from the saved preset would clobber the values file.
	if cmd.Name() == "upgrade" && pluginPreset == "" && pluginList == "" &&
		!bnpkg.ValuesFileDefinesPlugins(validatedValuesFile) {
		if stateDefaults, defErr := state.ReadPromptDefaultsFromDisk(); defErr == nil {
			if bnpkg.IsKnownPreset(stateDefaults.BlockNode.PluginPreset) {
				pluginPreset = stateDefaults.BlockNode.PluginPreset
				pluginList = bnpkg.PluginListForPreset(pluginPreset, flagChartVersion)
			}
		}
	}
	if pluginList != "" && pluginPreset == "" {
		pluginPreset = bnpkg.PresetCustom
	}
	if pluginPreset == bnpkg.PresetCustom && pluginList == "" {
		return nil, nil, errorx.IllegalArgument.New("--plugins is required when --plugin-preset=%s", bnpkg.PresetCustom)
	}

	inputs := &models.UserInputs[models.BlockNodeInputs]{
		Common: models.CommonInputs{
			Force:            parentFlags.Force,
			NodeType:         models.NodeTypeBlock,
			ExecutionOptions: *execOpts,
		},
		Custom: models.BlockNodeInputs{
			Namespace:    flagNamespace,
			Release:      flagReleaseName,
			Chart:        flagChartRepo,
			ChartVersion: flagChartVersion,
			Storage: models.BlockNodeStorage{
				BasePath:             flagBasePath,
				ArchivePath:          flagArchivePath,
				ArchiveSize:          flagArchiveSize,
				LivePath:             flagLivePath,
				LiveSize:             flagLiveSize,
				LogPath:              flagLogPath,
				LogSize:              flagLogSize,
				VerificationPath:     flagVerificationPath,
				VerificationSize:     flagVerificationSize,
				PluginsPath:          flagPluginsPath,
				PluginsSize:          flagPluginsSize,
				ApplicationStatePath: flagApplicationStatePath,
				ApplicationStateSize: flagApplicationStateSize,
			},
			Profile:             parentFlags.Profile,
			ValuesFile:          validatedValuesFile,
			ReuseValues:         !flagNoReuseValues,
			ResetStorage:        flagWithReset || flagPurgeStorage,
			PurgeStorage:        flagPurgeStorage,
			NoRestart:           flagNoRestart,
			SkipHardwareChecks:  parentFlags.SkipHardwareChecks,
			LoadBalancerEnabled: flagLoadBalancerEnabled,
			HistoricRetention:   flagHistoricRetention,
			RecentRetention:     flagRecentRetention,
			PluginPreset:        pluginPreset,
			PluginList:          pluginList,
			EgressInterface:     flagEgressInterface,
		},
	}

	logx.As().Info().Any("inputs", inputs).Msg("User inputs for block node operation")
	if err := inputs.Validate(); err != nil {
		return nil, nil, errorx.IllegalArgument.Wrap(err, "invalid user inputs")
	}

	return inputs, cv, nil
}
