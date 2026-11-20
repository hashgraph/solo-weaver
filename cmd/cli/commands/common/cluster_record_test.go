// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

type clusterFunc func() (state.ClusterState, error)

func (f clusterFunc) RefreshState(context.Context) (state.ClusterState, error) { return f() }

var liveAlloy = map[string]state.HelmReleaseInfo{
	"alloy": {Name: "grafana-alloy", Namespace: "grafana-alloy", ChartVersion: "1.13.1", Status: release.StatusDeployed},
}

// useCluster makes the recorder read a cluster whose releases are releases,
// or whose read fails with err.
func useCluster(t *testing.T, releases map[string]state.HelmReleaseInfo, err error) {
	t.Helper()
	orig := newClusterChecker
	t.Cleanup(func() { newClusterChecker = orig })
	newClusterChecker = func(state.Manager) reality.Checker[state.ClusterState] {
		return clusterFunc(func() (state.ClusterState, error) {
			return state.ClusterState{Created: true, Releases: releases}, err
		})
	}
}

func newRecordingManager(t *testing.T) (state.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	sm, err := state.NewStateManager(state.WithStateFile(filepath.Join(dir, state.StateFileName)))
	require.NoError(t, err)
	return sm, dir
}

func oneStepWorkflow(fails bool) *automa.WorkflowBuilder {
	return workflowWith(fails, automa.StopOnError, nil)
}

// workflowWith is a one-step chart install in mode. rolledBack, when set, is
// told whether the step was rolled back.
func workflowWith(fails bool, mode automa.TypeMode, rolledBack *bool) *automa.WorkflowBuilder {
	step := automa.NewStepBuilder().WithId("install-chart").
		WithExecute(func(context.Context, automa.Step) *automa.Report {
			if fails {
				return automa.StepFailureReport("install-chart", automa.WithError(errors.New("helm install failed")))
			}
			return automa.StepSuccessReport("install-chart")
		}).
		WithRollback(func(context.Context, automa.Step) *automa.Report {
			if rolledBack != nil {
				*rolledBack = true
			}
			return automa.StepSuccessReport("install-chart", automa.WithActionType(automa.ActionRollback))
		})
	return automa.NewWorkflowBuilder().WithId("install").WithExecutionMode(mode).Steps(step)
}

var alloyIntent = models.Intent{Action: models.ActionInstall, Target: models.TargetAlloy}

