// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"path/filepath"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
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

// RunWorkflowRecordingCluster runs wb, a workflow that installs cluster charts,
// while holding the cluster record's lock. A RecordState step at its end
// records the cluster as it now is, and the run's entry is appended once the
// workflow has finished.
func RunWorkflowRecordingCluster(ctx context.Context, wb *automa.WorkflowBuilder, intent models.Intent, inputs any) error {
	sm, err := clusterStateManager()
	if err != nil {
		return errx.Decorate(errorx.IllegalState.Wrap(err, "failed to create state manager"),
			reasons.StateOperationFailed,
			"Check that "+models.Paths().StateDir+" is readable and writable, then re-run this command.")
	}
	return RunWorkflow(ctx, func() (*automa.Report, error) {
		return runRecordingCluster(ctx, sm, intent, inputs, wb)
	})
}

// runRecordingCluster is the RunWorkflow function of RunWorkflowRecordingCluster.
// It returns no report when the workflow never ran: the lock, the state read or
// the build failed.
func runRecordingCluster(ctx context.Context, sm state.Manager, intent models.Intent, inputs any, wb *automa.WorkflowBuilder) (*automa.Report, error) {
	release, err := lockAndReadCluster(sm, intent.Target)
	if err != nil {
		return nil, err
	}
	defer release()

	wf, err := wb.Steps(steps.RecordState(recordCluster(sm))).Build()
	if err != nil {
		return nil, err
	}
	report := wf.Execute(ctx)
	RecordEntry(sm, steps.WorkflowEntry(ctx, intent, inputs, report), report)
	return report, nil
}

// lockAndReadCluster takes the cluster record's lock and reads the record, so
// the RecordState step keeps what it cannot read. The caller releases the lock.
func lockAndReadCluster(sm state.Manager, target models.TargetType) (func(), error) {
	ids := []state.ComponentID{state.ComponentCluster}
	if err := state.CheckTargetOwns(target, ids); err != nil {
		return nil, err
	}
	release, err := state.AcquireComponentLocks(filepath.Dir(sm.State().StateFile), ids, 0)
	if err != nil {
		return nil, err
	}
	if err := sm.RefreshScoped(ids...); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		release()
		return nil, errx.Decorate(errorx.IllegalState.Wrap(err, "failed to read state"),
			reasons.StateOperationFailed,
			"Fix or move aside the state file named in the error, then re-run this command.")
	}
	return release, nil
}

// recordCluster is the RecordState write of the chart installs: the cluster as
// the checker reads it. The checker keeps the record when it cannot read the
// cluster, so an error from it is an internal fault.
func recordCluster(sm state.Manager) func(ctx context.Context) ([]state.ChangedFile, error) {
	return func(ctx context.Context) ([]state.ChangedFile, error) {
		cs, err := newClusterChecker(sm).RefreshState(ctx)
		if err != nil {
			return nil, errx.Decorate(errorx.InternalError.Wrap(err, "unexpected error from the cluster checker"),
				reasons.Internal)
		}
		st := sm.State()
		st.ClusterState = cs
		return sm.Set(st).Persist(state.ComponentCluster)
	}
}

// entryRecorder appends a finished run's entry; state.Manager and
// *rsl.RuntimeResolver satisfy it.
type entryRecorder interface {
	Record(entry state.ActionHistory, cause error) error
}

// RecordEntry appends entry for the run that produced report. A failed run's
// entry that can't be appended is already a state warning; a successful
// run's becomes a warning in report, since the run itself happened.
func RecordEntry(rec entryRecorder, entry state.ActionHistory, report *automa.Report) {
	if err := rec.Record(entry, steps.FailureOf(report)); err != nil {
		addReportWarnings(report, []string{"The command's action history entry was not appended: " +
			doctor.OperatorMessage(err) + "."})
	}
}
