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
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var (
	nodeType = core.NodeTypeBlock

	flagStopOnError     bool
	flagRollbackOnError bool
	flagContinueOnError bool

	flagProfile string // inherited from parent
	flagForce   bool   // inherited from parent

	flagValuesFile   string
	flagChartVersion string
	flagChartRepo    string
	flagNamespace    string
	flagReleaseName  string
	flagBasePath     string
	flagArchivePath  string
	flagLivePath     string
	flagLogPath      string
	flagLiveSize     string
	flagArchiveSize  string
	flagLogSize      string

	nodeCmd = &cobra.Command{
		Use:   "node",
		Short: "Manage lifecycle of a Hedera Block Node",
		Long:  "Manage lifecycle of a Hedera Block Node",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
	}
)

func init() {
	// Helm chart configuration flags
	nodeCmd.PersistentFlags().StringVar(&flagChartVersion, "chart-version", "", "Helm chart version to use")
	nodeCmd.PersistentFlags().StringVar(&flagChartRepo, "chart-repo", "", "Helm chart repository URL")
	nodeCmd.PersistentFlags().StringVar(&flagNamespace, "namespace", "", "Kubernetes namespace for block node")
	nodeCmd.PersistentFlags().StringVar(&flagReleaseName, "release-name", "", "Helm release name")

	// Storage path configuration flags
	nodeCmd.PersistentFlags().StringVar(&flagBasePath, "base-path", "", "Base path for all storage (used when individual paths are not specified)")
	nodeCmd.PersistentFlags().StringVar(&flagArchivePath, "archive-path", "", "Path for archive storage")
	nodeCmd.PersistentFlags().StringVar(&flagLivePath, "live-path", "", "Path for live storage")
	nodeCmd.PersistentFlags().StringVar(&flagLogPath, "log-path", "", "Path for log storage")

	// Storage size configuration flags
	nodeCmd.PersistentFlags().StringVar(&flagLiveSize, "live-size", "", "Size for live storage PV/PVC (e.g., 5Gi, 10Gi)")
	nodeCmd.PersistentFlags().StringVar(&flagArchiveSize, "archive-size", "", "Size for archive storage PV/PVC (e.g., 5Gi, 10Gi)")
	nodeCmd.PersistentFlags().StringVar(&flagLogSize, "log-size", "", "Size for log storage PV/PVC (e.g., 5Gi, 10Gi)")

	nodeCmd.AddCommand(checkCmd, installCmd, upgradeCmd)
}

func GetCmd() *cobra.Command {
	return nodeCmd
}

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

	// initialize runtime
	err = runtime.InitClusterRuntime(conf, currentState.Cluster, realityChecker, runtime.DefaultRefreshInterval)
	if err != nil {
		return err
	}
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

	flagForce, err = common.FlagForce.Value(cmd, args)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
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
			ReleaseName:  flagReleaseName,
			ChartRepo:    flagChartRepo,
			ChartVersion: flagChartVersion,
			Storage: config.BlockNodeStorage{
				BasePath:    flagBasePath,
				ArchivePath: flagArchivePath,
				ArchiveSize: flagArchiveSize,
				LivePath:    flagLivePath,
				LiveSize:    flagLogSize,
				LogPath:     flagLogPath,
				LogSize:     flagLogSize,
			},
			Profile:     flagProfile,
			ValuesFile:  validatedValuesFile,
			ReuseValues: !flagNoReuseValues,
		},
	}

	if err := inputs.Validate(); err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "invalid user inputs")
	}

	return inputs, nil
}
