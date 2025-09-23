package commands

import (
	"context"
	"os"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-provisioner/internal/doctor"
	"golang.hedera.com/solo-provisioner/internal/workflows"
)

// createNodeCheckCommand creates a check command for a specific node type
func createNodeCheckCommand(nodeType string) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Runs safety checks to validate system readiness for " + nodeType + " nodes",
		Long:  "Runs safety checks to validate system readiness for deploying " + nodeType + " node components",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.ParseFlags(args); err != nil {
				logx.As().Error().Err(err).Msg("Failed to parse flags")
				os.Exit(1)
			}

			logx.As().Debug().Strs("args", args).Str("nodeType", nodeType).Msg("Running solo provisioner preflight checks")

			runNodeSafetyCheck(cmd.Context(), nodeType)
		},
	}
}

// runNodeSafetyCheck runs safety checks for a specific node type
func runNodeSafetyCheck(ctx context.Context, nodeType string) {
	// get an instance of preflight workflow for the specific node type
	wb, err := workflows.NewNodeSafetyCheckWorkflow(nodeType).Build()
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	report, err := wb.Execute(ctx)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	// For each step that failed, run the doctor to diagnose the error
	if len(report.StepReports) > 0 {
		for _, stepReport := range report.StepReports {
			if stepReport.Status == automa.StatusFailed {
				doctor.CheckErr(ctx, stepReport.Error)
			}
		}
	}

	logx.As().Info().Interface("report", report).Str("nodeType", nodeType).Msg("Node preflight checks completed successfully")
}
