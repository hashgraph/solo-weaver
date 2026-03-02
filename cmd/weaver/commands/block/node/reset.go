// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
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
		err := initializeDependencies()
		if err != nil {
			return err
		}

		inputs, err := prepareBlocknodeInputs(cmd, args)
		if err != nil {
			return err
		}

		intent := models.Intent{
			Action: models.ActionReset,
			Target: models.TargetBlocknode,
		}

		logx.As().Debug().
			Any("intent", intent).
			Any("inputs", inputs).
			Msg("Resetting Hedera Block Node")

		report, err := blockNodeHandler.HandleIntent(cmd.Context(), intent, *inputs)
		if err != nil {
			return err
		}
		common.CheckWorkflowReport(cmd.Context(), report)

		logx.As().Info().Msg("Successfully reset Hedera Block Node")
		return nil
	},
}

func init() {
	initializeExecutionFlags(resetCmd)
}
