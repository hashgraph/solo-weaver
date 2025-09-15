package commands

import (
	"context"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"os"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup a hardware for Hedera network components",
	Long:  "Setup a hardware with Kubernetes cluster and solo operator to run Hedera network components",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cmd.ParseFlags(args); err != nil {
			logx.As().Error().Err(err).Msg("Failed to parse flags")
			os.Exit(1)
		}

		logx.As().Debug().Strs("args", args).Msg("Running solo provisioner setup")

		runSetup(cmd.Context())
	},
}

func runSetup(ctx context.Context) {
}
