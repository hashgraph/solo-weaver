// SPDX-License-Identifier: Apache-2.0

package ui

import (
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
	id             string
	name           string
	status         entryStatus
	duration       time.Duration
	errMsg         string
	started        time.Time
	steps          []stepEntry
	completedSteps int       // incremented on StepDone/StepFailed
	completedAt    time.Time // when the phase transitioned to done (for compact view delay)
}

// Model is the Bubble Tea model for the TUI display.
type Model struct {
	phases           []phaseEntry
	spinner          spinner.Model
	quitting         bool
	report           *automa.Report
	err              error
	done             bool
	start            time.Time
	backgroundDetail string // latest background activity message
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
func (m Model) Report() *automa.Report { return m.report }

// Err returns any build/init error captured during workflow execution.
func (m Model) Err() error { return m.err }

// Init starts the spinner tick.
func (m Model) Init() tea.Cmd { return m.spinner.Tick }

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
				m.phases[i].completedAt = time.Now()
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
					m.phases[i].completedSteps++
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
		ph.completedSteps++
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
					m.phases[i].completedSteps++
					return m, nil
				}
			}
		}
		ph := &m.phases[len(m.phases)-1]
		ph.steps = append(ph.steps, stepEntry{
			id:     msg.ID,
			name:   msg.Name,
			status: statusFailed,
			errMsg: msg.ErrMsg,
		})
		ph.completedSteps++
		return m, nil

	case StepDetailMsg:
		m.backgroundDetail = msg.Detail
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
		m.backgroundDetail = ""
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

// isAnyStepRunning returns true if any step in any phase is currently running.
func (m Model) isAnyStepRunning() bool {
	for i := len(m.phases) - 1; i >= 0; i-- {
		for _, s := range m.phases[i].steps {
			if s.status == statusRunning {
				return true
			}
		}
	}
	return false
}
