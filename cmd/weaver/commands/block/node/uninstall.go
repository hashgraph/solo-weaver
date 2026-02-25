// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:     "uninstall",
	Aliases: []string{"setup"}, // deprecated, will be removed soon
	Short:   "Uninstall Hedera Block Node",
	Long:    "Uninstall Hedera Block Node",
	RunE: func(cmd *cobra.Command, args []string) error {
		err := initializeDependencies(cmd.Context())
		if err != nil {
			return err
		}

		inputs, err := prepareBlocknodeInputs(cmd, args)
		if err != nil {
			return err
		}

		intent := core.Intent{
			Action: core.ActionUninstall,
			Target: core.TargetBlocknode,
		}

		logx.As().Info().
			Any("intent", intent).
			Any("inputs", inputs).
			Msg("Uninstalling Hedera Block Node")

		report, err := bll.BlockNode().HandleIntent(intent, *inputs)
		if err != nil {
			return err
		}

		common.CheckWorkflowReport(cmd.Context(), report)

		logx.As().Info().Msg("Successfully installed Hedera Block Node")

		return nil
	},
}

func init() {
	initializeExecutionFlags(uninstallCmd)
	common.FlagWithStorageReset.SetVarP(upgradeCmd, &flagWithReset, false)
}
