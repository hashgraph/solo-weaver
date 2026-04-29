// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var (
	flagNoRestart bool

	reconfigureCmd = &cobra.Command{
		Use:   "reconfigure",
		Short: "Reconfigure a Hedera Block Node",
		Long:  "Re-apply configuration to an existing Hedera Block Node deployment without changing its chart version",
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
				Action: models.ActionReconfigure,
				Target: models.TargetBlockNode,
			}

			logx.As().Debug().
				Any("intent", intent).
				Any("inputs", inputs).
				Msg("Reconfiguring Hedera Block Node")

			handler, err := blockNodeHandler.ForAction(intent.Action)
			if err != nil {
				return err
			}

			if err := common.RunWorkflowE(cmd.Context(), func() (*automa.Report, error) {
				return handler.HandleIntent(cmd.Context(), intent, *inputs)
			}); err != nil {
				return err
			}

			logx.As().Info().Msg("Successfully reconfigured Hedera Block Node")
			return nil
		},
	}
)

func init() {
	common.FlagWithStorageReset().SetVarP(reconfigureCmd, &flagWithReset, false)
	common.FlagValuesFile().SetVarP(reconfigureCmd, &flagValuesFile, false)
	common.FlagNoReuseValues().SetVarP(reconfigureCmd, &flagNoReuseValues, false)
	common.FlagNoRestart().SetVar(reconfigureCmd, &flagNoRestart, false)
}
