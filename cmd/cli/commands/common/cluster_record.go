// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// Seams so tests can use a temp state dir and a fake cluster.
var (
	clusterStateManager = func() (state.Manager, error) { return state.NewStateManager() }
	newClusterChecker   = reality.NewLiveClusterChecker
)

// RunWorkflowRecordingCluster runs b, a workflow that installs cluster charts,
// while holding the cluster record's lock, then records the cluster as it now
// is, with an action history entry for intent. A failed run records only its
// entry. The record is written inside the run, before its report is saved and
// summarised, so a warning about writing it reaches both.
func RunWorkflowRecordingCluster(ctx context.Context, b automa.Builder, intent models.Intent, inputs any) error {
	sm, err := clusterStateManager()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager")
	}
	entry := state.NewEntry(ctx, intent, inputs)

	var recordErr error
	err = RunWorkflow(ctx, func() (*automa.Report, error) {
		var report *automa.Report
		report, recordErr = runRecordingCluster(ctx, sm, entry, b)
		if report == nil {
			return nil, recordErr // nothing ran: the lock, the state read or the build failed
		}
		return report, nil
	})
	if err != nil {
		return err
	}
	return recordErr
}

// runRecordingCluster runs b under the cluster record's lock and records the
// cluster afterwards. It returns the run's report, nil when b never ran, and
// the error the command should return.
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
				return errorx.IllegalState.Wrap(err, "failed to read the cluster after the run")
			}
			if cs.Unobserved {
				logx.As().Warn().Msg("Could not read the cluster after the run; keeping its recorded state")
				return nil
			}
			st.ClusterState = cs
			return nil
		})
	return report, err
}
