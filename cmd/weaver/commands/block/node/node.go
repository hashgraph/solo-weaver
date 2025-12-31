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

	flagProfile      string // inherited from parent
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
	currentState := core.NewState()
	realityChecker := reality.NewChecker()
	conf := config.Get()

	// initialize runtime
	err := runtime.InitClusterRuntime(currentState.Cluster, realityChecker, runtime.DefaultRefreshInterval)
	if err != nil {
		return err
	}
	err = runtime.InitBlockNodeRuntime(currentState.BlockNode, realityChecker, runtime.DefaultRefreshInterval)
	if err != nil {
		return err
	}

	// initialize BLL
	_, err = bll.InitBlockNodeIntentHandler(conf.BlockNode)
	if err != nil {
		return err
	}

	return nil
}

func prepareUserInputs(cmd *cobra.Command, args []string) (*core.UserInputs[core.BlocknodeInputs], error) {
	var err error

	flagProfile, err = common.FlagProfile.Value(cmd, args)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
	}

	if flagProfile == "" {
		return nil, errorx.IllegalArgument.New("profile flag is required")
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

	// Apply overrides from flags to the default block node configs
	// We do this after validating other flags as early as possible
	overrides := config.BlockNodeConfig{
		Namespace:    flagNamespace,
		Release:      flagReleaseName,
		Chart:        flagChartRepo,
		ChartVersion: flagChartVersion,
		Storage: config.BlockNodeStorage{
			BasePath:    flagBasePath,
			ArchivePath: flagArchivePath,
			LivePath:    flagLivePath,
			LogPath:     flagLogPath,
			LiveSize:    flagLiveSize,
			ArchiveSize: flagArchiveSize,
			LogSize:     flagLogSize,
		},
	}

	blockNodeConfig := config.Get().BlockNode
	if overrides.Version != "" {
		blockNodeConfig.Version = overrides.Version
	}

	if overrides.Namespace != "" {
		blockNodeConfig.Namespace = overrides.Namespace
	}
	if overrides.Release != "" {
		blockNodeConfig.Release = overrides.Release
	}
	if overrides.Chart != "" {
		blockNodeConfig.Chart = overrides.Chart
	}
	if overrides.ChartVersion != "" {
		blockNodeConfig.ChartVersion = overrides.ChartVersion
	}
	if overrides.Storage.BasePath != "" {
		blockNodeConfig.Storage.BasePath = overrides.Storage.BasePath
	}
	if overrides.Storage.ArchivePath != "" {
		blockNodeConfig.Storage.ArchivePath = overrides.Storage.ArchivePath
	}
	if overrides.Storage.LivePath != "" {
		blockNodeConfig.Storage.LivePath = overrides.Storage.LivePath
	}
	if overrides.Storage.LogPath != "" {
		blockNodeConfig.Storage.LogPath = overrides.Storage.LogPath
	}
	if overrides.Storage.LiveSize != "" {
		blockNodeConfig.Storage.LiveSize = overrides.Storage.LiveSize
	}
	if overrides.Storage.ArchiveSize != "" {
		blockNodeConfig.Storage.ArchiveSize = overrides.Storage.ArchiveSize
	}
	if overrides.Storage.LogSize != "" {
		blockNodeConfig.Storage.LogSize = overrides.Storage.LogSize
	}

	// validate Block Node configuration after applying overrides
	if err := blockNodeConfig.Validate(); err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "invalid block node configuration")
	}

	return &core.UserInputs[core.BlocknodeInputs]{
		Common: core.CommonInputs{
			Force:            false,
			NodeType:         core.NodeTypeBlock,
			ExecutionOptions: *execOpts,
		},
		Custom: core.BlocknodeInputs{
			Version:      blockNodeConfig.Version,
			Namespace:    blockNodeConfig.Namespace,
			Release:      blockNodeConfig.Release,
			Chart:        blockNodeConfig.Chart,
			ChartVersion: blockNodeConfig.ChartVersion,
			Storage:      blockNodeConfig.Storage,
			Profile:      flagProfile,
			ValuesFile:   validatedValuesFile,
			ReuseValues:  !flagNoReuseValues,
		},
	}, nil
}