func loadClusterRecord(t *testing.T, dir string) state.ClusterState {
	t.Helper()
	sm, err := state.NewStateManager(state.WithStateFile(filepath.Join(dir, state.StateFileName)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	return sm.State().ClusterState
}

func lastEntry(t *testing.T, dir string) state.ActionHistory {
	t.Helper()
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	return entries[0]
}

func hasStep(report *automa.Report, id string) bool {
	for _, sr := range report.StepReports {
		if sr.Id == id {
			return true
		}
	}
	return false
}

// A successful run records the cluster in its RecordState step, and one entry
// naming the cluster file.
func TestRunRecordingCluster_SuccessRecordsTheCluster(t *testing.T) {
	useCluster(t, liveAlloy, nil)
	sm, dir := newRecordingManager(t)

	report, err := runRecordingCluster(context.Background(), sm, alloyIntent, nil, oneStepWorkflow(false))

	require.NoError(t, err)
	require.True(t, report.IsSuccess())
	require.True(t, hasStep(report, steps.RecordStateStepID))
	require.Equal(t, liveAlloy, loadClusterRecord(t, dir).Releases)
	entry := lastEntry(t, dir)
	require.Equal(t, state.OutcomeSucceeded, entry.Outcome)
	require.Equal(t, models.TargetAlloy, entry.Intent.Target)
	require.Equal(t, []state.ChangedFile{state.ComponentCluster.File()}, entry.Components)
}

// A failed run stops before the RecordState step: no cluster record, and an
// entry that says it failed.
func TestRunRecordingCluster_FailedRunRecordsOnlyItsEntry(t *testing.T) {
	useCluster(t, liveAlloy, nil)
	sm, dir := newRecordingManager(t)

	report, err := runRecordingCluster(context.Background(), sm, alloyIntent, nil, oneStepWorkflow(true))

	require.NoError(t, err)
	require.ErrorContains(t, steps.FailureOf(report), "helm install failed")
	require.NoFileExists(t, state.ComponentFilePath(dir, state.ComponentCluster))
	require.Equal(t, state.OutcomeFailed, lastEntry(t, dir).Outcome)
}

// failingPersist is a state manager whose Persist fails.
type failingPersist struct{ state.Manager }

func (f failingPersist) Set(s state.State) state.Writer {
	f.Manager.Set(s)
	return f
}

func (failingPersist) Persist(...state.ComponentID) ([]state.ChangedFile, error) {
	return nil, errors.New("disk full")
}

// A record that can't be written leaves the run successful, with a warning in
// the summary and an entry marked NotPersisted.
func TestRunRecordingCluster_RecordFailureIsAWarning(t *testing.T) {
	useCluster(t, liveAlloy, nil)
	sm, dir := newRecordingManager(t)

	report, err := runRecordingCluster(context.Background(), failingPersist{sm}, alloyIntent, nil, oneStepWorkflow(false))

	require.NoError(t, err)
	require.True(t, report.IsSuccess())
	require.Contains(t, strings.Join(ui.CollectWarnings(report), "\n"), "The state was not recorded: disk full")
	entry := lastEntry(t, dir)
	require.Equal(t, state.OutcomeSucceeded, entry.Outcome)
	require.True(t, entry.NotPersisted)
	require.Contains(t, entry.Error, "disk full")
	require.Equal(t, reasons.StateOperationFailed, entry.Reason)
}

// Under --rollback-on-error, a record that can't be written does not roll back
// the chart the run installed.
func TestRunRecordingCluster_RecordFailureRollsNothingBack(t *testing.T) {
	useCluster(t, liveAlloy, nil)
	sm, _ := newRecordingManager(t)
	rolledBack := false

	report, err := runRecordingCluster(context.Background(), failingPersist{sm}, alloyIntent, nil,
		workflowWith(false, automa.RollbackOnError, &rolledBack))

	require.NoError(t, err)
	require.True(t, report.IsSuccess())
	require.False(t, rolledBack)
}

// The cluster checker reports a cluster it cannot read by keeping the record,
// so an error from it is an internal fault: nothing is recorded, and the entry
// says why.
func TestRunRecordingCluster_UnexpectedCheckerErrorIsNotRecorded(t *testing.T) {
	useCluster(t, nil, errors.New("checker bug"))
	sm, dir := newRecordingManager(t)

	report, err := runRecordingCluster(context.Background(), sm, alloyIntent, nil, oneStepWorkflow(false))

	require.NoError(t, err)
	require.True(t, report.IsSuccess())
	require.NoFileExists(t, state.ComponentFilePath(dir, state.ComponentCluster))
	entry := lastEntry(t, dir)
	require.True(t, entry.NotPersisted)
	require.Contains(t, entry.Error, "unexpected error from the cluster checker")
	require.Equal(t, reasons.Internal, entry.Reason)
}

// A cluster the refresh could not read leaves the recorded cluster as it was.
func TestRunRecordingCluster_UnobservedClusterKeepsTheRecord(t *testing.T) {
	sm, dir := newRecordingManager(t)
	st := sm.State()
	st.ClusterState = state.ClusterState{Created: true, Releases: liveAlloy}
	require.NoError(t, sm.Set(st).Commit(state.ActionHistory{}, state.ComponentCluster))

	orig := newClusterChecker
	t.Cleanup(func() { newClusterChecker = orig })
	newClusterChecker = func(sm state.Manager) reality.Checker[state.ClusterState] {
		c, err := reality.NewClusterChecker(sm,
			func() (reality.HelmManager, error) { return nil, errors.New("not called") },
			func() reality.ClusterReachability { return reality.Unobservable })
		require.NoError(t, err)
		return c
	}

	_, err := runRecordingCluster(context.Background(), sm, alloyIntent, nil, oneStepWorkflow(false))

	require.NoError(t, err)
	got := loadClusterRecord(t, dir)
	require.True(t, got.Created)
	require.Equal(t, liveAlloy, got.Releases)
}

// A command that cannot take the cluster lock fails before its workflow runs
// and records nothing.
func TestRunRecordingCluster_FailsFastWhenTheClusterIsLocked(t *testing.T) {
	useCluster(t, liveAlloy, nil)
	sm, dir := newRecordingManager(t)
	release, err := state.AcquireComponentLocks(dir, []state.ComponentID{state.ComponentCluster}, 0)
	require.NoError(t, err)
	defer release()

	report, err := runRecordingCluster(context.Background(), sm, alloyIntent, nil, oneStepWorkflow(false))

	require.Error(t, err)
	require.Nil(t, report, "the workflow must not run")
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestRunWorkflowRecordingCluster_StateManagerErrorIsDecorated(t *testing.T) {
	orig := clusterStateManager
	t.Cleanup(func() { clusterStateManager = orig })
	clusterStateManager = func() (state.Manager, error) { return nil, errors.New("permission denied") }

	err := RunWorkflowRecordingCluster(context.Background(), oneStepWorkflow(false), alloyIntent, nil)

	require.ErrorContains(t, err, "permission denied")
	reason, _ := errx.ReasonOf(err)
	require.Equal(t, reasons.StateOperationFailed, reason)
	hints, _ := errx.Hints(err)
	require.NotEmpty(t, hints)
}

// failingRecord is an entry recorder whose append fails.
type failingRecord struct{}

func (failingRecord) Record(state.ActionHistory, error) error {
	return errors.New("history not writable")
}

// A successful run whose entry can't be appended still succeeds, with a warning.
func TestRecordEntry_LostEntryOfASuccessfulRunIsAWarning(t *testing.T) {
	report := automa.StepSuccessReport("install")

	RecordEntry(failingRecord{}, state.ActionHistory{}, report)

	require.True(t, report.IsSuccess())
	require.Contains(t, strings.Join(ui.CollectWarnings(report), "\n"), "history not writable")
}
