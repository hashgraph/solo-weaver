// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a Kubernetes Cluster",
	Long:  "Run safety checks, setup a K8s cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Cluster install is workload-agnostic: it validates only the Kubernetes
		// substrate hardware floor, so no --profile or --node-type is required.
		// Both flags are deprecated: still accepted (hidden) for backward compatibility
		// but ignored. Tell the operator if either was explicitly provided.
		if cmd.Flags().Changed(common.FlagProfile().Name) {
			logx.As().Warn().Msgf("--%s is deprecated and ignored for 'kube cluster install': it validates only the Kubernetes substrate floor. Profile-based sizing applies to 'block node' commands.",
				common.FlagProfile().Name)
		}
		if cmd.Flags().Changed(common.FlagNodeType().Name) {
			logx.As().Warn().Msgf("--%s is deprecated and ignored for 'kube cluster install': it validates only the Kubernetes substrate floor.",
				common.FlagNodeType().Name)
		}

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

		skipHardwareChecks, err := common.FlagSkipHardwareChecks().Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get %s flag", common.FlagSkipHardwareChecks().Name)
		}

		sr, err := common.Setup()
		if err != nil {
			return err
		}

		mr, ok := sr.Runtime.MachineRuntime.(*rsl.MachineRuntimeResolver)
		if !ok {
			return errorx.IllegalArgument.New("expected MachineRuntime to be *rsl.MachineRuntimeResolver but got %T", sr.Runtime.MachineRuntime)
		}

		wb := workflows.WithWorkflowExecutionMode(workflows.InstallClusterWorkflow(skipHardwareChecks, mr), opts)
		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		logx.As().Info().Msg("Successfully installed Kubernetes Cluster")
		return nil
	},
}

func init() {
	// Deprecated: --node-type no longer affects cluster install (substrate-only floor).
	// Kept hidden for backward compatibility; the value is ignored (see RunE notice).
	common.FlagNodeType().SetVarPHidden(installCmd, &flagNodeType, false)
	common.FlagStopOnError().SetVarP(installCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(installCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(installCmd, &flagContinueOnError, false)
	installCmd.MarkFlagsMutuallyExclusive(
		common.FlagStopOnError().Name,
		common.FlagContinueOnError().Name,
		common.FlagRollbackOnError().Name,
	)
}
