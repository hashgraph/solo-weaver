// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/spf13/cobra"
)

var (
	flagNoReuseValues bool

	upgradeCmd = &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade a Hedera Block Node",
		Long:  "Upgrade an existing Hedera Block Node deployment with new configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := initializeDependencies(cmd.Context())
			if err != nil {
				return err
			}

			inputs, err := prepareUserInputs(cmd, args)
			if err != nil {
				return err
			}

			intent := core.Intent{
				Action: core.ActionUpgrade,
				Target: core.TargetBlocknode,
			}

			logx.As().Debug().
				Any("intent", intent).
				Any("inputs", inputs).
				Msg("Uninstalling Hedera Block Node")

			report, err := bll.BlockNode().HandleIntent(intent, *inputs)
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
	upgradeCmd.Flags().StringVarP(
		&flagValuesFile, "values", "f", "", fmt.Sprintf("Values file"))
	upgradeCmd.Flags().BoolVar(
		&flagNoReuseValues, "no-reuse-values", false, "Don't reuse the last release's values (resets to chart defaults)")

	common.FlagStopOnError.SetVarP(upgradeCmd, &flagStopOnError, false)
	common.FlagRollbackOnError.SetVarP(upgradeCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError.SetVarP(upgradeCmd, &flagContinueOnError, false)
}
