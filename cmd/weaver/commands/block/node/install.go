// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"setup"}, // deprecated, will be removed soon
	Short:   "Install a Hedera Block Node",
	Long:    "Run safety checks, setup a K8s cluster and install a Hedera Block Node",
	RunE: func(cmd *cobra.Command, args []string) error {
		inputs, err := prepareBlocknodeInputs(cmd, args)
		if err != nil {
			return err
		}

		err = initializeDependencies()
		if err != nil {
			return err
		}

		intent := models.Intent{
			Action: models.ActionInstall,
			Target: models.TargetBlockNode,
		}

		logx.As().Info().
			Any("intent", intent).
			Any("inputs", inputs).
			Msg("Installing Hedera Block Node")

		handler, err := blockNodeHandler.ForAction(intent.Action)
		if err != nil {
			return err
		}

		report, err := handler.HandleIntent(cmd.Context(), intent, *inputs)
		if err != nil {
			return err
		}

		common.CheckWorkflowReport(cmd.Context(), report)

		logx.As().Info().Msg("Successfully installed Hedera Block Node")

		return nil
	},
}

func init() {
	common.FlagValuesFile().SetVarP(installCmd, &flagValuesFile, false)
}
