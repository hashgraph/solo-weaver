// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/stretchr/testify/require"
)

// outcomeStep builds a step whose execute and rollback succeed or fail as told.
func outcomeStep(id string, executeFails, rollbackFails bool) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(id).
		WithExecute(func(context.Context, automa.Step) *automa.Report {
			if executeFails {
				return automa.StepFailureReport(id, automa.WithError(errors.New("execute failed")))
			}
			return automa.StepSuccessReport(id)
		}).
		WithRollback(func(context.Context, automa.Step) *automa.Report {
			if rollbackFails {
				return automa.StepFailureReport(id, automa.WithActionType(automa.ActionRollback),
					automa.WithError(errors.New("rollback failed")))
			}
			return automa.StepSuccessReport(id, automa.WithActionType(automa.ActionRollback))
		})
}

func runOutcomeWorkflow(t *testing.T, mode automa.TypeMode, steps ...automa.Builder) *automa.Report {
	t.Helper()
	wf, err := automa.NewWorkflowBuilder().WithId("outcome").WithExecutionMode(mode).Steps(steps...).Build()
	require.NoError(t, err)
	return wf.Execute(context.Background())
}

// TestOutcomeOf_ReadsTheWorkflowReport: a workflow that succeeded, one that
// failed and was rolled back, and one that stopped without rolling back.
func TestOutcomeOf_ReadsTheWorkflowReport(t *testing.T) {
	require.Equal(t, state.OutcomeSucceeded, OutcomeOf(runOutcomeWorkflow(t, automa.RollbackOnError,
		outcomeStep("a", false, false), outcomeStep("b", false, false))))

	require.Equal(t, state.OutcomeFailed, OutcomeOf(runOutcomeWorkflow(t, automa.RollbackOnError,
		outcomeStep("a", false, false), outcomeStep("b", true, false))),
		"a rollback does not make a failed workflow succeed")

	require.Equal(t, state.OutcomeFailed, OutcomeOf(runOutcomeWorkflow(t, automa.StopOnError,
		outcomeStep("a", false, false), outcomeStep("b", true, false))))

	require.Equal(t, state.OutcomeFailed, OutcomeOf(nil))
}

// TestProvisionerVersionPersisted_ReadsTheStepsMetadata: only a version step
// that wrote provisioner.yaml counts, wherever it sits in the workflow.
func TestProvisionerVersionPersisted_ReadsTheStepsMetadata(t *testing.T) {
	stamp := func(persisted string) *automa.Report {
		return &automa.Report{Id: recordProvisionerVersionStepID, Metadata: automa.StringMap{metaVersionPersisted: persisted}}
	}
	nested := func(r *automa.Report) *automa.Report {
		return &automa.Report{Id: "install", StepReports: []*automa.Report{
			{Id: "setup-kubernetes", StepReports: []*automa.Report{{Id: "kubeadm-init"}, r}},
		}}
	}

	require.True(t, ProvisionerVersionPersisted(nested(stamp("true"))))
	require.False(t, ProvisionerVersionPersisted(nested(stamp("false"))), "a failed persist wrote nothing")
	require.False(t, ProvisionerVersionPersisted(nested(&automa.Report{Id: "other"})), "the step did not run")
	require.False(t, ProvisionerVersionPersisted(nil))
}

// TestProvisionerVersionFailed_ReadsTheStepsMetadata: only a version step whose
// write failed counts; one that found the version current, or wrote it without
// a sync, does not.
func TestProvisionerVersionFailed_ReadsTheStepsMetadata(t *testing.T) {
	stamp := func(meta automa.StringMap) *automa.Report {
		return &automa.Report{Id: recordProvisionerVersionStepID, Metadata: meta}
	}
	nested := func(r *automa.Report) *automa.Report {
		return &automa.Report{Id: "install", StepReports: []*automa.Report{
			{Id: "setup-kubernetes", StepReports: []*automa.Report{{Id: "kubeadm-init"}, r}},
		}}
	}

	require.True(t, ProvisionerVersionFailed(nested(stamp(automa.StringMap{metaVersionPersisted: "false"}))))
	require.False(t, ProvisionerVersionFailed(nested(stamp(automa.StringMap{metaVersionPersisted: "true", metaVersionSynced: "false"}))),
		"a version written but not synced landed")
	require.False(t, ProvisionerVersionFailed(nested(stamp(nil))), "the version was already current")
	require.False(t, ProvisionerVersionFailed(nested(&automa.Report{Id: "other"})), "the step did not run")
	require.False(t, ProvisionerVersionFailed(nil))
}
