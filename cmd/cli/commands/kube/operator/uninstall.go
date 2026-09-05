// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall the solo-operator from the cluster",
	Long:  "Uninstall the solo-operator Helm release. Idempotent: a no-op when the operator is not installed.",
	RunE: func(cmd *cobra.Command, args []string) error {
		execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
		if err != nil {
			return errorx.Decorate(err, "failed to determine execution mode")
		}

		opts := workflows.DefaultWorkflowExecutionOptions()
		opts.ExecutionMode = execMode

		logx.As().Debug().
			Strs("args", args).
			Any("opts", opts).
			Msg("Uninstalling solo-operator")

		wb := workflows.WithWorkflowExecutionMode(workflows.UninstallOperatorWorkflow(), opts)
		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		logx.As().Info().Msg("Successfully uninstalled solo-operator")
		return nil
	},
}

func init() {
	common.FlagStopOnError().SetVarP(uninstallCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(uninstallCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(uninstallCmd, &flagContinueOnError, false)
	uninstallCmd.MarkFlagsMutuallyExclusive(
		common.FlagStopOnError().Name,
		common.FlagContinueOnError().Name,
		common.FlagRollbackOnError().Name,
	)
}
