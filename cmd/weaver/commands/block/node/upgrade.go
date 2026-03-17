// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var (
	flagNoReuseValues bool
	flagWithReset     bool

	upgradeCmd = &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade a Hedera Block Node",
		Long:  "Upgrade an existing Hedera Block Node deployment with new configuration",
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
				Action: models.ActionUpgrade,
				Target: models.TargetBlockNode,
			}

			logx.As().Debug().
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

			logx.As().Info().Msg("Successfully upgraded Hedera Block Node")
			return nil
		},
	}
)

func init() {
	common.FlagWithStorageReset.SetVarP(upgradeCmd, &flagWithReset, false)
	common.FlagValuesFile.SetVarP(upgradeCmd, &flagValuesFile, false)
	common.FlagNoReuseValues.SetVarP(upgradeCmd, &flagNoReuseValues, false)
}
