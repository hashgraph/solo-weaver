// SPDX-License-Identifier: Apache-2.0

package cluster

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
}

func uninstallExecutionOptions() models.WorkflowExecutionOptions {
	switch {
	case flagRollbackOnError:
		return models.WorkflowExecutionOptions{
			ExecutionMode: automa.StopOnError,
			RollbackMode:  automa.RollbackOnError,
		}
	case flagContinueOnError:
		return models.WorkflowExecutionOptions{
			ExecutionMode: automa.ContinueOnError,
			RollbackMode:  automa.ContinueOnError,
		}
	default:
		return models.WorkflowExecutionOptions{
			ExecutionMode: automa.StopOnError,
			RollbackMode:  automa.ContinueOnError,
		}
	}
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall Teleport Kubernetes cluster agent",
	Long:  "Uninstall the Teleport Kubernetes cluster agent and remove its Helm release",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := initializeDependencies(); err != nil {
			return err
		}

		intent := models.Intent{
			Action: models.ActionUninstall,
			Target: models.TargetTeleportCluster,
		}

		inputs := &models.UserInputs[models.TeleportClusterInputs]{
			Common: models.CommonInputs{
				ExecutionOptions: uninstallExecutionOptions(),
			},
		}

		handler, err := teleportHandler.ForClusterAction(intent.Action)
		if err != nil {
			return err
		}

		logx.As().Debug().Msg("Uninstalling Teleport cluster agent")

		common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			return handler.HandleIntent(cmd.Context(), intent, *inputs)
		})

		logx.As().Info().Msg("Successfully uninstalled Teleport cluster agent")
		return nil
	},
}
