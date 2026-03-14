// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"fmt"
	"io"
	"log"
	"path"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/spf13/cobra"
)

const KeyRequireGlobalChecks = "requireGlobalChecks"

// RunWorkflow executes a workflow and handles error.
// When stdout is a TTY (and --no-tui is not set), it renders progress via
// Bubble Tea with spinners and status icons.  Otherwise it falls back to a
// simple line-based output suitable for CI and piped environments.
func RunWorkflow(ctx context.Context, b automa.Builder) {
	wb, err := b.Build()
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	if ui.ShouldUseTUI() {
		runWithTUI(ctx, wb)
	} else {
		runWithFallback(ctx, wb)
	}
}

// runWithTUI runs the workflow with the Bubble Tea TUI active.
func runWithTUI(ctx context.Context, step automa.Step) {
	m := ui.NewModel()
	program := tea.NewProgram(m)

	// Replace the default notify handler to feed messages into the TUI
	notify.SetDefault(ui.NewTUIHandler(program))

	// Suppress Go standard log output while the TUI owns stdout.
	// Third-party libraries (e.g. syspkg) use log.Println which would
	// corrupt the Bubble Tea display.
	origLogOutput := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(origLogOutput)

	// Execute the workflow in a background goroutine; the TUI event loop
	// owns the main goroutine until the workflow finishes.
	go func() {
		report := step.Execute(ctx)
		program.Send(ui.WorkflowDoneMsg{Report: report})
	}()

	finalModel, err := program.Run()
	if err != nil {
		// TUI failed — fall back to simple output for this run
		fmt.Printf("TUI error: %v — falling back to simple output\n", err)
		report := step.Execute(ctx)
		handleWorkflowResult(ctx, report)
		return
	}

	result, ok := finalModel.(ui.Model)
	if !ok {
		doctor.CheckErr(ctx, fmt.Errorf("unexpected TUI model type"))
	}

	handleWorkflowResult(ctx, result.Report())
}

// runWithFallback runs the workflow with the line-based fallback handler.
func runWithFallback(ctx context.Context, step automa.Step) {
	notify.SetDefault(ui.NewFallbackHandler())
	report := step.Execute(ctx)
	handleWorkflowResult(ctx, report)
}

// handleWorkflowResult checks the report for errors, saves the YAML report to
// disk, and prints a compact summary to stdout.
func handleWorkflowResult(ctx context.Context, report *automa.Report) {
	if report == nil {
		return
	}

	// Save the full YAML report to a file (always)
	timestamp := time.Now().Format("20060102_150405")
	reportPath := path.Join(core.Paths().LogsDir, fmt.Sprintf("setup_report_%s.yaml", timestamp))
	steps.PrintWorkflowReport(report, reportPath)

	// Print compact summary to stdout (after TUI has quit, safe to write)
	totalDuration := report.EndTime.Sub(report.StartTime)
	logPath := path.Join(core.Paths().LogsDir, "solo-provisioner.log")
	fmt.Print(ui.RenderSummaryTable(report, totalDuration, reportPath, logPath))

	logx.As().Info().
		Str("report_path", reportPath).
		Str("log_path", logPath).
		Msg("Workflow report is saved")

	// Check for errors and run diagnostics (may call os.Exit)
	if report.Error != nil {
		doctor.CheckReportErr(ctx, report)
	}

	if len(report.StepReports) > 0 {
		for _, stepReport := range report.StepReports {
			if stepReport.Status == automa.StatusFailed {
				doctor.CheckReportErr(ctx, stepReport)
			}
		}
	}
}

// RunGlobalChecks runs global pre-run checks before executing any command.
// It performs unless the command has the annotation "requireGlobalChecks" set to "false".
// Uses quiet execution — no summary or report is printed.
func RunGlobalChecks(cmd *cobra.Command, args []string) error {
	if cmd == nil {
		return nil
	}

	if !RequireGlobalChecks(cmd) {
		return nil
	}

	wb, err := workflows.CheckWeaverInstallationWorkflow().Build()
	if err != nil {
		doctor.CheckErr(cmd.Context(), err)
	}

	// Execute quietly — only check for errors, no summary/report
	notify.SetDefault(ui.NewFallbackHandler())
	report := wb.Execute(cmd.Context())
	if report != nil && report.Error != nil {
		doctor.CheckReportErr(cmd.Context(), report)
	}

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
