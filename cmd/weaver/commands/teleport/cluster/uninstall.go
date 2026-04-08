// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

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
				ExecutionOptions: models.WorkflowExecutionOptions{
					ExecutionMode: automa.StopOnError,
					RollbackMode:  automa.ContinueOnError,
				},
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
