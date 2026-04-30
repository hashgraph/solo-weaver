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
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	pkgconfig "github.com/hashgraph/solo-weaver/pkg/config"
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

// RunWorkflow executes a workflow function and renders progress via Bubble Tea
// with spinners and status icons, or raw zerolog output when --non-interactive is set.
func RunWorkflow(ctx context.Context, fn func() (*automa.Report, error)) {
	if ui.IsUnformatted() {
		report, err := fn()
		if err != nil {
			doctor.CheckErr(ctx, err)
		}
		handleWorkflowResult(ctx, report)
		return
	}

	// Capture os.Stdout BEFORE creating the program so third-party libs
	// (Helm OCI pull, syspkg) write to the captured pipe, not the terminal.
	origStdout, restoreStdout := ui.CaptureOutput()

	m := ui.NewModel()
	program := tea.NewProgram(m, tea.WithOutput(origStdout))

	// Replace the default notify handler to feed messages into the TUI.
	prevHandler := *notify.As()
	notify.SetDefault(ui.NewTUIHandler(program))
	defer notify.SetDefault(&prevHandler)

	// Re-apply console suppression with the program reference so that a
	// zerolog hook forwards log messages to the TUI as transient detail
	// text beneath the currently running step.
	ui.SuppressConsoleLogging(ensureLogConfig(), program)

	// Suppress Go standard log output while the TUI owns stdout.
	origLogOutput := log.Writer()
	log.SetOutput(io.Discard)

	// Execute the workflow in a background goroutine; the TUI event loop
	// owns the main goroutine until the workflow finishes.
	reportCh := make(chan *automa.Report, 1)
	go func() {
		report, err := fn()
		reportCh <- report
		program.Send(ui.WorkflowDoneMsg{Report: report, Err: err})
	}()

	finalModel, err := program.Run()

	// Restore stdout, log, and detach the TUI log hook before printing the summary.
	restoreStdout()
	log.SetOutput(origLogOutput)
	ui.SuppressConsoleLogging(ensureLogConfig())

	if err != nil {
		fmt.Printf("TUI error: %v\n", err)
		report := <-reportCh
		handleWorkflowResult(ctx, report)
		return
	}

	result, ok := finalModel.(ui.Model)
	if !ok {
		doctor.CheckErr(ctx, fmt.Errorf("unexpected TUI model type"))
	}

	if result.Err() != nil {
		doctor.CheckErr(ctx, result.Err())
	}

	handleWorkflowResult(ctx, result.Report())
}

// RunWorkflowBuilder is a convenience wrapper that builds an automa workflow
// and runs it via RunWorkflow.
func RunWorkflowBuilder(ctx context.Context, b automa.Builder) {
	RunWorkflow(ctx, func() (*automa.Report, error) {
		step, err := b.Build()
		if err != nil {
			return nil, err
		}
		return step.Execute(ctx), nil
	})
}

// RunWorkflowE is like RunWorkflow but propagates errors back to the caller
// instead of calling os.Exit via doctor.CheckErr. Use this inside cobra RunE
// handlers where the error must be returned so tests and callers can inspect it.
func RunWorkflowE(ctx context.Context, fn func() (*automa.Report, error)) error {
	if ui.IsUnformatted() {
		report, err := fn()
		if err != nil {
			return err
		}
		handleWorkflowResult(ctx, report)
		return nil
	}

	// TUI path — same structure as RunWorkflow but returns errors instead of
	// calling doctor.CheckErr / os.Exit.
	origStdout, restoreStdout := ui.CaptureOutput()

	m := ui.NewModel()
	program := tea.NewProgram(m, tea.WithOutput(origStdout))

	prevHandler := *notify.As()
	notify.SetDefault(ui.NewTUIHandler(program))
	defer notify.SetDefault(&prevHandler)

	ui.SuppressConsoleLogging(ensureLogConfig(), program)

	origLogOutput := log.Writer()
	log.SetOutput(io.Discard)

	reportCh := make(chan *automa.Report, 1)
	go func() {
		report, err := fn()
		reportCh <- report
		program.Send(ui.WorkflowDoneMsg{Report: report, Err: err})
	}()

	finalModel, tuiErr := program.Run()

	restoreStdout()
	log.SetOutput(origLogOutput)
	ui.SuppressConsoleLogging(ensureLogConfig())

	if tuiErr != nil {
		fmt.Printf("TUI error: %v\n", tuiErr)
		report := <-reportCh
		handleWorkflowResult(ctx, report)
		return nil
	}

	result, ok := finalModel.(ui.Model)
	if !ok {
		return fmt.Errorf("unexpected TUI model type")
	}

	if result.Err() != nil {
		return result.Err()
	}

	handleWorkflowResult(ctx, result.Report())
	return nil
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
	logCfg := ensureLogConfig()
	timestamp := time.Now().Format("20060102_150405")
	reportPath := path.Join(logCfg.Directory, fmt.Sprintf("setup_report_%s.yaml", timestamp))
	steps.PrintWorkflowReport(report, reportPath)

	// Print compact summary to stdout (after TUI has quit, safe to write)
	totalDuration := report.EndTime.Sub(report.StartTime)
	logPath := path.Join(logCfg.Directory, logCfg.Filename)
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

// RunPersistentPreRun is the body of the root PersistentPreRunE hook.
// It runs before every command and is responsible for:
//  1. Verifying the weaver installation (skipped for commands that opt out via SkipGlobalChecks).
//  2. Running any pending startup migrations.
func RunPersistentPreRun(cmd *cobra.Command, args []string) error {
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

	// Execute quietly — the default logx-based notify handler logs to the file
	// only. Errors are surfaced via CheckReportErr below.
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
