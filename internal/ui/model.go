// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Status values for each entry in the TUI.
type entryStatus int

const (
	statusRunning entryStatus = iota
	statusSuccess
	statusFailed
	statusSkipped
	statusPending // phase announced but not yet started
)

// stepEntry tracks the state of a single workflow step in the TUI.
type stepEntry struct {
	id       string
	name     string
	status   entryStatus
	duration time.Duration
	errMsg   string
	started  time.Time
	detail   string // transient sub-status (greyed out below spinner)
}

// phaseEntry groups related steps under a section header.
type phaseEntry struct {
	id       string
	name     string
	status   entryStatus
	duration time.Duration
	errMsg   string
	started  time.Time
	steps    []stepEntry
}

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

// Model is the Bubble Tea model for the TUI display.
type Model struct {
	phases   []phaseEntry
	spinner  spinner.Model
	quitting bool
	report   *automa.Report
	err      error
	done     bool
	start    time.Time
}

// NewModel creates a new TUI model ready for use with tea.NewProgram.
func NewModel() Model {
	s := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("6"))),
	)
	return Model{
		spinner: s,
		start:   time.Now(),
	}
}

// Report returns the workflow report captured after completion.
func (m Model) Report() *automa.Report {
	return m.report
}

// Err returns any build/init error captured during workflow execution.
func (m Model) Err() error {
	return m.err
}

// Init starts the spinner tick.
func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

// currentPhase returns the last phase, or nil if none exist.
func (m *Model) currentPhase() *phaseEntry {
	if len(m.phases) == 0 {
		return nil
	}
	return &m.phases[len(m.phases)-1]
}

// ensureDefaultPhase creates an implicit unnamed phase for steps that arrive
// before any PhaseStartedMsg (e.g., self-install which has no phases).
func (m *Model) ensureDefaultPhase() {
	if len(m.phases) == 0 {
		m.phases = append(m.phases, phaseEntry{
			status:  statusRunning,
			started: time.Now(),
		})
	}
}

// Update handles incoming messages and updates the model state.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	// ── Phase events ──────────────────────────────────────────────────

	case PhaseStartedMsg:
		m.phases = append(m.phases, phaseEntry{
			id:      msg.ID,
			name:    msg.Name,
			status:  statusRunning,
			started: time.Now(),
		})
		return m, nil

	case PhaseDoneMsg:
		for i := len(m.phases) - 1; i >= 0; i-- {
			if m.phases[i].id == msg.ID {
				if msg.Status == automa.StatusSkipped {
					m.phases[i].status = statusSkipped
				} else {
					m.phases[i].status = statusSuccess
				}
				m.phases[i].duration = msg.Duration
				return m, nil
			}
		}
		return m, nil

	case PhaseFailedMsg:
		for i := len(m.phases) - 1; i >= 0; i-- {
			if m.phases[i].id == msg.ID {
				m.phases[i].status = statusFailed
				m.phases[i].errMsg = msg.ErrMsg
				if !m.phases[i].started.IsZero() {
					m.phases[i].duration = time.Since(m.phases[i].started)
				}
				return m, nil
			}
		}
		return m, nil

	// ── Step events ───────────────────────────────────────────────────

	case StepStartedMsg:
		m.ensureDefaultPhase()
		ph := &m.phases[len(m.phases)-1]

		// Check if step already exists in current phase
		found := false
		for j, s := range ph.steps {
			if s.id == msg.ID {
				ph.steps[j].status = statusRunning
				ph.steps[j].name = msg.Name
				ph.steps[j].started = time.Now()
				found = true
				break
			}
		}
		if !found {
			ph.steps = append(ph.steps, stepEntry{
				id:      msg.ID,
				name:    msg.Name,
				status:  statusRunning,
				started: time.Now(),
			})
		}
		return m, nil

	case StepDoneMsg:
		m.ensureDefaultPhase()
		// Search all phases (step may belong to a previous phase)
		for i := len(m.phases) - 1; i >= 0; i-- {
			for j, s := range m.phases[i].steps {
				if s.id == msg.ID {
					if msg.Status == automa.StatusSkipped {
						m.phases[i].steps[j].status = statusSkipped
					} else {
						m.phases[i].steps[j].status = statusSuccess
					}
					m.phases[i].steps[j].duration = msg.Duration
					m.phases[i].steps[j].detail = ""
					return m, nil
				}
			}
		}
		// Step not seen before — add to current phase
		ph := &m.phases[len(m.phases)-1]
		status := statusSuccess
		if msg.Status == automa.StatusSkipped {
			status = statusSkipped
		}
		ph.steps = append(ph.steps, stepEntry{
			id:       msg.ID,
			name:     msg.Name,
			status:   status,
			duration: msg.Duration,
		})
		return m, nil

	case StepFailedMsg:
		m.ensureDefaultPhase()
		for i := len(m.phases) - 1; i >= 0; i-- {
			for j, s := range m.phases[i].steps {
				if s.id == msg.ID {
					m.phases[i].steps[j].status = statusFailed
					m.phases[i].steps[j].errMsg = msg.ErrMsg
					m.phases[i].steps[j].detail = ""
					if !m.phases[i].steps[j].started.IsZero() {
						m.phases[i].steps[j].duration = time.Since(m.phases[i].steps[j].started)
					}
					return m, nil
				}
			}
		}
		// Step not seen before
		ph := &m.phases[len(m.phases)-1]
		ph.steps = append(ph.steps, stepEntry{
			id:     msg.ID,
			name:   msg.Name,
			status: statusFailed,
			errMsg: msg.ErrMsg,
		})
		return m, nil

	case StepDetailMsg:
		// Update the detail text on the last running step in the last running phase
		for i := len(m.phases) - 1; i >= 0; i-- {
			for j := len(m.phases[i].steps) - 1; j >= 0; j-- {
				if m.phases[i].steps[j].status == statusRunning {
					m.phases[i].steps[j].detail = msg.Detail
					return m, nil
				}
			}
		}
		return m, nil

	case WorkflowDoneMsg:
		m.done = true
		m.report = msg.Report
		m.err = msg.Err
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

