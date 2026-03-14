// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	tea "github.com/charmbracelet/bubbletea"
	pkgconfig "github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/version"
	"github.com/spf13/cobra"
)

const KeyRequireGlobalChecks = "requireGlobalChecks"

// ensureLogConfig returns the log configuration with file logging enabled
// and sensible defaults for directory and filename.
func ensureLogConfig() logx.LoggingConfig {
	cfg := pkgconfig.Get().Log
	cfg.FileLogging = true
	if cfg.Directory == "" {
		cfg.Directory = models.Paths().LogsDir
	}
	if cfg.Filename == "" {
		cfg.Filename = "solo-provisioner.log"
	}
	return cfg
}

// RunWorkflow executes a workflow and handles error.
// When stdout is a TTY, it renders progress via Bubble Tea with spinners and
// status icons. Otherwise it falls back to a simple line-based output suitable
// for CI and piped environments.
func RunWorkflow(ctx context.Context, b automa.Builder) {
	wb, err := b.Build()
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	if ui.IsUnformatted() {
		runUnformatted(ctx, wb)
	} else if ui.ShouldUseTUI() {
		runWithTUI(ctx, wb)
	} else {
		runWithFallback(ctx, wb)
	}
}

// runUnformatted runs the workflow with no output formatting (-VVV). Zerolog
// writes directly to the console. The default notify handler logs via logx.
func runUnformatted(ctx context.Context, step automa.Step) {
	report := step.Execute(ctx)
	handleWorkflowResult(ctx, report)
}

