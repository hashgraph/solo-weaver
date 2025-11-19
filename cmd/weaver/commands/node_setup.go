package commands

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/doctor"
	"golang.hedera.com/solo-weaver/internal/workflows"
	"golang.hedera.com/solo-weaver/internal/workflows/steps"
)

// createNodeSetupCommand creates a setup command for a specific node type
func createNodeSetupCommand(nodeType string) *cobra.Command {
	var profile string

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Installs prerequisites and kubernetes for deploying " + nodeType + " network components",
		Long:  "Installs the system prerequisites and kubernetes for deploying " + nodeType + " network components",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.ParseFlags(args); err != nil {
				logx.As().Error().Err(err).Msg("Failed to parse flags")
				os.Exit(1)
			}

			logx.As().Debug().
				Strs("args", args).
				Str("nodeType", nodeType).
				Str("profile", profile).
				Msg("Running solo weaver node setup")

			runNodeSetup(cmd.Context(), nodeType, profile)
		},
	}

	cmd.Flags().StringVarP(&profile, "profile", "p", "",
		"Deployment profile (local, perfnet, testnet, mainnet)")
	_ = cmd.MarkFlagRequired("profile")

	return cmd
}

// runNodeSetup runs the setup workflow for a specific node type
func runNodeSetup(ctx context.Context, nodeType string, profile string) {
	// get an instance of cluster setup workflow for the specific node type
	wb, err := workflows.NewSetupClusterWorkflow(nodeType, profile).Build()
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	report := wb.Execute(ctx)
	if report.Error != nil {
		// Check for instructions in any nested reports before showing error
		instructions := doctor.GetInstructionsFromReport(report)
		doctor.CheckErr(ctx, report.Error, instructions)
	}

	// For each step that failed, run the doctor to diagnose the error
	if len(report.StepReports) > 0 {
		for _, stepReport := range report.StepReports {
			if stepReport.Status == automa.StatusFailed {
				instructions := doctor.GetInstructionsFromReport(stepReport)
				doctor.CheckErr(ctx, stepReport.Error, instructions)
			}
		}
	}

	logx.As().Info().
		Str("nodeType", nodeType).
		Str("profile", profile).
		Msg("Node setup completed successfully")

	timestamp := time.Now().Format("20060102_150405")
	reportPath := path.Join(core.Paths().LogsDir, fmt.Sprintf("setup_report_%s.yaml", timestamp))
	steps.PrintWorkflowReport(report, reportPath)

	logx.As().Info().Str("report_path", reportPath).Msg("Setup report saved")
}
