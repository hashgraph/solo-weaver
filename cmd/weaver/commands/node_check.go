package commands

import (
	"context"
	"os"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/internal/doctor"
	"golang.hedera.com/solo-weaver/internal/workflows"
)

// createNodeCheckCommand creates a check command for a specific node type
func createNodeCheckCommand(nodeType string) *cobra.Command {
	var profile string

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Runs safety checks to validate system readiness for " + nodeType + " nodes",
		Long:  "Runs safety checks to validate system readiness for deploying " + nodeType + " node components",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.ParseFlags(args); err != nil {
				logx.As().Error().Err(err).Msg("Failed to parse flags")
				os.Exit(1)
			}

			logx.As().Debug().
				Strs("args", args).
				Str("nodeType", nodeType).
				Str("profile", profile).
				Msg("Running solo weaver preflight checks")

			runNodeSafetyCheck(cmd.Context(), nodeType, profile)
		},
	}

	cmd.Flags().StringVarP(&profile, "profile", "p", "",
		"Deployment profile (local, perfnet, testnet, mainnet)")
	_ = cmd.MarkFlagRequired("profile")

	return cmd
}

// runNodeSafetyCheck runs safety checks for a specific node type
func runNodeSafetyCheck(ctx context.Context, nodeType string, profile string) {
	// get an instance of preflight workflow for the specific node type
	wb, err := workflows.NewNodeSafetyCheckWorkflow(nodeType, profile).Build()
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
		Interface("report", report).
		Str("nodeType", nodeType).
		Str("profile", profile).
		Msg("Node preflight checks completed successfully")
}
