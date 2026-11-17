// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// Seams so tests can use a temp state dir and a fake cluster.
var (
	clusterStateManager = func() (state.Manager, error) { return state.NewStateManager() }
	newClusterChecker   = reality.NewLiveClusterChecker
)

// RecordStateStepID names the step FailReportOnRecordError adds to a report.
const RecordStateStepID = "record-state"

// RunWorkflowRecordingCluster runs b, a workflow that installs cluster charts,
// while holding the cluster record's lock, then records the cluster as it now
// is, with an action history entry for intent. A failed run records only its
// entry. The record is written inside the run, before its report is saved and
// summarised, so a failure to write it shows in the summary.
func RunWorkflowRecordingCluster(ctx context.Context, b automa.Builder, intent models.Intent, inputs any) error {
	sm, err := clusterStateManager()
	if err != nil {
		return errx.Decorate(errorx.IllegalState.Wrap(err, "failed to create state manager"),
			reasons.StateOperationFailed,
			"Check that "+models.Paths().StateDir+" is readable and writable, then re-run this command.")
	}
	return RunWorkflow(ctx, recordingClusterRun(ctx, sm, state.NewEntry(ctx, intent, inputs), b))
}

// recordingClusterRun is the RunWorkflow function of RunWorkflowRecordingCluster.
// It returns no report when b never ran, so the error alone reaches the
// operator. Otherwise the report carries every failure, including a run that
// succeeded but could not be recorded.
func recordingClusterRun(ctx context.Context, sm state.Manager, entry state.ActionHistory, b automa.Builder) func() (*automa.Report, error) {
	return func() (*automa.Report, error) {
		report, err := runRecordingCluster(ctx, sm, entry, b)
		if report == nil {
			return nil, err
		}
		if err != nil && steps.FailureOf(report) == nil {
			FailReportOnRecordError(report, err)
		}
		return report, nil
	}
}

// runRecordingCluster runs b under the cluster record's lock and records the
// cluster afterwards. It returns nil for the report when b never ran: the lock,
// the state read or the build failed.
func runRecordingCluster(ctx context.Context, sm state.Manager, entry state.ActionHistory, b automa.Builder) (*automa.Report, error) {
	var report *automa.Report
	err := state.RunOwning(sm, entry, []state.ComponentID{state.ComponentCluster},
		func() error {
			wf, err := b.Build()
			if err != nil {
				return err
			}
			report = wf.Execute(ctx)
			return steps.FailureOf(report)
		},
		func(st *state.State) error {
			cs, err := newClusterChecker(sm).RefreshState(ctx)
			if err != nil {
				return errx.Decorate(errorx.InternalError.Wrap(err, "unexpected error from the cluster checker"),
					reasons.Internal)
			}
			st.ClusterState = cs
			return nil
		})
	return report, err
}

// FailReportOnRecordError marks report, from a workflow that succeeded, as
// failed by err, the failure to record its state. The summary, the saved
// report and the command's error then all show it. An err with no reason gets
// one, with a hint to re-run.
func FailReportOnRecordError(report *automa.Report, err error) {
	if _, ok := errx.ReasonOf(err); !ok {
		err = errx.Decorate(err, reasons.StateOperationFailed,
			"The workflow ran but its state was not recorded. Fix the cause above, then re-run the same command; it is safe to repeat.")
	}
	report.StepReports = append(report.StepReports, automa.StepFailureReport(RecordStateStepID, automa.WithError(err)))
	report.Status = automa.StatusFailed
	report.Error = err
}
