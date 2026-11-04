// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

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
// provisioner version when the workflow recorded it, or marked NotPersisted
// when it could not.
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
	return entry
}
