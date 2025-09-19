package commands

import (
	"context"
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
	"os"
)

var systemSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Installs prerequisites and kubernetes for deploying Hedera network components",
	Long:  "Installs the system prerequisites and kubernetes for deploying Hedera network components",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cmd.ParseFlags(args); err != nil {
			logx.As().Error().Err(err).Msg("Failed to parse flags")
			os.Exit(1)
		}

		logx.As().Debug().Strs("args", args).Msg("Running solo provisioner system setup")

		runSystemSetup(cmd.Context())
	},
}

func runSystemSetup(ctx context.Context) {
}
