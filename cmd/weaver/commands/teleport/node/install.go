// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Teleport node agent for SSH access",
	Long:  "Install the Teleport node agent on the host machine for secure SSH access via Teleport",
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagNodeAgentToken == "" {
			return errorx.IllegalArgument.New("--token flag is required for node agent installation")
		}

		config.OverrideTeleportConfig(models.TeleportConfig{
			NodeAgentToken:     flagNodeAgentToken,
			NodeAgentProxyAddr: flagNodeAgentProxyAddr,
		})

		cfg := config.Get()
		if err := cfg.Teleport.ValidateNodeAgent(); err != nil {
			return err
		}

		if err := initializeDependencies(); err != nil {
			return err
		}

		intent := models.Intent{
			Action: models.ActionInstall,
			Target: models.TargetTeleportNode,
		}

		inputs := &models.UserInputs[models.TeleportNodeInputs]{
			Common: models.CommonInputs{
				ExecutionOptions: models.WorkflowExecutionOptions{
					ExecutionMode: automa.StopOnError,
					RollbackMode:  automa.ContinueOnError,
				},
			},
			Custom: models.TeleportNodeInputs{
				Token:     flagNodeAgentToken,
				ProxyAddr: flagNodeAgentProxyAddr,
			},
		}

		handler, err := teleportHandler.ForNodeAction(intent.Action)
		if err != nil {
			return err
		}

		logx.As().Debug().
			Str("proxyAddr", flagNodeAgentProxyAddr).
			Msg("Installing Teleport node agent")

		common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			return handler.HandleIntent(cmd.Context(), intent, *inputs)
		})

		logx.As().Info().Msg("Successfully installed Teleport node agent")
		return nil
	},
}

func init() {
	common.FlagStopOnError().SetVarP(installCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(installCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(installCmd, &flagContinueOnError, false)
}
