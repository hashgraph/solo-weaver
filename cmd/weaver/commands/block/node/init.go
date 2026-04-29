// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll/blocknode"
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

// promptForMissingFlags runs interactive prompts for any required block node
// flags that were not supplied on the command line. It writes prompted values
// back into the Cobra flag set and the package-level flag variables so that
// downstream extraction and validation sees them.
//
// Prompts are skipped when:
//   - --force/-y is set
//   - --non-interactive is set
//   - stdout is not a TTY
//   - the command is "uninstall" (infers values from existing state/config)
func promptForMissingFlags(cmd *cobra.Command, args []string) error {
	// Destructive commands that operate on an existing deployment should not
	// prompt — they infer everything from the current state/config.
	if cmd.Name() == "uninstall" {
		return nil
	}

	var rootFlags common.RootFlags
	_ = common.ExtractRootFlags(cmd, args, &rootFlags) // best-effort to get Force

	if !prompt.ShouldPrompt(rootFlags.Force) {
		return nil
	}

	cv := prompt.NewChosenValues()

	// Read all prompt-relevant fields from the on-disk state file in one pass.
	defaults, err := state.ReadPromptDefaultsFromDisk()
	if err != nil {
		logx.As().Debug().Err(err).Msg("Could not read prompt defaults from state file; using config/defaults only")
	}

	// profileTarget is a local copy that the prompt writes to; after the prompt
	// completes we propagate it into Cobra's persistent flag set so that
	// common.FlagProfile().Value(cmd, args) returns the prompted value.
	var profileTarget string
	selectPrompts := prompt.BlockNodeSelectPrompts(defaults, &profileTarget)
	if err := prompt.RunSelectPrompts(cmd, selectPrompts, cv); err != nil {
		return err
	}

	// Propagate the prompted profile value into Cobra's inherited persistent
	// flag set so extractBlockNodeParentFlags sees it.
	if profileTarget != "" {
		if f := cmd.InheritedFlags().Lookup("profile"); f != nil && !f.Changed {
			_ = f.Value.Set(profileTarget)
			f.Changed = true
		}
	}

	// Run text-input prompts for optional flags (namespace, release, chart version, retention thresholds).
	inputPrompts := prompt.BlockNodeInputPrompts(defaults, &flagNamespace, &flagReleaseName, &flagChartVersion, &flagHistoricRetention, &flagRecentRetention)
	if err := prompt.RunInputPrompts(cmd, inputPrompts, cv); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(cmd.ErrOrStderr())
	cv.Print("Selected Inputs")

	return nil
}

// prepareBlocknodeInputs prepares and validates user inputs from command flags.
// When running interactively (TTY, no --force, no --non-interactive), it presents
// huh prompts for any required flags that were not supplied on the command line.
func prepareBlocknodeInputs(cmd *cobra.Command, args []string) (*models.UserInputs[models.BlockNodeInputs], error) {
	var err error

	// ── Interactive prompts ──────────────────────────────────────────────
	// Run prompts for missing flags before extracting/validating them.
	if err = promptForMissingFlags(cmd, args); err != nil {
		return nil, err
	}

	// ── Extract & validate flags ─────────────────────────────────────────
	// extract shared flags set in the parent commands
	var parentFlags BlockNodeFlags
	// Uninstall infers profile from existing state/config, so it is not required on the CLI.
	requireProfile := cmd.Name() != "uninstall"
	err = extractBlockNodeParentFlagsWithOpts(cmd, args, &parentFlags, requireProfile)
	if err != nil {
		return nil, err
	}

	// Validate the value file path if provided
	// This is the primary security validation point for user-supplied file paths.
	var validatedValuesFile string
	if flagValuesFile != "" {
		validatedValuesFile, err = sanity.ValidateInputFile(flagValuesFile)
		if err != nil {
			return nil, err
		}
	}

	// Determine execution mode based on flags
	execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
	if err != nil {
		return nil, errorx.Decorate(err, "failed to determine execution mode")
	}
	execOpts := workflows.DefaultWorkflowExecutionOptions()
	execOpts.ExecutionMode = execMode

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
				BasePath:         flagBasePath,
				ArchivePath:      flagArchivePath,
				ArchiveSize:      flagArchiveSize,
				LivePath:         flagLivePath,
				LiveSize:         flagLiveSize,
				LogPath:          flagLogPath,
				LogSize:          flagLogSize,
				VerificationPath: flagVerificationPath,
				VerificationSize: flagVerificationSize,
				PluginsPath:      flagPluginsPath,
				PluginsSize:      flagPluginsSize,
			},
			Profile:             parentFlags.Profile,
			ValuesFile:          validatedValuesFile,
			ReuseValues:         !flagNoReuseValues,
			ResetStorage:        flagWithReset,
			SkipHardwareChecks:  parentFlags.SkipHardwareChecks,
			HistoricRetention:   flagHistoricRetention,
			RecentRetention:     flagRecentRetention,
			LoadBalancerEnabled: flagLoadBalancerEnabled,
		},
	}

	logx.As().Info().Any("inputs", inputs).Msg("User inputs for block node operation")
	if err := inputs.Validate(); err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "invalid user inputs")
	}

	return inputs, nil
}
