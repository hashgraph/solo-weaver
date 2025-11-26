package node

import (
	"fmt"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/cmd/weaver/commands/common"
	"golang.hedera.com/solo-weaver/internal/workflows"
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

		logx.As().Debug().
			Strs("args", args).
			Str("nodeType", nodeType).
			Str("profile", flagProfile).
			Str("valuesFile", flagValuesFile).
			Msg("Installing Hedera Block Node")

		common.RunWorkflow(cmd.Context(), workflows.NewBlockNodeInstallWorkflow(flagProfile, flagValuesFile))

		logx.As().Info().Msg("Successfully installed Hedera Block Node")
		return nil
	},
}

func init() {
	installCmd.Flags().StringVarP(
		&flagValuesFile, "values", "f", "", fmt.Sprintf("Values file"))
}
