// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset a Hedera Block Node by clearing its storage",
	Long: `Reset a Hedera Block Node by clearing all data stored on disk (blocks, logs, etc.).

This command will:
1. Scale down the block node StatefulSet to stop the pod
2. Wait for the block node pod to terminate
3. Clear all files from the storage directories
4. Scale the StatefulSet back up to restart the pod
5. Wait for the block node to become ready

WARNING: This operation is destructive and cannot be undone. All block data will be lost.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		flagProfile, err := common.FlagProfile.Value(cmd, args)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
		}

		// Set the profile in the global config if provided
		if flagProfile != "" {
			config.SetProfile(flagProfile)
		}

		// Apply configuration overrides from flags
		applyConfigOverrides()

		// Validate the configuration
		if err := config.Get().Validate(); err != nil {
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
			Str("nodeType", nodeType).
			Str("profile", flagProfile).
			Any("opts", opts).
			Msg("Resetting Hedera Block Node")

		wb := workflows.WithWorkflowExecutionMode(
			workflows.NewBlockNodeResetWorkflow(), opts)

		common.RunWorkflow(cmd.Context(), wb)

		logx.As().Info().Msg("Successfully reset Hedera Block Node")
		return nil
	},
}

func init() {
	common.FlagStopOnError.SetVarP(resetCmd, &flagStopOnError, false)
	common.FlagRollbackOnError.SetVarP(resetCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError.SetVarP(resetCmd, &flagContinueOnError, false)
}
