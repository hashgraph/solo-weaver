// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Teleport Kubernetes cluster agent",
	Long:  "Install the Teleport Kubernetes cluster agent for secure kubectl access and audit logging",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Values file is required for cluster agent installation
		if flagValuesFile == "" {
			return errorx.IllegalArgument.New("--values flag is required for cluster agent installation")
		}

		// Validate the values file path
		validatedValuesFile, err := sanity.ValidateInputFile(flagValuesFile)
		if err != nil {
			return err
		}

		// Apply Teleport configuration overrides
		teleportOverrides := config.TeleportConfig{
			Version:    flagVersion,
			ValuesFile: validatedValuesFile,
		}
		config.OverrideTeleportConfig(teleportOverrides)

		// Validate the configuration
		cfg := config.Get()
		if err := cfg.Teleport.ValidateClusterAgent(); err != nil {
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
			Str("valuesFile", validatedValuesFile).
			Str("version", flagVersion).
			Any("opts", opts).
			Msg("Installing Teleport cluster agent")

		wb := workflows.WithWorkflowExecutionMode(
			workflows.NewTeleportClusterAgentInstallWorkflow(), opts)

		common.RunWorkflow(cmd.Context(), wb)

		logx.As().Info().Msg("Successfully installed Teleport cluster agent")
		return nil
	},
}

func init() {
	common.FlagStopOnError.SetVarP(installCmd, &flagStopOnError, false)
	common.FlagRollbackOnError.SetVarP(installCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError.SetVarP(installCmd, &flagContinueOnError, false)
}