// View renders the TUI output.
// NOTE: The summary is NOT rendered here. It is printed once by
// handleWorkflowResult() after the TUI quits, using the definitive automa
// report which includes all steps (even those without notify callbacks) and
// the report file path.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString("\n")

	for i, ph := range m.phases {
		b.WriteString(m.renderPhase(ph))
		// Add spacing between phases (but not after the last one)
		if i < len(m.phases)-1 {
			b.WriteString("\n")
		}
	}

	if !m.done {
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderPhase(ph phaseEntry) string {
	var b strings.Builder

	// Render phase header (skip for unnamed default phase)
	if ph.name != "" {
		b.WriteString(m.renderPhaseHeader(ph))
	}

	// Render child steps indented
	for _, s := range ph.steps {
		b.WriteString(m.renderStep(s, ph.name != ""))
	}

	return b.String()
}

func (m Model) renderPhaseHeader(ph phaseEntry) string {
	name := phaseNameStyle.Render(ph.name)
	dur := formatDuration(ph.duration)

	switch ph.status {
	case statusRunning:
		return fmt.Sprintf("  %s %s\n", m.spinner.View(), name)
	case statusSuccess:
		return fmt.Sprintf("  %s %s %s\n", successIcon, name, durationStyle.Render(dur))
	case statusFailed:
		line := fmt.Sprintf("  %s %s\n", failedIcon, name)
		if ph.errMsg != "" {
			line += fmt.Sprintf("    %s\n", errorDetailStyle.Render(ph.errMsg))
		}
		return line
	case statusSkipped:
		return fmt.Sprintf("  %s %s %s\n", skippedIcon, name, durationStyle.Render(dur))
	case statusPending:
		return fmt.Sprintf("  %s %s\n", pendingIcon, durationStyle.Render(ph.name))
	default:
		return ""
	}
}

func (m Model) renderStep(s stepEntry, indented bool) string {
	indent := "  "
	detailIndent := "      "
	if indented {
		indent = "      "
		detailIndent = "          "
	}

	switch s.status {
	case statusRunning:
		line := fmt.Sprintf("%s%s %s\n",
			indent,
			m.spinner.View(),
			stepNameStyle.Render(s.name))
		if s.detail != "" {
			line += fmt.Sprintf("%s%s\n", detailIndent, subDetailStyle.Render(s.detail))
		}
		return line

	case statusSuccess:
		dur := formatDuration(s.duration)
		return fmt.Sprintf("%s%s %s %s\n",
			indent,
			successIcon,
			stepNameStyle.Render(s.name),
			durationStyle.Render(dur))

	case statusFailed:
		line := fmt.Sprintf("%s%s %s\n", indent, failedIcon, stepNameStyle.Render(s.name))
		if s.errMsg != "" {
			line += fmt.Sprintf("%s%s\n", detailIndent, errorDetailStyle.Render(s.errMsg))
		}
		return line

	case statusSkipped:
		dur := formatDuration(s.duration)
		return fmt.Sprintf("%s%s %s %s\n",
			indent,
			skippedIcon,
			stepNameStyle.Render(s.name),
			durationStyle.Render(dur))

	default:
		return ""
	}
}

// RenderSummaryTable produces a compact summary for use after the TUI quits
// or in fallback mode. reportPath and logPath are included if non-empty.
func RenderSummaryTable(report *automa.Report, totalDuration time.Duration, reportPath string, logPath string) string {
	if report == nil {
		return ""
	}

	passed, skipped, failed := countStatuses(report)

	var parts []string
	if passed > 0 {
		parts = append(parts, summaryPassedStyle.Render(fmt.Sprintf("%d succeeded", passed)))
	}
	if skipped > 0 {
		parts = append(parts, summarySkippedStyle.Render(fmt.Sprintf("%d skipped", skipped)))
	}
	if failed > 0 {
		parts = append(parts, summaryFailedStyle.Render(fmt.Sprintf("%d failed", failed)))
	}

	var b strings.Builder
	b.WriteString("\n  ─────────────────────────────────────────────────\n")
	b.WriteString(fmt.Sprintf("  %s %s\n", summaryLabelStyle.Render("Summary:"), strings.Join(parts, ", ")))
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
		// Count nested step reports recursively
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

