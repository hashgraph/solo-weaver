// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
	"github.com/hashgraph/solo-weaver/pkg/version"
	"github.com/muesli/termenv"
)

// Styles for TUI rendering.
var (
	successIcon = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓")
	failedIcon  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗")
	skippedIcon = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("⊘")
	pendingIcon = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("◌")

	phaseNameStyle   = lipgloss.NewStyle().Bold(true)
	stepNameStyle    = lipgloss.NewStyle()
	durationStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorDetailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	subDetailStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // greyed out

	summaryPassedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	summarySkippedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	summaryFailedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	summaryLabelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
)

// ── View ─────────────────────────────────────────────────────────────────

// View renders the TUI output. The layout depends on VerboseLevel:
//
//	0 — collapsed phases with progress bar + current step
//	1 — completion lines only (✓/✗/⊘)
//	2 — all steps (start + completion) + detail lines under running step
func (m Model) View() string {
	var b strings.Builder
	header := RenderVersionHeader()
	if header != "" {
		b.WriteString(header)
	} else {
		b.WriteString("\n")
	}

	for _, ph := range m.phases {
		// Unnamed default phases (e.g. self-install) always expand to show
		// individual steps — there is no phase header to collapse into.
		if ph.name == "" || VerboseLevel >= 1 {
			b.WriteString(m.renderPhaseExpanded(ph))
		} else {
			b.WriteString(m.renderPhaseCompact(ph))
		}
	}

	if m.weaving != "" && !m.isAnyStepRunning() {
		b.WriteString(fmt.Sprintf("    %s\n", subDetailStyle.Render(m.weaving)))
	}

	if !m.done {
		b.WriteString("\n")
	}

	return b.String()
}

// renderPhaseCompact renders level 0: collapsed phases + time-based progress bar.
func (m Model) renderPhaseCompact(ph phaseEntry) string {
	var b strings.Builder
	name := phaseNameStyle.Render(ph.name)
	dur := formatDuration(ph.duration)

	switch ph.status {
	case statusSuccess:
		b.WriteString(fmt.Sprintf("  %s %s %s\n", successIcon, name, durationStyle.Render(dur)))

	case statusSkipped:
		b.WriteString(fmt.Sprintf("  %s %s %s\n", skippedIcon, name, durationStyle.Render(dur)))

	case statusFailed:
		b.WriteString(fmt.Sprintf("  %s %s\n", failedIcon, name))
		for _, s := range ph.steps {
			if s.status == statusFailed {
				b.WriteString(fmt.Sprintf("    %s %s\n", failedIcon, stepNameStyle.Render(s.name)))
				if s.errMsg != "" {
					b.WriteString(fmt.Sprintf("      %s\n", errorDetailStyle.Render(s.errMsg)))
				}
			}
		}

	case statusRunning:
		elapsed := time.Since(ph.started)
		expected := PhaseBenchmarks[ph.id]
		bar := renderTimeProgressBar(elapsed, expected)

		// Find current step name
		stepName := ""
		for _, s := range ph.steps {
			if s.status == statusRunning {
				stepName = s.name
				break
			}
		}

		b.WriteString(fmt.Sprintf("  %s %s\n", m.spinner.View(), name))
		if stepName != "" {
			b.WriteString(fmt.Sprintf("    %s  %s\n", bar, durationStyle.Render(stepName)))
		} else {
			b.WriteString(fmt.Sprintf("    %s\n", bar))
		}

	case statusPending:
		b.WriteString(fmt.Sprintf("  %s %s\n", pendingIcon, durationStyle.Render(ph.name)))

	default:
		// Unnamed default phase — show steps directly
		for _, s := range ph.steps {
			switch s.status {
			case statusRunning:
				b.WriteString(fmt.Sprintf("  %s %s\n", m.spinner.View(), stepNameStyle.Render(s.name)))
			case statusSuccess:
				b.WriteString(fmt.Sprintf("  %s %s %s\n", successIcon, stepNameStyle.Render(s.name), durationStyle.Render(formatDuration(s.duration))))
			case statusFailed:
				b.WriteString(fmt.Sprintf("  %s %s\n", failedIcon, stepNameStyle.Render(s.name)))
			}
		}
	}

	return b.String()
}

// renderPhaseExpanded renders level 1+: all steps visible.
func (m Model) renderPhaseExpanded(ph phaseEntry) string {
	var b strings.Builder
	name := phaseNameStyle.Render(ph.name)
	dur := formatDuration(ph.duration)

	// Phase header (skip for unnamed default phases)
	if ph.name != "" {
		switch ph.status {
		case statusRunning:
			b.WriteString(fmt.Sprintf("  %s %s\n", m.spinner.View(), name))
		case statusSuccess:
			b.WriteString(fmt.Sprintf("  %s %s %s\n", successIcon, name, durationStyle.Render(dur)))
		case statusFailed:
			b.WriteString(fmt.Sprintf("  %s %s\n", failedIcon, name))
		case statusSkipped:
			b.WriteString(fmt.Sprintf("  %s %s %s\n", skippedIcon, name, durationStyle.Render(dur)))
		case statusPending:
			b.WriteString(fmt.Sprintf("  %s %s\n", pendingIcon, durationStyle.Render(ph.name)))
		}
	}

	// All child steps
	indent := "    "
	detailIndent := "      "
	if ph.name == "" {
		indent = "  "
		detailIndent = "    "
	}

	for _, s := range ph.steps {
		switch s.status {
		case statusRunning:
			b.WriteString(fmt.Sprintf("%s%s %s\n", indent, m.spinner.View(), stepNameStyle.Render(s.name)))
			if s.detail != "" && VerboseLevel >= 2 {
				b.WriteString(fmt.Sprintf("%s%s\n", detailIndent, subDetailStyle.Render(s.detail)))
			}
		case statusSuccess:
			b.WriteString(fmt.Sprintf("%s%s %s %s\n", indent, successIcon, stepNameStyle.Render(s.name), durationStyle.Render(formatDuration(s.duration))))
		case statusFailed:
			b.WriteString(fmt.Sprintf("%s%s %s\n", indent, failedIcon, stepNameStyle.Render(s.name)))
			if s.errMsg != "" {
				b.WriteString(fmt.Sprintf("%s%s\n", detailIndent, errorDetailStyle.Render(s.errMsg)))
			}
		case statusSkipped:
			b.WriteString(fmt.Sprintf("%s%s %s %s\n", indent, skippedIcon, stepNameStyle.Render(s.name), durationStyle.Render(formatDuration(s.duration))))
		}
	}

	return b.String()
}

