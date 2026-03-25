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
//	1 (-V) — all steps visible with transient detail text
func (m Model) View() string {
	var b strings.Builder
	if VerboseLevel < 1 {
		b.WriteString("\n")
	}

	prevAnimating := false
	for _, ph := range m.phases {
		// At level 1+, completed phases were already printed above the TUI
		// via program.Println in the handler — only show running phases.
		if VerboseLevel >= 1 && ph.status != statusRunning {
			continue
		}
		// Wait for the previous phase's completion animation before showing the next.
		if ph.status == statusRunning && prevAnimating {
			continue
		}
		// Unnamed default phases (e.g. self-install) always expand to show
		// individual steps — there is no phase header to collapse into.
		if ph.name == "" || VerboseLevel >= 1 {
			b.WriteString(m.renderPhaseExpanded(ph))
		} else {
			b.WriteString(m.renderPhaseCompact(ph))
		}
		// Detect if this completed phase is still animating its progress bar fill.
		if ph.status == statusSuccess && !m.done && !ph.completedAt.IsZero() {
			fill := ph.completedSteps + int(time.Since(ph.completedAt)/(15*time.Millisecond))
			prevAnimating = fill < progressBarWidth
		}
	}

	if m.backgroundDetail != "" && !m.isAnyStepRunning() {
		b.WriteString(fmt.Sprintf("    %s\n", subDetailStyle.Render(m.backgroundDetail)))
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
		fill := progressBarWidth
		if !m.done && !ph.completedAt.IsZero() {
			fill = ph.completedSteps + int(time.Since(ph.completedAt)/(15*time.Millisecond))
		}
		if fill < progressBarWidth {
			bar := renderStepProgressBar(fill, progressBarWidth, ph.duration)
			b.WriteString(fmt.Sprintf("  %s %s  %s\n", m.spinner.View(), name, bar))
		} else {
			b.WriteString(fmt.Sprintf("  %s %s %s\n", successIcon, name, durationStyle.Render(dur)))
		}

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
		bar := renderStepProgressBar(ph.completedSteps, progressBarWidth, elapsed)

		// Find current step name
		stepName := ""
		for _, s := range ph.steps {
			if s.status == statusRunning {
				stepName = s.name
				break
			}
		}

		if stepName != "" {
			b.WriteString(fmt.Sprintf("  %s %s  %s  %s\n", m.spinner.View(), name, bar, durationStyle.Render(stepName)))
		} else {
			b.WriteString(fmt.Sprintf("  %s %s  %s\n", m.spinner.View(), name, bar))
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

	// Phase header — at level 1+, named phase headers are printed permanently
	// via tea.Println on PhaseStartedMsg, so skip them here.
	if ph.name == "" {
		// Unnamed default phases always show their header in the view.
		// (No header to render for unnamed phases.)
	} else if VerboseLevel < 1 {
		// Level 0: phase header is part of the compact view (rendered by renderPhaseCompact).
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
	// Level 1+: header already printed above via tea.Println — skip.

	// All child steps
	indent := "    "
	detailIndent := "        "
	if ph.name == "" {
		indent = "  "
		detailIndent = "      "
	}

	for _, s := range ph.steps {
		// At level 1+, completed steps were printed above via program.Println.
		if VerboseLevel >= 1 && s.status != statusRunning {
			continue
		}
		switch s.status {
		case statusRunning:
			b.WriteString(fmt.Sprintf("%s%s %s\n", indent, m.spinner.View(), stepNameStyle.Render(s.name)))
			if s.detail != "" {
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

const progressBarWidth = 30

// renderStepProgressBar renders a step-count-based progress bar with elapsed time.
func renderStepProgressBar(completed, total int, elapsed time.Duration) string {
	ratio := 0.0
	if total > 0 {
		ratio = float64(completed) / float64(total)
	}

	bar := progress.New(
		progress.WithScaledGradient("#6C6CFF", "#22D3EE"),
		progress.WithWidth(progressBarWidth),
		progress.WithoutPercentage(),
		progress.WithColorProfile(termenv.TrueColor),
	).ViewAs(ratio)

	pct := int(ratio * 100)
	return bar + durationStyle.Render(fmt.Sprintf(" %d%% (%s)", pct, formatDurationShort(elapsed)))
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
// or in line handler mode. reportPath and logPath are included if non-empty.
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
