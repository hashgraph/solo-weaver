package commands

import (
	"context"
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
)

var blockNodeDeploy = &cobra.Command{
	Use:   "block-node",
	Short: "Commands to manage and configure block nodes",
	Long:  "Commands to manage and configure block nodes",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cmd.ParseFlags(args); err != nil {
			logx.As().Error().Err(err).Msg("Failed to parse flags")
			return
		}

		logx.As().Debug().Strs("args", args).Msg("Running solo provisioner block-node deploy")

		runBlockNodeDeploy(cmd.Context())
	},
}

func runBlockNodeDeploy(context context.Context) {

}
