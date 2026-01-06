// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/spf13/cobra"
)

const KeyRequireGlobalChecks = "requireGlobalChecks"

// RunWorkflow executes a workflow and handles error
func RunWorkflow(ctx context.Context, b automa.Builder) {
	wb, err := b.Build()
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	report := wb.Execute(ctx)
	CheckWorkflowReport(ctx, report)
}

func CheckWorkflowReport(ctx context.Context, report *automa.Report) {
	if report.Error != nil {
		doctor.CheckReportErr(ctx, report)
	}

	// For each step that failed, run the doctor to diagnose the error
	if len(report.StepReports) > 0 {
		for _, stepReport := range report.StepReports {
			if stepReport.Status == automa.StatusFailed {
				doctor.CheckReportErr(ctx, stepReport)
			}
		}
	}

	timestamp := time.Now().Format("20060102_150405")
	reportPath := path.Join(core.Paths().LogsDir, fmt.Sprintf("setup_report_%s.yaml", timestamp))
	steps.PrintWorkflowReport(report, reportPath)
	logx.As().Info().Str("report_path", reportPath).Msg("Workflow report is saved")
}

// RunGlobalChecks runs global pre-run checks before executing any command.
// It performs unless the command has the annotation "requireGlobalChecks" set to "false".
func RunGlobalChecks(cmd *cobra.Command, args []string) error {
	if cmd == nil {
		return nil
	}

	if !RequireGlobalChecks(cmd) {
		return nil
	}

	RunWorkflow(cmd.Context(), workflows.CheckWeaverInstallationWorkflow())
	return nil
}

// DefaultRunE is a default RunE function that shows help message and provides a placeholder to add common behaviour.
// We always add a run function to commands to ensure cobra marks it as Runnable and allows our commands to invoke
// PersistentPreRunE functions of the root command.
func DefaultRunE(cmd *cobra.Command, args []string) error {
	return cmd.Help()
}

// SkipGlobalChecks marks a command to skip global pre-run checks.
// This is useful for commands like 'install' that need to run before any checks can be performed.
// It sets the annotation "requireGlobalChecks" to "false".
func SkipGlobalChecks(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = make(map[string]string)
	}
	cmd.Annotations[KeyRequireGlobalChecks] = "false"
}

// RequireGlobalChecks checks if a command requires global pre-run checks.
// By default, commands require global checks unless explicitly marked to skip them.
func RequireGlobalChecks(cmd *cobra.Command) bool {
	if cmd.Annotations == nil {
		return true
	}

	val, ok := cmd.Annotations[KeyRequireGlobalChecks]
	if !ok {
		return true
	}

	return val != "false"
}
