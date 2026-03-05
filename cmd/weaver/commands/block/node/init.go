// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll/blocknode"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
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
var blockNodeHandler *blocknode.HandlerFactory

func initializeDependencies() error {
	conf := config.Get()
	if err := conf.Validate(); err != nil {
		return errorx.IllegalState.Wrap(err, "invalid configuration")
	}

	logx.As().Debug().Any("config", conf).Msg("Loaded configuration")

	sm, err := state.NewStateManager()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager")
	}

	if err = sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return errorx.IllegalState.Wrap(err, "failed to refresh state")
	}

	currentState := sm.State()
	logx.As().Info().Str("state_file", currentState.StateFile).Any("currentState", currentState).Msg("Current state")

	realityChecker, err := reality.NewCheckers(sm)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create reality checker")
	}

	// Build the rsl.RuntimeResolver — single call that constructs cluster + block-node runtimes.
	runtime, err := rsl.NewRuntimeResolver(conf, sm, realityChecker, rsl.DefaultRefreshInterval)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to initialise rsl registry")
	}

	// Build the BLL handler, injecting the registry instead of relying on singletons.
	// sm satisfies both state.Reader and state.Writer — pass it for both roles.
	blockNodeHandler, err = blocknode.NewHandlerFactory(sm, runtime)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to initialise block-node intent handler")
	}

	return nil
}

func initializeExecutionFlags(resetCmd *cobra.Command) {
	common.FlagStopOnError.SetVarP(resetCmd, &flagStopOnError, false)
	common.FlagRollbackOnError.SetVarP(resetCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError.SetVarP(resetCmd, &flagContinueOnError, false)
}

func extractBlockNodeParentFlags(cmd *cobra.Command, args []string, parentFlags *common.ParentCmdFlags) error {
	var err error

	// extract common flags set in the root command
	err = common.ExtractRootFlags(cmd, args, parentFlags)
	if err != nil {
		return err
	}

	// extract the profile flag set in the block node command
	parentFlags.Profile, err = common.FlagProfile.Value(cmd, args)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
	}

	if parentFlags.Profile == "" {
		return errorx.IllegalArgument.New("profile flag is required")
	}

	// Validate profile early for better error messages
	if parentFlags.Profile != "" && !hardware.IsValidProfile(parentFlags.Profile) {
		return errorx.IllegalArgument.New("unsupported profile: %q. Supported profiles: %v",
			parentFlags.Profile, hardware.SupportedProfiles())
	}

	return nil
}

// prepareBlocknodeInputs prepares and validates user inputs from command flags.
func prepareBlocknodeInputs(cmd *cobra.Command, args []string) (*models.UserInputs[models.BlocknodeInputs], error) {
	var err error

	// extract shared flags set in the parent commands
	var parentFlags common.ParentCmdFlags
	err = extractBlockNodeParentFlags(cmd, args, &parentFlags)
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

	inputs := &models.UserInputs[models.BlocknodeInputs]{
		Common: models.CommonInputs{
			Force:            parentFlags.Force,
			NodeType:         models.NodeTypeBlock,
			ExecutionOptions: *execOpts,
		},
		Custom: models.BlocknodeInputs{
			Namespace:    flagNamespace,
			Release:      flagReleaseName,
			Chart:        flagChartRepo,
			ChartVersion: flagChartVersion,
			Storage: models.BlockNodeStorage{
				BasePath:         flagBasePath,
				ArchivePath:      flagArchivePath,
				ArchiveSize:      flagArchiveSize,
				LivePath:         flagLivePath,
				LiveSize:         flagLogSize,
				LogPath:          flagLogPath,
				LogSize:          flagLogSize,
				VerificationPath: flagVerificationPath,
				VerificationSize: flagVerificationSize,
			},
			Profile:            parentFlags.Profile,
			ValuesFile:         validatedValuesFile,
			ReuseValues:        !flagNoReuseValues,
			ResetStorage:       flagWithReset,
			SkipHardwareChecks: parentFlags.SkipHardwareChecks,
		},
	}

	// If base path is not provided, use the default from config.
	// This allows users to specify individual storage paths/sizes without needing to provide a base path.
	if flagBasePath == "" {
		inputs.Custom.Storage.BasePath = config.Get().BlockNode.Storage.BasePath
	}

	if err := inputs.Validate(); err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "invalid user inputs")
	}

	return inputs, nil
}