// runWithTUI runs the workflow with the Bubble Tea TUI active.
func runWithTUI(ctx context.Context, step automa.Step) {
	// Capture os.Stdout BEFORE creating the program so third-party libs
	// (Helm OCI pull, syspkg) write to the captured pipe, not the terminal.
	origStdout, restoreStdout := ui.CaptureOutput()

	m := ui.NewModel()
	program := tea.NewProgram(m, tea.WithOutput(origStdout))

	// Replace the default notify handler to feed messages into the TUI
	notify.SetDefault(ui.NewTUIHandler(program))

	// Re-apply console suppression with the program reference so that a
	// zerolog hook forwards log messages to the TUI as transient "weaving"
	// detail text beneath the currently running step.
	ui.SuppressConsoleLogging(ensureLogConfig(), program)

	// Suppress Go standard log output while the TUI owns stdout.
	origLogOutput := log.Writer()
	log.SetOutput(io.Discard)

	// Execute the workflow in a background goroutine; the TUI event loop
	// owns the main goroutine until the workflow finishes.
	reportCh := make(chan *automa.Report, 1)
	go func() {
		report := step.Execute(ctx)
		reportCh <- report
		program.Send(ui.WorkflowDoneMsg{Report: report})
	}()

	finalModel, err := program.Run()

	// Restore stdout and log before printing the summary.
	restoreStdout()
	log.SetOutput(origLogOutput)

	if err != nil {
		fmt.Printf("TUI error: %v — falling back to simple output\n", err)
		report := <-reportCh
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
	// Capture os.Stdout so third-party libs (Helm OCI, syspkg) don't write
	// stray lines to the terminal. The handler writes to origStdout directly.
	origStdout, restoreStdout := ui.CaptureOutput()

	handler, detailFn := ui.NewFallbackHandler(origStdout)
	notify.SetDefault(handler)

	// Re-apply console suppression with the fallback log hook so that log
	// messages are forwarded as transient detail text (same as TUI mode).
	ui.SuppressConsoleLoggingForFallback(ensureLogConfig(), detailFn)

	// Suppress Go standard log output while the fallback handler owns stdout.
	origLogOutput := log.Writer()
	log.SetOutput(io.Discard)

	report := step.Execute(ctx)

	// Restore stdout and log before printing the summary.
	restoreStdout()
	log.SetOutput(origLogOutput)

	handleWorkflowResult(ctx, report)
}

// RunHandlerWorkflow wraps a handler-based workflow (like block node install)
// with the same output capture, handler setup, and summary rendering as
// RunWorkflow. Use this for commands that call HandleIntent directly instead
// of building an automa workflow.
func RunHandlerWorkflow(ctx context.Context, fn func() (*automa.Report, error)) {
	if ui.IsUnformatted() {
		report, err := fn()
		if err != nil {
			doctor.CheckErr(ctx, err)
		}
		handleWorkflowResult(ctx, report)
		return
	}

	origStdout, restoreOutput := ui.CaptureOutput()

	handler, detailFn := ui.NewFallbackHandler(origStdout)
	notify.SetDefault(handler)

	ui.SuppressConsoleLoggingForFallback(ensureLogConfig(), detailFn)

	origLogOutput := log.Writer()
	log.SetOutput(io.Discard)

	report, err := fn()

	restoreOutput()
	log.SetOutput(origLogOutput)

	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	handleWorkflowResult(ctx, report)
}

// CheckWorkflowReport checks for errors, saves the YAML report, and prints a
// compact summary. Exported for commands that handle reports outside RunWorkflow
// (e.g. block node install/upgrade/reset).
func CheckWorkflowReport(ctx context.Context, report *automa.Report) {
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
	reportPath := path.Join(models.Paths().LogsDir, fmt.Sprintf("setup_report_%s.yaml", timestamp))
	steps.PrintWorkflowReport(report, reportPath)

	// Print compact summary to stdout (after TUI has quit, safe to write)
	totalDuration := report.EndTime.Sub(report.StartTime)
	logPath := path.Join(models.Paths().LogsDir, "solo-provisioner.log")
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

	// Print version header before anything else when verbose.
	fmt.Print(ui.RenderVersionHeader())

	if !RequireGlobalChecks(cmd) {
		return nil
	}

	wb, err := workflows.CheckWeaverInstallationWorkflow().Build()
	if err != nil {
		doctor.CheckErr(cmd.Context(), err)
	}

	// Execute quietly — only check for errors, no summary/report.
	// In raw mode, use the default logx-based notify handler (no formatting).
	if !ui.IsUnformatted() {
		handler, _ := ui.NewFallbackHandler(os.Stdout)
		notify.SetDefault(handler)
	}
	report := wb.Execute(cmd.Context())
	if report != nil && report.Error != nil {
		doctor.CheckReportErr(cmd.Context(), report)
	}

	// Run any pending startup migrations before any command executes.
	if err := RunStartupMigrations(cmd.Context()); err != nil {
		return err
	}

	return nil
}

// RunStartupMigrations runs a single ordered pass over all startup-scoped migrations.
// It is a no-op when no migrations apply.
func RunStartupMigrations(ctx context.Context) error {
	// Read the provisioner version last written to disk — this is the "installed"
	// CLI version before the current binary ran for the first time.
	installedCLIVersion, err := state.ReadProvisionerVersionFromDisk()
	if err != nil {
		return err
	}

	mctx := &migration.Context{
		Component: migration.ScopeStartup,
		Data:      &automa.SyncStateBag{},
	}
	mctx.Data.Set(migration.CtxKeyInstalledCLIVersion, installedCLIVersion)
	mctx.Data.Set(migration.CtxKeyCurrentCLIVersion, version.Number())

	migrations, err := migration.GetApplicableMigrations(migration.ScopeStartup, mctx)
	if err != nil {
		return err
	}

	if len(migrations) == 0 {
		logx.As().Debug().Msg("No startup migrations needed")
		return nil
	}

	migrationWf := migration.MigrationsToWorkflow(migrations, mctx)
	wf, err := migrationWf.Build()
	if err != nil {
		return err
	}

	logx.As().Info().Msg("Running startup migrations...")
	report := wf.Execute(ctx)
	if report.Error != nil {
		return report.Error
	}
	logx.As().Info().Msg("Startup migrations completed successfully")
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
