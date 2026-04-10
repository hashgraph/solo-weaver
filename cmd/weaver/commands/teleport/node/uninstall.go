// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

func init() {
	common.FlagStopOnError().SetVar(uninstallCmd, &flagStopOnError, false)
	common.FlagContinueOnError().SetVar(uninstallCmd, &flagContinueOnError, false)
	common.FlagRollbackOnError().SetVar(uninstallCmd, &flagRollbackOnError, false)
	uninstallCmd.MarkFlagsMutuallyExclusive(
		common.FlagStopOnError().Name,
		common.FlagContinueOnError().Name,
		common.FlagRollbackOnError().Name,
	)
}

func getUninstallExecutionOptions() (models.WorkflowExecutionOptions, error) {
	switch {
	case flagRollbackOnError:
		return models.WorkflowExecutionOptions{
			ExecutionMode: automa.StopOnError,
			RollbackMode:  automa.RollbackOnError,
		}, nil
	case flagContinueOnError:
		return models.WorkflowExecutionOptions{
			ExecutionMode: automa.ContinueOnError,
			RollbackMode:  automa.ContinueOnError,
		}, nil
	default:
		return models.WorkflowExecutionOptions{
			ExecutionMode: automa.StopOnError,
			RollbackMode:  automa.ContinueOnError,
		}, nil
	}
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall Teleport node agent",
	Long:  "Uninstall the Teleport node agent, stopping the systemd service and removing binaries and configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initializeDependencies(); err != nil {
			return err
		}

		executionOptions, err := getUninstallExecutionOptions()
		if err != nil {
			return err
		}

		intent := models.Intent{
			Action: models.ActionUninstall,
			Target: models.TargetTeleportNode,
		}

		inputs := &models.UserInputs[models.TeleportNodeInputs]{
			Common: models.CommonInputs{
				ExecutionOptions: executionOptions,
			},
		}

		handler, err := teleportHandler.ForNodeAction(intent.Action)
		if err != nil {
			return err
		}

		logx.As().Debug().Msg("Uninstalling Teleport node agent")

		common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			return handler.HandleIntent(cmd.Context(), intent, *inputs)
		})

		logx.As().Info().Msg("Successfully uninstalled Teleport node agent")
		return nil
	},
}
