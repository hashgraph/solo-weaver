// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/automa-saga/version"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	pkgconfig "github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

const KeyRequireGlobalChecks = "requireGlobalChecks"

// OutputFormat holds the value of the root persistent --output/-o flag. Like
// ui.NonInteractive and ui.VerboseLevel, it is bound directly by the root
// command's SetVarP so the value is visible to this package without threading a
// *cobra.Command through every workflow-run call site. It selects the stdout
// log format: "text" (default) keeps the human-readable console / TUI, while
// "json" emits machine-readable NDJSON log lines plus a final summary object.
var OutputFormat string

// resolveOutputFormat normalizes the raw --output value to "text" or "json".
// Only an explicit "json" selects machine output; every other value (including
// empty or malformed) falls back to "text" so an unrecognized format never
// silently produces unexpected machine output.
func resolveOutputFormat(raw string) string {
	if strings.ToLower(strings.TrimSpace(raw)) == "json" {
		return "json"
	}
	return "text"
}

// OutputIsJSON reports whether the operator requested machine-readable JSON
// output on stdout via --output json.
func OutputIsJSON() bool {
	return resolveOutputFormat(OutputFormat) == "json"
}

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
	type fnResult struct {
		report *automa.Report
		err    error
	}
	resultCh := make(chan fnResult, 1)
	go func() {
		report, err := fn()
		resultCh <- fnResult{report: report, err: err}
		program.Send(ui.WorkflowDoneMsg{Report: report, Err: err})
	}()

	finalModel, tuiErr := program.Run()

	// Restore stdout, log, and detach the TUI log hook before printing the summary.
	restoreStdout()
	log.SetOutput(origLogOutput)
	ui.SuppressConsoleLogging(ensureLogConfig())

	// Always drain resultCh. In the normal path the goroutine sends its result
	// before WorkflowDoneMsg (which causes tea.Quit), so this is a non-blocking
	// read. When the TUI exits early (e.g. SSH/headless: BubbleTea quits without
	// a terminal error before WorkflowDoneMsg arrives), we block here until fn()
	// finishes — preventing a goroutine leak and ensuring we have the real result.
	wfResult := <-resultCh

	if tuiErr != nil {
		fmt.Printf("TUI error: %v\n", tuiErr)
		if wfResult.err != nil {
			return wfResult.err
		}
		return finalizeWorkflowReport(wfResult.report)
	}

	result, ok := finalModel.(ui.Model)
	if !ok {
		return errorx.AssertionFailed.New("unexpected TUI model type")
	}

	// Use the TUI model's report/err (set by WorkflowDoneMsg). Fall back to the
	// goroutine's direct result when the TUI quit before WorkflowDoneMsg was
	// processed — e.g. in SSH/headless mode where BubbleTea exits cleanly but
	// model.Report() returns nil, causing finalizeWorkflowReport(nil) → exit 0.
	report := result.Report()
	fnErr := result.Err()
	if report == nil && fnErr == nil {
		report = wfResult.report
		fnErr = wfResult.err
	}

	if fnErr != nil {
		return fnErr
	}
	return finalizeWorkflowReport(report)
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

// finalizeWorkflowReport saves the YAML report to disk, renders the run summary,
// and returns the deepest failure error in the report tree. In JSON output mode
// the human summary table is replaced by a single compact JSON summary line so
// stdout stays a valid NDJSON stream. Returning
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
	reportErr := steps.PrintWorkflowReport(report, reportPath)

	totalDuration := report.EndTime.Sub(report.StartTime)
	logPath := path.Join(logCfg.Directory, logCfg.Filename)
	daemonLogPath := ""
	if p := path.Join(logCfg.Directory, "solo-provisioner-daemon.log"); fileExists(p) {
		daemonLogPath = p
	}

	// Human mode: render the summary table before the save log (unchanged
	// ordering). JSON mode emits its summary object last (below) so it is the
	// final line finalizeWorkflowReport writes; the table would otherwise
	// corrupt the NDJSON stdout stream.
	if !OutputIsJSON() {
		fmt.Print(ui.RenderSummaryTable(report, totalDuration, reportPath, logPath, daemonLogPath))
	}

	// report_path is the machine-readable handoff for automation, so do not
	// claim a successful save when the write failed.
	if reportErr != nil {
		logx.As().Error().
			Err(reportErr).
			Str("report_path", reportPath).
			Msg("Failed to save workflow report")
	} else {
		logx.As().Info().
			Str("report_path", reportPath).
			Str("log_path", logPath).
			Msg("Workflow report is saved")
	}

	// Emit the machine-readable summary as the final finalize output. It is
	// tagged "type":"summary" so consumers select it regardless of position
	// (a caller may still log further lines after finalize returns).
	if OutputIsJSON() {
		printJSONSummary(report, totalDuration, reportPath)
	}

	if err := deepestFailureError(report); err != nil {
		return err
	}
	return report.Error
}

// printJSONSummary writes a single compact JSON object to stdout summarizing the
// workflow run. It is the final line of the NDJSON stream emitted in --output
// json mode, tagged "type":"summary" so consumers can distinguish it from the
// per-event log lines that precede it. The full report tree is embedded via
// automa.Report's JSON marshaler.
func printJSONSummary(report *automa.Report, duration time.Duration, reportPath string) {
	summary := struct {
		Type       string         `json:"type"`
		Status     string         `json:"status"`
		DurationMS int64          `json:"duration_ms"`
		ReportPath string         `json:"report_path"`
		Report     *automa.Report `json:"report"`
	}{
		Type:       "summary",
		Status:     report.Status.String(),
		DurationMS: duration.Milliseconds(),
		ReportPath: reportPath,
		Report:     report,
	}

	b, err := json.Marshal(summary)
	if err != nil {
		logx.As().Error().Err(err).Msg("Failed to marshal JSON summary")
		return
	}
	fmt.Println(string(b))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
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

	// `--version` on the root command must work on an uninstalled host: the
	// root's RunE prints the version and returns, so running the installation
	// check first would defeat the flag's purpose. Limit the bypass to the
	// root (Parent == nil) so an inherited --version on a subcommand — which
	// has no print path — still goes through the normal checks. Query
	// PersistentFlags directly: it's where the flag is declared, and unlike
	// Flags() it doesn't depend on cobra's pre-Execute merge step (which
	// matters in unit tests that invoke this PreRun without Execute).
	if cmd.Parent() == nil {
		if v, _ := cmd.PersistentFlags().GetBool(FlagVersion().Name); v {
			return nil
		}
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

	// Absent state.yaml reads back as ""; treat it as the baseline so boundary
	// migrations still run instead of being skipped as a fresh install.
	installedCLIVersion = migration.ResolveInstalledCLIVersion(installedCLIVersion)

	mctx := &migration.Context{
		Component: migration.ScopeStartup,
		Data:      &automa.SyncStateBag{},
	}
	mctx.Data.Set(migration.CtxKeyInstalledCLIVersion, installedCLIVersion)
	mctx.Data.Set(migration.CtxKeyCurrentCLIVersion, version.Get().Version)

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
