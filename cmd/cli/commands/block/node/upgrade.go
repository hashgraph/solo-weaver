// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var (
	flagNoReuseValues bool
	flagWithReset     bool
	flagPurgeStorage  bool

	upgradeCmd = &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade a Hedera Block Node",
		Long:  "Upgrade an existing Hedera Block Node deployment with new configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateBlockNodeFlags(cmd); err != nil {
				return err
			}

			inputs, cv, err := prepareBlocknodeInputs(cmd, args)
			if err != nil {
				return err
			}
			if cv != nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr())
				cv.Print("Selected Inputs")
			}

			err = initializeDependencies()
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

			if err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
				return handler.HandleIntent(cmd.Context(), intent, *inputs)
			}); err != nil {
				return err
			}

			logx.As().Info().Msg("Successfully upgraded Hedera Block Node")
			return nil
		},
	}
)

func init() {
	upgradeCmd.Flags().StringVar(&flagChartVersion, "chart-version", "", "Helm chart version to use")
	common.FlagWithStorageReset().SetVarP(upgradeCmd, &flagWithReset, false)
	common.FlagValuesFile().SetVarP(upgradeCmd, &flagValuesFile, false)
	common.FlagNoReuseValues().SetVarP(upgradeCmd, &flagNoReuseValues, false)
	common.FlagHelmTimeout().SetVarP(upgradeCmd, &flagHelmTimeout, false)
}
