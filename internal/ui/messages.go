// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"time"

	"github.com/automa-saga/automa"
)

// StepStartedMsg is sent when a workflow step begins execution.
type StepStartedMsg struct {
	ID   string
	Name string
}

// StepDoneMsg is sent when a workflow step completes successfully or is skipped.
type StepDoneMsg struct {
	ID       string
	Name     string
	Status   automa.TypeStatus
	Duration time.Duration
}

// StepFailedMsg is sent when a workflow step fails.
type StepFailedMsg struct {
	ID     string
	Name   string
	ErrMsg string
}

// StepDetailMsg is a transient sub-status update shown as greyed-out text
// under the currently running step. Used by child steps to indicate progress
// without creating a new top-level entry in the TUI.
type StepDetailMsg struct {
	Detail string
}

// PhaseStartedMsg is sent when a major workflow phase begins.
// The TUI renders this as a section header with subsequent steps indented below.
type PhaseStartedMsg struct {
	ID   string
	Name string
}

// PhaseDoneMsg is sent when a major workflow phase completes.
type PhaseDoneMsg struct {
	ID       string
	Name     string
	Status   automa.TypeStatus
	Duration time.Duration
}

// PhaseFailedMsg is sent when a major workflow phase fails.
type PhaseFailedMsg struct {
	ID     string
	Name   string
	ErrMsg string
}

// WorkflowDoneMsg is sent when the entire workflow finishes execution.
type WorkflowDoneMsg struct {
	Report *automa.Report
	Err    error
}
