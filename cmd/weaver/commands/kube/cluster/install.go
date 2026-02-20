// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a Kubernetes Cluster",
	Long:  "Run safety checks, setup a K8s cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		flagProfile, err := common.FlagProfile.Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
		}

		if flagProfile == "" {
			return errorx.IllegalArgument.New("profile flag is required")
		}

		// Validate node type and profile early for better error messages
		if !hardware.IsValidNodeType(flagNodeType) {
			return errorx.IllegalArgument.New("unsupported node type: %q. Supported types: %v",
				flagNodeType, hardware.SupportedNodeTypes())
		}

		if !hardware.IsValidProfile(flagProfile) {
			return errorx.IllegalArgument.New("unsupported profile: %q. Supported profiles: %v",
				flagProfile, hardware.SupportedProfiles())
		}

		// Set the profile in the global config so other components can access it
		config.SetProfile(flagProfile)

		execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
		if err != nil {
			return errorx.Decorate(err, "failed to determine execution mode")
		}

		opts := workflows.DefaultWorkflowExecutionOptions()
		opts.ExecutionMode = execMode

		logx.As().Debug().
			Strs("args", args).
			Any("opts", opts).
			Msg("Installing Kubernetes Cluster")

		skipHardwareChecks, err := cmd.Flags().GetBool(common.FlagSkipHardwareChecks.Name)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get %s flag", common.FlagSkipHardwareChecks.Name)
		}
		wb := workflows.WithWorkflowExecutionMode(workflows.InstallClusterWorkflow(flagNodeType, flagProfile, skipHardwareChecks), opts)
		common.RunWorkflow(cmd.Context(), wb)

		logx.As().Info().Msg("Successfully installed Kubernetes Cluster")
		return nil
	},
}

func init() {
	common.FlagNodeType.SetVarP(installCmd, &flagNodeType, true)
	common.FlagStopOnError.SetVarP(installCmd, &flagStopOnError, false)
	common.FlagRollbackOnError.SetVarP(installCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError.SetVarP(installCmd, &flagContinueOnError, false)
}
