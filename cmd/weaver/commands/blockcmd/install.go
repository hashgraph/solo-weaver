package blockcmd

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/cmd/weaver/commands/common"
	"golang.hedera.com/solo-weaver/internal/workflows"
)

var blockNodeInstallCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"setup"}, // deprecated
	Short:   "Install a Hedera Block Node",
	Long:    "Run safety checks, setup a K8s cluster and install a Hedera Block Node",
	RunE: func(cmd *cobra.Command, args []string) error {
		logx.As().Debug().
			Strs("args", args).
			Str("nodeType", nodeType).
			Str("profile", flagProfile).
			Msg("Installing Hedera Block Node")

		common.RunWorkflow(cmd.Context(), workflows.NewBlockNodeInstallWorkflow(flagProfile))

		logx.As().Info().Msg("Successfully installed Hedera Block Node")
		return nil
	},
}
