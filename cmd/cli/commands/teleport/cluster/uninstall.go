// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

func init() {
	common.FlagStopOnError().SetVar(uninstallCmd, &flagStopOnError, false)
	common.FlagContinueOnError().SetVar(uninstallCmd, &flagContinueOnError, false)
	common.FlagRollbackOnError().SetVar(uninstallCmd, &flagRollbackOnError, false)
	common.FlagOverrideDrift().SetVar(uninstallCmd, &flagOverrideDrift, false)
	uninstallCmd.MarkFlagsMutuallyExclusive(
		common.FlagStopOnError().Name,
		common.FlagContinueOnError().Name,
		common.FlagRollbackOnError().Name,
	)
}

func uninstallExecutionOptions() (models.WorkflowExecutionOptions, error) {
	execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
	if err != nil {
		return models.WorkflowExecutionOptions{}, err
	}

	rollbackMode := automa.ContinueOnError
	if flagRollbackOnError {
		rollbackMode = automa.RollbackOnError
	}

	return models.WorkflowExecutionOptions{
		ExecutionMode: execMode,
		RollbackMode:  rollbackMode,
	}, nil
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

		inputs, err := uninstallInputs(cmd, args)
		if err != nil {
			return err
		}

		handler, err := teleportHandler.ForClusterAction(intent.Action)
		if err != nil {
			return err
		}

		logx.As().Debug().Msg("Uninstalling Teleport cluster agent")

		if err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			return handler.HandleIntent(cmd.Context(), intent, *inputs)
		}); err != nil {
			return err
		}

		logx.As().Info().Msg("Successfully uninstalled Teleport cluster agent")
		return nil
	},
}

// uninstallInputs builds the uninstall's inputs: the execution options, and
// the flags the drift guard reads.
func uninstallInputs(cmd *cobra.Command, args []string) (*models.UserInputs[models.TeleportClusterInputs], error) {
	executionOptions, err := uninstallExecutionOptions()
	if err != nil {
		return nil, err
	}

	inputs := &models.UserInputs[models.TeleportClusterInputs]{
		Common: models.CommonInputs{
			ExecutionOptions: executionOptions,
		},
	}
	if err := common.ExtractDriftGuardFlags(cmd, args, &inputs.Common); err != nil {
		return nil, err
	}
	return inputs, nil
}
