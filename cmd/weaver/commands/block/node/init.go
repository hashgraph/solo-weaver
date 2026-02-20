// SPDX-License-Identifier: Apache-2.0

package node

import (
	"context"

	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/runtime"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

func initializeDependencies(ctx context.Context) error {
	conf := config.Get()
	err := conf.Validate()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "invalid configuration")
	}

	sm, err := core.NewStateManager()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager")
	}

	currentState := sm.State()
	realityChecker, err := reality.NewChecker(currentState)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create reality checker")
	}

	// initialize cluster runtime
	err = runtime.InitClusterRuntime(conf, currentState.Cluster, realityChecker, runtime.DefaultRefreshInterval)
	if err != nil {
		return err
	}

	// initialize block node runtime
	err = runtime.InitBlockNodeRuntime(conf, currentState.BlockNode, realityChecker, runtime.DefaultRefreshInterval)
	if err != nil {
		return err
	}

	// initialize BLL
	_, err = bll.InitBlockNodeIntentHandler(conf.BlockNode, sm)
	if err != nil {
		return err
	}

	return nil
}

// prepareUserInputs prepares and validates user inputs from command flags.
func prepareUserInputs(cmd *cobra.Command, args []string) (*core.UserInputs[core.BlocknodeInputs], error) {
	var err error

	flagProfile, err = common.FlagProfile.Value(cmd, args)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
	}

	if flagProfile == "" {
		return nil, errorx.IllegalArgument.New("profile flag is required")
	}

	// Validate profile early for better error messages
	if !hardware.IsValidProfile(flagProfile) {
		return nil, errorx.IllegalArgument.New("unsupported profile: %q. Supported profiles: %v",
			flagProfile, hardware.SupportedProfiles())
	}

	if flagProfile == "" {
		return nil, errorx.IllegalArgument.New("profile flag is required")
	}

	flagForce, err = common.FlagForce.Value(cmd, args)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
	}

	skipHardwareChecks, err := cmd.Flags().GetBool(common.FlagSkipHardwareChecks.Name)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to get %s flag", common.FlagSkipHardwareChecks.Name)
	}

	// Validate the values file path if provided
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

	inputs := &core.UserInputs[core.BlocknodeInputs]{
		Common: core.CommonInputs{
			Force:            flagForce,
			NodeType:         core.NodeTypeBlock,
			ExecutionOptions: *execOpts,
		},
		Custom: core.BlocknodeInputs{
			Namespace:    flagNamespace,
			Release:      flagReleaseName,
			Chart:        flagChartRepo,
			ChartVersion: flagChartVersion,
			Storage: core.BlockNodeStorage{
				BasePath:    flagBasePath,
				ArchivePath: flagArchivePath,
				ArchiveSize: flagArchiveSize,
				LivePath:    flagLivePath,
				LiveSize:    flagLogSize,
				LogPath:     flagLogPath,
				LogSize:     flagLogSize,
			},
			Profile:            flagProfile,
			ValuesFile:         validatedValuesFile,
			ReuseValues:        !flagNoReuseValues,
			SkipHardwareChecks: skipHardwareChecks,
		},
	}

	if err := inputs.Validate(); err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "invalid user inputs")
	}

	return inputs, nil
}
