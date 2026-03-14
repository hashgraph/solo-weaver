// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/automa-saga/automa"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/mattn/go-isatty"
)

// Fallback styles for non-TUI output (simple ANSI).
var (
	fbSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓")
	fbFailed  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗")
	fbSkipped = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("⊘")
	fbRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("•")
	fbPhase   = lipgloss.NewStyle().Bold(true)
	fbDetail  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// ShouldUseTUI returns true if the TUI should be used for output.
// It checks whether stdout is a terminal (or Cygwin pseudo-terminal).
// Non-TTY environments (pipes, CI) automatically get the fallback handler.
func ShouldUseTUI() bool {
	fd := os.Stdout.Fd()
	if isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd) {
		return true
	}
	term := os.Getenv("TERM")
	return term != "" && term != "dumb"
}

// NewTUIHandler creates a notify.Handler that sends messages to the given tea.Program.
func NewTUIHandler(program *tea.Program) *notify.Handler {
	return &notify.Handler{
		StepStart: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			program.Send(StepStartedMsg{ID: stp.Id(), Name: fmt.Sprintf(msg, args...)})
		},
		StepCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			program.Send(StepDoneMsg{ID: stp.Id(), Name: fmt.Sprintf(msg, args...), Status: report.Status, Duration: report.Duration()})
		},
		StepFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			errMsg := ""
			if report.Error != nil {
				errMsg = report.Error.Error()
			}
			program.Send(StepFailedMsg{ID: stp.Id(), Name: fmt.Sprintf(msg, args...), ErrMsg: errMsg})
		},
		StepDetail: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			detail := sanitizeDetail(fmt.Sprintf(msg, args...))
			if detail != "" {
				program.Send(StepDetailMsg{Detail: detail})
			}
		},
		PhaseStart: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			program.Send(PhaseStartedMsg{ID: stp.Id(), Name: fmt.Sprintf(msg, args...)})
		},
		PhaseCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			program.Send(PhaseDoneMsg{ID: stp.Id(), Name: fmt.Sprintf(msg, args...), Status: report.Status, Duration: report.Duration()})
		},
		PhaseFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			errMsg := ""
			if report.Error != nil {
				errMsg = report.Error.Error()
			}
			program.Send(PhaseFailedMsg{ID: stp.Id(), Name: fmt.Sprintf(msg, args...), ErrMsg: errMsg})
		},
	}
}

// ── Fallback helpers ─────────────────────────────────────────────────────

// stepIndent returns the indentation for a step line. Steps inside a phase
// are indented deeper than top-level steps.
func stepIndent(insidePhase bool) string {
	if insidePhase {
		return "    "
	}
	return "  "
}

// completionIcon returns the appropriate icon for a step/phase completion status.
func completionIcon(status automa.TypeStatus) string {
	if status == automa.StatusSkipped {
		return fbSkipped
	}
	return fbSuccess
}

// ── Fallback handler (line-based output) ─────────────────────────────────

// fallbackWriter manages the "active area" — the overwritable lines at the
// bottom of the output. Used in compact mode (VerboseLevel 0).
type fallbackWriter struct {
	mu          sync.Mutex
	out         *os.File
	activeLines int
}

func newFallbackWriter(out *os.File) *fallbackWriter {
	return &fallbackWriter{out: out}
}

// clearInline erases all active overwritable lines using cursor-up and clear.
func (fw *fallbackWriter) clearInline() {
	for fw.activeLines > 0 {
		fmt.Fprint(fw.out, "\r\033[K")
		if fw.activeLines > 1 {
			fmt.Fprint(fw.out, "\033[A") // cursor up one line
		}
		fw.activeLines--
	}
}

// writePermanent clears any inline content and writes a permanent line.
func (fw *fallbackWriter) writePermanent(format string, args ...interface{}) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.clearInline()
	fmt.Fprintf(fw.out, format, args...)
}

// writeInline overwrites active lines in-place. Supports multi-line content
// (lines separated by \n). Tracks the number of lines for proper cleanup.
func (fw *fallbackWriter) writeInline(text string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.clearInline()
	fmt.Fprint(fw.out, text)
	fw.activeLines = 1 + strings.Count(text, "\n")
}

// writeLineRaw clears any transient inline content, then writes a permanent line.
// Used at level 1+ for step start/completion lines.
func (fw *fallbackWriter) writeLineRaw(format string, args ...interface{}) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.clearInline()
	fmt.Fprintf(fw.out, format, args...)
}

// writeDetailTransient overwrites the current line with detail text. Level 2+.
func (fw *fallbackWriter) writeDetailTransient(detail string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fmt.Fprintf(fw.out, "\r\033[K      %s", fbDetail.Render(detail))
	fw.activeLines = 1
}

