// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"context"
	"fmt"
	"os"
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
)

// ShouldUseTUI returns true if the TUI should be used for output.
// It checks that stdout is a terminal and the NoTUI flag is not set.
func ShouldUseTUI() bool {
	return !NoTUI && isatty.IsTerminal(os.Stdout.Fd())
}

// NewTUIHandler creates a notify.Handler that sends messages to the given tea.Program.
// This bridges the existing automa workflow notification system into the Bubble Tea TUI.
func NewTUIHandler(program *tea.Program) *notify.Handler {
	return &notify.Handler{
		StepStart: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			program.Send(StepStartedMsg{
				ID:   stp.Id(),
				Name: fmt.Sprintf(msg, args...),
			})
		},
		StepCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			program.Send(StepDoneMsg{
				ID:       stp.Id(),
				Name:     fmt.Sprintf(msg, args...),
				Status:   report.Status,
				Duration: report.Duration(),
			})
		},
		StepFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			errMsg := ""
			if report.Error != nil {
				errMsg = report.Error.Error()
			}
			program.Send(StepFailedMsg{
				ID:     stp.Id(),
				Name:   fmt.Sprintf(msg, args...),
				ErrMsg: errMsg,
			})
		},
		StepDetail: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			program.Send(StepDetailMsg{
				Detail: fmt.Sprintf(msg, args...),
			})
		},
		PhaseStart: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			program.Send(PhaseStartedMsg{
				ID:   stp.Id(),
				Name: fmt.Sprintf(msg, args...),
			})
		},
		PhaseCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			program.Send(PhaseDoneMsg{
				ID:       stp.Id(),
				Name:     fmt.Sprintf(msg, args...),
				Status:   report.Status,
				Duration: report.Duration(),
			})
		},
		PhaseFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			errMsg := ""
			if report.Error != nil {
				errMsg = report.Error.Error()
			}
			program.Send(PhaseFailedMsg{
				ID:     stp.Id(),
				Name:   fmt.Sprintf(msg, args...),
				ErrMsg: errMsg,
			})
		},
	}
}

// NewFallbackHandler creates a simple line-based notify.Handler for non-TTY environments.
// Output is plain text with unicode status symbols, suitable for CI logs and piped output.
func NewFallbackHandler() *notify.Handler {
	stepStarts := make(map[string]time.Time)

	return &notify.Handler{
		StepStart: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			stepStarts[stp.Id()] = time.Now()
			fmt.Fprintf(os.Stdout, "    %s %s\n", fbRunning, fmt.Sprintf(msg, args...))
		},
		StepCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			icon := fbSuccess
			if report.Status == automa.StatusSkipped {
				icon = fbSkipped
			}
			dur := ""
			if start, ok := stepStarts[stp.Id()]; ok {
				elapsed := time.Since(start).Round(100 * time.Millisecond)
				dur = fmt.Sprintf(" (%s)", elapsed)
			}
			fmt.Fprintf(os.Stdout, "    %s %s%s\n", icon, fmt.Sprintf(msg, args...), dur)
		},
		StepFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			fmt.Fprintf(os.Stdout, "    %s %s\n", fbFailed, fmt.Sprintf(msg, args...))
		},
		StepDetail: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			// Silent in fallback mode — detail is only for TUI sub-status
		},
		PhaseStart: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			stepStarts[stp.Id()] = time.Now()
			fmt.Fprintf(os.Stdout, "\n  %s %s\n", fbRunning, fbPhase.Render(fmt.Sprintf(msg, args...)))
		},
		PhaseCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			icon := fbSuccess
			if report.Status == automa.StatusSkipped {
				icon = fbSkipped
			}
			dur := ""
			if start, ok := stepStarts[stp.Id()]; ok {
				elapsed := time.Since(start).Round(100 * time.Millisecond)
				dur = fmt.Sprintf(" (%s)", elapsed)
			}
			fmt.Fprintf(os.Stdout, "  %s %s%s\n", icon, fbPhase.Render(fmt.Sprintf(msg, args...)), dur)
		},
		PhaseFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			fmt.Fprintf(os.Stdout, "  %s %s\n", fbFailed, fbPhase.Render(fmt.Sprintf(msg, args...)))
		},
	}
}

