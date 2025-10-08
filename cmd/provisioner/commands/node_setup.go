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

// createNodeSetupCommand creates a setup command for a specific node type
func createNodeSetupCommand(nodeType string) *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Installs prerequisites and kubernetes for deploying " + nodeType + " network components",
		Long:  "Installs the system prerequisites and kubernetes for deploying " + nodeType + " network components",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.ParseFlags(args); err != nil {
				logx.As().Error().Err(err).Msg("Failed to parse flags")
				os.Exit(1)
			}

			logx.As().Debug().Strs("args", args).Str("nodeType", nodeType).Msg("Running solo provisioner node setup")

			runNodeSetup(cmd.Context(), nodeType)
		},
	}
}

// runNodeSetup runs the setup workflow for a specific node type
func runNodeSetup(ctx context.Context, nodeType string) {
	// get an instance of cluster setup workflow for the specific node type
	wb, err := workflows.NewSetupClusterWorkflow(nodeType).Build()
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	report := wb.Execute(ctx)
	if report.Error != nil {
		doctor.CheckErr(ctx, report.Error)
	}

	// For each step that failed, run the doctor to diagnose the error
	if len(report.StepReports) > 0 {
		for _, stepReport := range report.StepReports {
			if stepReport.Status == automa.StatusFailed {
				doctor.CheckErr(ctx, stepReport.Error)
			}
		}
	}

	logx.As().Info().Interface("report", report).Str("nodeType", nodeType).Msg("Node setup completed successfully")
}
