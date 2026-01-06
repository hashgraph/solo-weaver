// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"setup"}, // deprecated, will be removed soon
	Short:   "Install a Hedera Block Node",
	Long:    "Run safety checks, setup a K8s cluster and install a Hedera Block Node",
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
			Action: core.ActionInstall,
			Target: core.TargetBlocknode,
		}

		logx.As().Debug().
			Any("intent", intent).
			Any("inputs", inputs).
			Msg("Installing Hedera Block Node")

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
	common.FlagValuesFile.SetVarP(installCmd, &flagValuesFile, false)
	common.FlagStopOnError.SetVarP(installCmd, &flagStopOnError, false)
	common.FlagRollbackOnError.SetVarP(installCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError.SetVarP(installCmd, &flagContinueOnError, false)
}
