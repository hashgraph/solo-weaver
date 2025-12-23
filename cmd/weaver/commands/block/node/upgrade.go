// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var (
	flagNoReuseValues bool

	upgradeCmd = &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade a Hedera Block Node",
		Long:  "Upgrade an existing Hedera Block Node deployment with new configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			flagProfile, err := cmd.Flags().GetString("profile")
			if err != nil {
				return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
			}

			// Apply configuration overrides from flags
			applyConfigOverrides()

			// Validate the configuration after applying overrides
			// This catches invalid storage paths and other configuration issues early,
			// before the workflow starts
			if err := config.Get().Validate(); err != nil {
				return err
			}

			// Validate the values file path if provided
			// This is the primary security validation point for user-supplied file paths.
			var validatedValuesFile string
			if flagValuesFile != "" {
				validatedValuesFile, err = sanity.ValidateInputFile(flagValuesFile)
				if err != nil {
					return err
				}
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
				Str("valuesFile", validatedValuesFile).
				Bool("noReuseValues", flagNoReuseValues).
				Any("opts", opts).
				Msg("Upgrading Hedera Block Node")

			wb := workflows.WithWorkflowExecutionMode(
				workflows.NewBlockNodeUpgradeWorkflow(flagProfile, validatedValuesFile, !flagNoReuseValues), opts)

			common.RunWorkflow(cmd.Context(), wb)

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
