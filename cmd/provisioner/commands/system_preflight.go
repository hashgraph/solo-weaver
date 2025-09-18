package commands

import (
	"context"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-provisioner/internal/doctor"
	"golang.hedera.com/solo-provisioner/internal/workflows"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"os"
)

var preflightCmd = &cobra.Command{
	Use:   "preflight",
	Short: "Runs preflight checks to validate system readiness",
	Long:  "Runs preflight checks to validate system readiness for deploying Hedera network components",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cmd.ParseFlags(args); err != nil {
			logx.As().Error().Err(err).Msg("Failed to parse flags")
			os.Exit(1)
		}

		logx.As().Debug().Strs("args", args).Msg("Running solo provisioner preflight checks")

		runPreflight(cmd.Context())
	},
}

func runPreflight(ctx context.Context) {
	// get an instance of preflight workflow
	wb, err := workflows.NewPreflightWorkflow().Build()
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	report, err := wb.Execute(ctx)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	logx.As().Info().Interface("report", report).Msg("Preflight checks completed successfully")
}
