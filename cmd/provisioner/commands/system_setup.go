package commands

import (
	"context"
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-provisioner/internal/doctor"
	"golang.hedera.com/solo-provisioner/internal/workflows"
	"os"
)

var systemSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Installs prerequisites and setup a kubernetes cluster",
	Long:  "Installs the system prerequisites and kubernetes cluster for deploying Hedera network components",
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
	wb, err := workflows.SetupWorkflow().Build()
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	report, err := wb.Execute(ctx)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	if report.Status != automa.StatusSuccess {
		for _, stepReport := range report.StepReports {
			if stepReport.Error != nil {
				logx.As().Error().Err(stepReport.Error).
					Str("step", stepReport.Id).Msgf("Step %q failed", stepReport.Id)
			}
		}

		logx.As().Error().Interface("report", report).Msg("System setup failed")
		os.Exit(1)
	}

	logx.As().Info().Interface("report", report).Msg("System setup completed successfully")
}