// ── Progress bar ─────────────────────────────────────────────────────────

const progressBarWidth = 40

// renderTimeProgressBar renders a time-based progress bar using benchmark data.
// If no benchmark exists, returns an empty string (no bar).
func renderTimeProgressBar(elapsed, expected time.Duration) string {
	if expected <= 0 {
		// No benchmark — just show elapsed time
		return durationStyle.Render(fmt.Sprintf("(%s)", formatDurationShort(elapsed)))
	}

	ratio := float64(elapsed) / float64(expected)
	if ratio > 1 {
		ratio = 1
	}

	bar := progress.New(
		progress.WithScaledGradient("#6C6CFF", "#22D3EE"),
		progress.WithWidth(progressBarWidth),
		progress.WithoutPercentage(),
		progress.WithColorProfile(termenv.TrueColor),
	).ViewAs(ratio)

	// Time estimate
	estimate := ""
	if elapsed < expected {
		remaining := expected - elapsed
		estimate = fmt.Sprintf(" ~%s", formatDurationShort(remaining))
	}

	return bar + durationStyle.Render(estimate)
}

// formatDurationShort formats a duration for the progress bar estimate.
func formatDurationShort(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) - m*60
	if s == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dm%ds", m, s)
}

// ── Version header ───────────────────────────────────────────────────────

var (
	versionNameStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	versionValueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	versionDetailStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// RenderVersionHeader returns a styled version block for display when
// VerboseLevel >= 1. Returns an empty string when verbosity is too low.
func RenderVersionHeader() string {
	if VerboseLevel < 1 {
		return ""
	}
	info := version.Get()
	commit := info.Commit
	if len(commit) > 8 {
		commit = commit[:8]
	}
	return fmt.Sprintf("%s %s %s %s\n\n",
		versionNameStyle.Render("solo-provisioner"),
		versionValueStyle.Render(info.Number),
		versionDetailStyle.Render("("+commit+")"),
		versionDetailStyle.Render(info.GoVersion))
}

// ── Summary ──────────────────────────────────────────────────────────────

// RenderSummaryTable produces a compact summary for use after the TUI quits
// or in fallback mode. reportPath and logPath are included if non-empty.
func RenderSummaryTable(report *automa.Report, totalDuration time.Duration, reportPath string, logPath string) string {
	if report == nil {
		return ""
	}

	passed, skipped, failed := countStatuses(report)

	var b strings.Builder
	b.WriteString("\n  ─────────────────────────────────────────────────\n")

	// Build a human-friendly summary line
	if failed == 0 && skipped == 0 {
		b.WriteString(fmt.Sprintf("  %s\n", summaryPassedStyle.Render("Completed successfully")))
	} else {
		var parts []string
		if passed > 0 {
			parts = append(parts, summaryPassedStyle.Render(fmt.Sprintf("%d passed", passed)))
		}
		if skipped > 0 {
			parts = append(parts, summarySkippedStyle.Render(fmt.Sprintf("%d skipped", skipped)))
		}
		if failed > 0 {
			parts = append(parts, summaryFailedStyle.Render(fmt.Sprintf("%d failed", failed)))
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", summaryLabelStyle.Render("Summary:"), strings.Join(parts, ", ")))
	}
	b.WriteString(fmt.Sprintf("  %s %s\n", summaryLabelStyle.Render("Duration:"), durationStyle.Render(totalDuration.Round(time.Millisecond).String())))
	if reportPath != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", summaryLabelStyle.Render("Report:"), reportPath))
	}
	if logPath != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", summaryLabelStyle.Render("Log:"), logPath))
	}
	b.WriteString("  ─────────────────────────────────────────────────\n")

	return b.String()
}

func countStatuses(report *automa.Report) (passed, skipped, failed int) {
	for _, sr := range report.StepReports {
		if sr == nil {
			continue
		}
		switch sr.Status {
		case automa.StatusSuccess:
			passed++
		case automa.StatusSkipped:
			skipped++
		case automa.StatusFailed:
			failed++
		}
		p, s, f := countStatuses(sr)
		passed += p
		skipped += s
		failed += f
	}
	return
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return ""
	}
	d = d.Round(100 * time.Millisecond)
	if d < time.Second {
		return fmt.Sprintf("(%dms)", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("(%.1fs)", d.Seconds())
	}
	return fmt.Sprintf("(%s)", d.Round(time.Second))
}
