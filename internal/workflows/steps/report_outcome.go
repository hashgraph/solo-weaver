// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// OutcomeOf says what a workflow did, for its action history entry: succeeded,
// or failed, rollback or not. A nil report reads as failed: nothing reported
// an outcome.
func OutcomeOf(report *automa.Report) state.Outcome {
	if report == nil || report.Error != nil || report.Status == automa.StatusFailed {
		return state.OutcomeFailed
	}
	return state.OutcomeSucceeded
}

// FailureOf returns the error a failed workflow shows the operator: the
// deepest failed step's, which keeps its reason and hints, or else the
// workflow's own. It is nil when report did not fail.
func FailureOf(report *automa.Report) error {
	if OutcomeOf(report) != state.OutcomeFailed || report == nil {
		return nil
	}
	if err := doctor.DeepestFailureError(report); err != nil {
		return err
	}
	return report.Error
}

// WorkflowEntry is the action history entry for intent's run of a workflow
// that produced report: its outcome, the failure the operator saw, and the
// files the RecordProvisionerVersion and RecordState steps wrote, or marked
// NotPersisted when they could not.
func WorkflowEntry(ctx context.Context, intent models.Intent, inputs any, report *automa.Report) state.ActionHistory {
	entry := state.NewEntry(ctx, intent, inputs)
	entry.Outcome = OutcomeOf(report)
	if ProvisionerVersionPersisted(report) {
		entry.Components = []state.ChangedFile{state.ChangedProvisioner}
	}
	if ProvisionerVersionFailed(report) {
		entry.NotPersisted = true
		entry.Error = "the provisioner version was not recorded; startup migrations may run again on the next invocation"
	}
	if err := FailureOf(report); err != nil {
		entry = entry.WithError(err)
	}
	applyRecordedState(&entry, report)
	return entry
}

// EntryRecorder appends a finished run's entry; state.Manager and
// *rsl.RuntimeResolver satisfy it.
type EntryRecorder interface {
	Record(entry state.ActionHistory, cause error) error
}

// RecordEntry appends entry for the run that produced report. A failed run's
// entry that can't be appended is already a state warning; a successful
// run's becomes a warning in report, since the run itself happened.
func RecordEntry(rec EntryRecorder, entry state.ActionHistory, report *automa.Report) {
	if err := rec.Record(entry, FailureOf(report)); err != nil {
		AddReportWarnings(report, []string{"The command's action history entry was not appended: " +
			doctor.OperatorMessage(err) + "."})
	}
}

// AddReportWarnings appends warnings to the report's own warning, which the
// summary table and the JSON summary both show.
func AddReportWarnings(report *automa.Report, warnings []string) {
	if len(warnings) == 0 {
		return
	}

	lines := make([]string, 0, len(warnings)+1)
	if existing := report.Metadata[models.ReportMetaWarning]; existing != "" {
		lines = append(lines, existing)
	}
	lines = append(lines, warnings...)

	if report.Metadata == nil {
		report.Metadata = map[string]string{}
	}
	report.Metadata[models.ReportMetaWarning] = strings.Join(lines, "\n")
}
