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
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	pkgconfig "github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	version "github.com/hashgraph/solo-weaver/pkg/version/cli"
)

const KeyRequireGlobalChecks = "requireGlobalChecks"

// ensureLogConfig returns the log configuration with file logging enabled
// and a directory default. The CLI's log filename is hardcoded — not
// config-overridable — so it stays distinct from the daemon's log file
// ("solo-provisioner-daemon.log") and the summary table's logPath always
// reflects where the CLI actually writes. See the matching hardcode in
// cmd/cli/commands/root.go's initConfig.
func ensureLogConfig() logx.LoggingConfig {
	cfg := pkgconfig.Get().Log
	cfg.FileLogging = true
	if cfg.Directory == "" {
		cfg.Directory = models.Paths().LogsDir
	}
	cfg.Filename = "solo-provisioner.log"
	return cfg
}

// RunWorkflow executes a workflow function and renders progress via Bubble Tea
// with spinners and status icons, or raw zerolog output when --non-interactive is set.
// It returns the first error encountered (workflow execution error or step failure)
// so cobra RunE handlers can propagate it to the top-level error handler in main.go.
func RunWorkflow(ctx context.Context, fn func() (*automa.Report, error)) error {
	if ui.IsUnformatted() {
		report, err := fn()
		if err != nil {
			return err
		}
		return finalizeWorkflowReport(report)
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

	finalModel, tuiErr := program.Run()

	// Restore stdout, log, and detach the TUI log hook before printing the summary.
	restoreStdout()
	log.SetOutput(origLogOutput)
	ui.SuppressConsoleLogging(ensureLogConfig())

	if tuiErr != nil {
		fmt.Printf("TUI error: %v\n", tuiErr)
		return finalizeWorkflowReport(<-reportCh)
	}

	result, ok := finalModel.(ui.Model)
	if !ok {
		return errorx.AssertionFailed.New("unexpected TUI model type")
	}

	if result.Err() != nil {
		return result.Err()
	}

	return finalizeWorkflowReport(result.Report())
}

// RunWorkflowBuilder is a convenience wrapper that builds an automa workflow
// and runs it via RunWorkflow.
func RunWorkflowBuilder(ctx context.Context, b automa.Builder) error {
	return RunWorkflow(ctx, func() (*automa.Report, error) {
		step, err := b.Build()
		if err != nil {
			return nil, err
		}
		return step.Execute(ctx), nil
	})
}

// finalizeWorkflowReport saves the YAML report to disk, prints a compact
// summary, and returns the deepest failure error in the report tree. Returning
// the deepest error (rather than the immediate top-level step error) preserves
// errorx properties such as ErrPropertyResolution: when a sub-workflow step
// fails, automa sets the parent's step-report Error to a fresh
// "workflow X completed with N step failures" wrapper that does NOT preserve
// the leaf error's properties. Walking the StepReports tree to the leaf keeps
// the user-facing resolution panel intact.
func finalizeWorkflowReport(report *automa.Report) error {
	if report == nil {
		return nil
	}

	logCfg := ensureLogConfig()
	timestamp := time.Now().Format("20060102_150405")
	reportPath := path.Join(logCfg.Directory, fmt.Sprintf("setup_report_%s.yaml", timestamp))
	steps.PrintWorkflowReport(report, reportPath)

	totalDuration := report.EndTime.Sub(report.StartTime)
	logPath := path.Join(logCfg.Directory, logCfg.Filename)
	daemonLogPath := path.Join(logCfg.Directory, "solo-provisioner-daemon.log")
	fmt.Print(ui.RenderSummaryTable(report, totalDuration, reportPath, logPath, daemonLogPath))

	logx.As().Info().
		Str("report_path", reportPath).
		Str("log_path", logPath).
		Msg("Workflow report is saved")

	if err := deepestFailureError(report); err != nil {
		return err
	}
	return report.Error
}

// deepestFailureError descends through nested StepReports to return the
// leaf-level failed step's Error. Mirrors doctor.GetInstructionsFromReport's
// recursion so that errorx properties attached on a deeply nested step
// (e.g. preflight superuser check, weaver installation check) are not masked
// by automa's workflow-level "completed with N failures" wrapper.
func deepestFailureError(r *automa.Report) error {
	if r == nil {
		return nil
	}
	for _, sr := range r.StepReports {
		if sr == nil || sr.Status != automa.StatusFailed {
			continue
		}
		if deeper := deepestFailureError(sr); deeper != nil {
			return deeper
		}
		if sr.Error != nil {
			return sr.Error
		}
		return errorx.IllegalState.New("step %q failed", sr.Id)
	}
	return nil
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
