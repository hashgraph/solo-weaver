// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:     "uninstall",
	Aliases: []string{"setup"}, // deprecated, will be removed soon
	Short:   "Uninstall Hedera Block Node",
	Long:    "Uninstall Hedera Block Node",
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
			Action: models.ActionUninstall,
			Target: models.TargetBlockNode,
		}

		logx.As().Info().
			Any("intent", intent).
			Any("inputs", inputs).
			Msg("Uninstalling Hedera Block Node")

		handler, err := blockNodeHandler.ForAction(intent.Action)
		if err != nil {
			return err
		}

		report, err := handler.HandleIntent(cmd.Context(), intent, *inputs)
		if err != nil {
			return err
		}

		common.CheckWorkflowReport(cmd.Context(), report)

		logx.As().Info().Msg("Successfully uninstalled Hedera Block Node")

		return nil
	},
}

func init() {
	common.FlagWithStorageReset.SetVarP(uninstallCmd, &flagWithReset, false)
}
