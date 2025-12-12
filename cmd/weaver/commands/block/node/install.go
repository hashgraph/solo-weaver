// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"setup"}, // deprecated, will be removed soon
	Short:   "Install a Hedera Block Node",
	Long:    "Run safety checks, setup a K8s cluster and install a Hedera Block Node",
	RunE: func(cmd *cobra.Command, args []string) error {
		flagProfile, err := cmd.Flags().GetString("profile")
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "failed to get profile flag")
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

		logx.As().Debug().
			Strs("args", args).
			Str("nodeType", nodeType).
			Str("profile", flagProfile).
			Str("valuesFile", validatedValuesFile).
			Msg("Installing Hedera Block Node")

		common.RunWorkflow(cmd.Context(), workflows.NewBlockNodeInstallWorkflow(flagProfile, validatedValuesFile))

		logx.As().Info().Msg("Successfully installed Hedera Block Node")
		return nil
	},
}

func init() {
	installCmd.Flags().StringVarP(
		&flagValuesFile, "values", "f", "", fmt.Sprintf("Values file"))
}
