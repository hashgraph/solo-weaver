// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Teleport Kubernetes cluster agent",
	Long:  "Install the Teleport Kubernetes cluster agent for secure kubectl access and audit logging",
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagValuesFile == "" {
			return errorx.IllegalArgument.New("--values flag is required for cluster agent installation")
		}

		validatedValuesFile, err := sanity.ValidateInputFile(flagValuesFile)
		if err != nil {
			return err
		}

		config.OverrideTeleportConfig(models.TeleportConfig{
			Version:    flagVersion,
			ValuesFile: validatedValuesFile,
		})

		cfg := config.Get()
		if err := cfg.Teleport.ValidateClusterAgent(); err != nil {
			return err
		}

		if err := initializeDependencies(); err != nil {
			return err
		}

		intent := models.Intent{
			Action: models.ActionInstall,
			Target: models.TargetTeleportCluster,
		}

		inputs := &models.UserInputs[models.TeleportClusterInputs]{
			Common: models.CommonInputs{
				ExecutionOptions: models.WorkflowExecutionOptions{
					ExecutionMode: automa.StopOnError,
					RollbackMode:  automa.ContinueOnError,
				},
			},
			Custom: models.TeleportClusterInputs{
				Version:    flagVersion,
				ValuesFile: validatedValuesFile,
			},
		}

		handler, err := teleportHandler.ForClusterAction(intent.Action)
		if err != nil {
			return err
		}

		logx.As().Debug().
			Str("valuesFile", validatedValuesFile).
			Str("version", flagVersion).
			Msg("Installing Teleport cluster agent")

		common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			return handler.HandleIntent(cmd.Context(), intent, *inputs)
		})

		logx.As().Info().Msg("Successfully installed Teleport cluster agent")
		return nil
	},
}

func init() {
	common.FlagStopOnError().SetVarP(installCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(installCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(installCmd, &flagContinueOnError, false)
}
