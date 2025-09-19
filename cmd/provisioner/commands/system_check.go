package commands

import (
	"context"
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-provisioner/internal/doctor"
	"golang.hedera.com/solo-provisioner/internal/workflows"
	"os"
)

var systemSafetyCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Runs safety checks to validate system readiness",
	Long:  "Runs safety checks to validate system readiness for deploying Hedera network components",
	Run: func(cmd *cobra.Command, args []string) {
		if err := cmd.ParseFlags(args); err != nil {
			logx.As().Error().Err(err).Msg("Failed to parse flags")
			os.Exit(1)
		}

		logx.As().Debug().Strs("args", args).Msg("Running solo provisioner preflight checks")

		runSystemSafetyCheck(cmd.Context())
	},
}

func runSystemSafetyCheck(ctx context.Context) {
	// get an instance of preflight workflow
	wb, err := workflows.NewSystemSafetyCheckWorkflow().Build()
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	report, err := wb.Execute(ctx)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	logx.As().Info().Interface("report", report).Msg("Preflight checks completed successfully")
}