// NewFallbackHandler creates a line-based handler. Output style depends on VerboseLevel.
// out is the file to write to (usually the real os.Stdout before capture).
func NewFallbackHandler(out *os.File) (*notify.Handler, func(string)) {
	fw := newFallbackWriter(out)
	startTimes := make(map[string]time.Time)

	// Compact mode (level 0) state
	var curPhaseID string
	var curPhaseName string
	var curPhaseStarted time.Time
	var curStepName string
	var curDetail string

	// Ticker for refreshing the progress bar every second
	var tickerStop chan struct{}

	startTicker := func(renderFn func()) {
		if tickerStop != nil {
			close(tickerStop)
		}
		tickerStop = make(chan struct{})
		go func(stop chan struct{}) {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					renderFn()
				case <-stop:
					return
				}
			}
		}(tickerStop)
	}

	stopTicker := func() {
		if tickerStop != nil {
			close(tickerStop)
			tickerStop = nil
		}
	}

	// renderCompactInline renders two overwritable lines: phase name on the
	// first line, progress bar + current step on the second.
	renderCompactInline := func() {
		elapsed := time.Since(curPhaseStarted)
		expected := PhaseBenchmarks[curPhaseID]
		bar := renderTimeProgressBar(elapsed, expected)
		step := curStepName
		if step == "" {
			step = curDetail
		}
		if step == "" {
			fw.writeInline(fmt.Sprintf("  %s %s\n    %s", fbRunning, fbPhase.Render(curPhaseName), bar))
		} else {
			fw.writeInline(fmt.Sprintf("  %s %s\n    %s  %s", fbRunning, fbPhase.Render(curPhaseName), bar, step))
		}
	}

	// Select detail function based on verbosity.
	// Detail is only shown when inside an active phase to avoid orphaned messages.
	detailFn := func(text string) {
		if VerboseLevel == 0 {
			curDetail = text
			if curPhaseName != "" {
				renderCompactInline()
			}
		} else if VerboseLevel >= 2 && curPhaseName != "" {
			fw.writeLineRaw("        %s\n", fbDetail.Render(text))
		}
		// Level 1: no detail shown
	}

	handler := &notify.Handler{
		PhaseStart: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			phaseName := fmt.Sprintf(msg, args...)
			startTimes[stp.Id()] = time.Now()
			curPhaseName = phaseName

			if VerboseLevel == 0 {
				curPhaseID = stp.Id()
				curPhaseStarted = time.Now()
				curStepName = ""
				curDetail = ""
				renderCompactInline()
				startTicker(renderCompactInline)
			} else {
				// Level 1+: show phase header before children
				fw.writeLineRaw("\n  %s %s\n", fbRunning, fbPhase.Render(phaseName))
			}
		},

		PhaseCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			icon := completionIcon(report.Status)
			dur := ""
			if start, ok := startTimes[stp.Id()]; ok {
				dur = " " + formatDuration(time.Since(start).Round(100*time.Millisecond))
			}
			phaseName := fmt.Sprintf(msg, args...)

			curPhaseName = ""
			if VerboseLevel == 0 {
				stopTicker()
				curStepName = ""
				curDetail = ""
				fw.writePermanent("  %s %s%s\n", icon, fbPhase.Render(phaseName), durationStyle.Render(dur))
			} else {
				fw.writeLineRaw("  %s %s%s\n", icon, fbPhase.Render(phaseName), durationStyle.Render(dur))
			}
		},

		PhaseFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			phaseName := fmt.Sprintf(msg, args...)
			curPhaseName = ""
			if VerboseLevel == 0 {
				stopTicker()
				curDetail = ""
				fw.writePermanent("  %s %s\n", fbFailed, fbPhase.Render(phaseName))
				if curStepName != "" {
					fmt.Fprintf(fw.out, "    %s %s\n", fbFailed, curStepName)
				}
				curStepName = ""
			} else {
				fw.writeLineRaw("  %s %s\n", fbFailed, fbPhase.Render(phaseName))
			}
		},

		StepStart: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			stepName := fmt.Sprintf(msg, args...)
			startTimes[stp.Id()] = time.Now() // track start time for duration display
			if VerboseLevel == 0 {
				curStepName = stepName
				curDetail = ""
				if curPhaseName != "" {
					renderCompactInline()
				} else {
					fw.writeInline(fmt.Sprintf("  %s %s", fbRunning, stepName))
				}
			} else if VerboseLevel == 1 {
				// Inline: shows briefly, replaced by ✓ completion
				ind := stepIndent(curPhaseName != "")
				fw.writeInline(fmt.Sprintf("%s%s %s", ind, fbRunning, stepName))
			} else {
				// Level 2: permanent line — detail lines appear indented below
				ind := stepIndent(curPhaseName != "")
				fw.writeLineRaw("%s%s %s\n", ind, fbRunning, stepName)
			}
		},

		StepCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			stepName := fmt.Sprintf(msg, args...)
			icon := completionIcon(report.Status)

			if VerboseLevel == 0 {
				curDetail = ""
				if curPhaseName == "" {
					curStepName = ""
					fw.writePermanent("  %s %s\n", icon, fbPhase.Render(stepName))
				} else {
					curStepName = ""
					renderCompactInline()
				}
			} else {
				ind := stepIndent(curPhaseName != "")
				dur := ""
				if start, ok := startTimes[stp.Id()]; ok {
					dur = " " + formatDuration(time.Since(start).Round(100*time.Millisecond))
				}
				fw.writeLineRaw("%s%s %s%s\n", ind, icon, stepName, durationStyle.Render(dur))
			}
		},

		StepFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			stepName := fmt.Sprintf(msg, args...)
			errMsg := ""
			if report.Error != nil {
				errMsg = report.Error.Error()
			}

			if VerboseLevel == 0 {
				curStepName = stepName
				curDetail = ""
			} else {
				ind := stepIndent(curPhaseName != "")
				fw.writeLineRaw("%s%s %s\n", ind, fbFailed, stepName)
				if errMsg != "" {
					fw.writeLineRaw("%s  %s\n", ind, errorDetailStyle.Render(errMsg))
				}
			}
		},

		StepDetail: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			detail := sanitizeDetail(fmt.Sprintf(msg, args...))
			if detail != "" {
				detailFn(detail)
			}
		},
	}

	return handler, detailFn
}
