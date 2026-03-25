// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
)

// NewTUIHandler creates a notify.Handler that sends messages to the given
// tea.Program. At VerboseLevel >= 1, completed steps, phases, and detail
// lines are printed permanently above the TUI via program.Println (which
// guarantees ordering). The model is then updated via program.Send so
// View() can skip already-printed content.
func NewTUIHandler(program *tea.Program) *notify.Handler {
	// insidePhase tracks whether we are currently inside a named phase.
	// Local to this handler instance so state doesn't leak across runs.
	insidePhase := false

	// stepIndent returns the indentation for a step line based on whether
	// we're inside a phase.
	stepIndent := func() string {
		if insidePhase {
			return "    "
		}
		return "  "
	}

	return &notify.Handler{
		PhaseStart: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			name := fmt.Sprintf(msg, args...)
			// Print the running phase header permanently above the TUI.
			if VerboseLevel >= 1 && name != "" {
				if insidePhase {
					// Separator between phases.
					program.Println("")
				}
				runIcon := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("•")
				program.Println(fmt.Sprintf("  %s %s", runIcon, phaseNameStyle.Render(name)))
				insidePhase = true
			}
			program.Send(PhaseStartedMsg{ID: stp.Id(), Name: name})
		},

		PhaseCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			name := fmt.Sprintf(msg, args...)
			dur := report.Duration()
			// Print the phase summary line above the TUI.
			if VerboseLevel >= 1 {
				icon := completionIcon(report.Status)
				program.Println(fmt.Sprintf("  %s %s %s", icon, phaseNameStyle.Render(name), durationStyle.Render(formatDuration(dur))))
			}
			program.Send(PhaseDoneMsg{ID: stp.Id(), Name: name, Status: report.Status, Duration: dur})
		},

		PhaseFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			name := fmt.Sprintf(msg, args...)
			errMsg := ""
			if report.Error != nil {
				errMsg = report.Error.Error()
			}
			if VerboseLevel >= 1 {
				program.Println(fmt.Sprintf("  %s %s", failedIcon, phaseNameStyle.Render(name)))
			}
			program.Send(PhaseFailedMsg{ID: stp.Id(), Name: name, ErrMsg: errMsg})
		},

		StepStart: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			program.Send(StepStartedMsg{ID: stp.Id(), Name: fmt.Sprintf(msg, args...)})
		},

		StepCompletion: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			name := fmt.Sprintf(msg, args...)
			dur := report.Duration()
			// Print the completed step line above the TUI at level 1+.
			if VerboseLevel >= 1 {
				icon := completionIcon(report.Status)
				indent := stepIndent()
				program.Println(fmt.Sprintf("%s%s %s %s", indent, icon, stepNameStyle.Render(name), durationStyle.Render(formatDuration(dur))))
			}
			program.Send(StepDoneMsg{ID: stp.Id(), Name: name, Status: report.Status, Duration: dur})
		},

		StepFailure: func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{}) {
			name := fmt.Sprintf(msg, args...)
			errMsg := ""
			if report.Error != nil {
				errMsg = report.Error.Error()
			}
			if VerboseLevel >= 1 {
				indent := stepIndent()
				program.Println(fmt.Sprintf("%s%s %s", indent, failedIcon, stepNameStyle.Render(name)))
				if errMsg != "" {
					program.Println(fmt.Sprintf("%s    %s", indent, errorDetailStyle.Render(errMsg)))
				}
			}
			program.Send(StepFailedMsg{ID: stp.Id(), Name: name, ErrMsg: errMsg})
		},

		StepDetail: func(ctx context.Context, stp automa.Step, msg string, args ...interface{}) {
			detail := sanitizeDetail(fmt.Sprintf(msg, args...))
			if detail != "" {
				program.Send(StepDetailMsg{Detail: detail})
			}
		},
	}
}

// completionIcon returns the appropriate icon for a step/phase completion status.
func completionIcon(status automa.TypeStatus) string {
	if status == automa.StatusSkipped {
		return skippedIcon
	}
	return successIcon
}
