// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/workflows"
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
		// Token is required for node agent installation
		if flagNodeAgentToken == "" {
			return errorx.IllegalArgument.New("--token flag is required for node agent installation")
		}

		// Apply Teleport configuration overrides
		teleportOverrides := models.TeleportConfig{
			NodeAgentToken:     flagNodeAgentToken,
			NodeAgentProxyAddr: flagNodeAgentProxyAddr,
		}
		config.OverrideTeleportConfig(teleportOverrides)

		// Validate the configuration
		cfg := config.Get()
		if err := cfg.Teleport.ValidateNodeAgent(); err != nil {
			return err
		}

		execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
		if err != nil {
			return errorx.Decorate(err, "failed to determine execution mode")
		}

		opts := workflows.DefaultWorkflowExecutionOptions()
		opts.ExecutionMode = execMode

		logx.As().Debug().
			Strs("args", args).
			Str("proxyAddr", flagNodeAgentProxyAddr).
			Any("opts", opts).
			Msg("Installing Teleport node agent")

		sr, err := common.Setup()
		if err != nil {
			return err
		}

		mr, ok := sr.Runtime.MachineRuntime.(*rsl.MachineRuntimeResolver)
		if !ok {
			return errorx.IllegalArgument.New("expected MachineRuntime to be *rsl.MachineRuntimeResolver but got %T", sr.Runtime.MachineRuntime)
		}

		wb := workflows.WithWorkflowExecutionMode(
			workflows.NewTeleportNodeAgentInstallWorkflow(mr), opts)

		common.RunWorkflowBuilder(cmd.Context(), wb)

		logx.As().Info().Msg("Successfully installed Teleport node agent")
		return nil
	},
}

func init() {
	common.FlagStopOnError().SetVarP(installCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(installCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(installCmd, &flagContinueOnError, false)
}
