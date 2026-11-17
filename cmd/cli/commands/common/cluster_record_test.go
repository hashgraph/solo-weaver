// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
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

func oneStepWorkflow(fails bool) automa.Builder {
	step := automa.NewStepBuilder().WithId("install-chart").
		WithExecute(func(context.Context, automa.Step) *automa.Report {
			if fails {
				return automa.StepFailureReport("install-chart", automa.WithError(errors.New("helm install failed")))
			}
			return automa.StepSuccessReport("install-chart")
		})
	return automa.NewWorkflowBuilder().WithId("install").Steps(step)
}

var alloyInstall = state.ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetAlloy}}

func loadClusterRecord(t *testing.T, dir string) state.ClusterState {
	t.Helper()
	sm, err := state.NewStateManager(state.WithStateFile(filepath.Join(dir, state.StateFileName)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	return sm.State().ClusterState
}

// A successful run records the cluster's releases and one entry naming the
// cluster file.
func TestRunRecordingCluster_SuccessRecordsTheCluster(t *testing.T) {
	useCluster(t, liveAlloy, nil)
	sm, dir := newRecordingManager(t)

	report, err := runRecordingCluster(context.Background(), sm, alloyInstall, oneStepWorkflow(false))

	require.NoError(t, err)
	require.NotNil(t, report)
	require.Equal(t, liveAlloy, loadClusterRecord(t, dir).Releases)
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, state.OutcomeSucceeded, entries[0].Outcome)
	require.Equal(t, models.TargetAlloy, entries[0].Intent.Target)
	require.Equal(t, []state.ChangedFile{state.ComponentCluster.File()}, entries[0].Components)
}

// A failed run writes no cluster record and records that it failed.
func TestRunRecordingCluster_FailedRunRecordsOnlyItsEntry(t *testing.T) {
	useCluster(t, liveAlloy, nil)
	sm, dir := newRecordingManager(t)

	report, err := runRecordingCluster(context.Background(), sm, alloyInstall, oneStepWorkflow(true))

	require.ErrorContains(t, err, "helm install failed")
	require.NotNil(t, report)
	require.NoFileExists(t, state.ComponentFilePath(dir, state.ComponentCluster))
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, state.OutcomeFailed, entries[0].Outcome)
}

// The cluster checker reports a cluster it cannot read by keeping the record,
// so an error from it is an internal fault: the run fails and is not recorded.
func TestRunRecordingCluster_UnexpectedCheckerErrorFails(t *testing.T) {
	useCluster(t, nil, errors.New("checker bug"))
	sm, dir := newRecordingManager(t)

	_, err := runRecordingCluster(context.Background(), sm, alloyInstall, oneStepWorkflow(false))

	require.ErrorContains(t, err, "unexpected error from the cluster checker")
	reason, _ := errx.ReasonOf(err)
	require.Equal(t, reasons.Internal, reason)
	require.NoFileExists(t, state.ComponentFilePath(dir, state.ComponentCluster))
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.True(t, entries[0].NotPersisted)
}

// A cluster the refresh could not read leaves the recorded cluster as it was,
// and the run still records its entry.
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

	_, err := runRecordingCluster(context.Background(), sm, alloyInstall, oneStepWorkflow(false))

	require.NoError(t, err)
	got := loadClusterRecord(t, dir)
	require.True(t, got.Created)
	require.Equal(t, liveAlloy, got.Releases)
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	require.Equal(t, state.OutcomeSucceeded, entries[len(entries)-1].Outcome)
}

// A command that cannot take the cluster lock fails before its workflow runs
// and records nothing.
func TestRunRecordingCluster_FailsFastWhenTheClusterIsLocked(t *testing.T) {
	useCluster(t, liveAlloy, nil)
	sm, dir := newRecordingManager(t)
	release, err := state.AcquireComponentLocks(dir, []state.ComponentID{state.ComponentCluster}, 0)
	require.NoError(t, err)
	defer release()

	report, err := runRecordingCluster(context.Background(), sm, alloyInstall, oneStepWorkflow(false))

	require.Error(t, err)
	require.Nil(t, report, "the workflow must not run")
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

// failingCommits is a state manager whose commits fail.
type failingCommits struct{ state.Manager }

func (f failingCommits) Set(s state.State) state.Writer {
	f.Manager.Set(s)
	return f
}

func (failingCommits) Commit(state.ActionHistory, ...state.ComponentID) error {
	return errors.New("disk full")
}

// A run that succeeded but could not be recorded comes back as a failed
// report, so the summary and the command's error agree.
func TestRecordingClusterRun_RecordFailureFailsTheReport(t *testing.T) {
	useCluster(t, liveAlloy, nil)
	sm, _ := newRecordingManager(t)

	report, err := recordingClusterRun(context.Background(), failingCommits{sm}, alloyInstall, oneStepWorkflow(false))()

	require.NoError(t, err, "the failure travels in the report")
	require.NotNil(t, report)
	require.Equal(t, automa.StatusFailed, report.Status)
	failure := steps.FailureOf(report)
	require.ErrorContains(t, failure, "disk full")
	reason, _ := errx.ReasonOf(failure)
	require.Equal(t, reasons.StateOperationFailed, reason)
	require.Equal(t, RecordStateStepID, report.StepReports[len(report.StepReports)-1].Id)
}

// A failed run keeps its own failure; the recorder adds nothing to it.
func TestRecordingClusterRun_FailedRunKeepsItsFailure(t *testing.T) {
	useCluster(t, liveAlloy, nil)
	sm, _ := newRecordingManager(t)

	report, err := recordingClusterRun(context.Background(), sm, alloyInstall, oneStepWorkflow(true))()

	require.NoError(t, err)
	require.ErrorContains(t, steps.FailureOf(report), "helm install failed")
	for _, sr := range report.StepReports {
		require.NotEqual(t, RecordStateStepID, sr.Id)
	}
}

// A workflow that never ran returns no report, only the error.
func TestRecordingClusterRun_NothingRanReturnsTheError(t *testing.T) {
	useCluster(t, liveAlloy, nil)
	sm, dir := newRecordingManager(t)
	release, err := state.AcquireComponentLocks(dir, []state.ComponentID{state.ComponentCluster}, 0)
	require.NoError(t, err)
	defer release()

	report, err := recordingClusterRun(context.Background(), sm, alloyInstall, oneStepWorkflow(false))()

	require.Error(t, err)
	require.Nil(t, report)
}

func TestRunWorkflowRecordingCluster_StateManagerErrorIsDecorated(t *testing.T) {
	orig := clusterStateManager
	t.Cleanup(func() { clusterStateManager = orig })
	clusterStateManager = func() (state.Manager, error) { return nil, errors.New("permission denied") }

	err := RunWorkflowRecordingCluster(context.Background(), oneStepWorkflow(false), alloyInstall.Intent, nil)

	require.ErrorContains(t, err, "permission denied")
	reason, _ := errx.ReasonOf(err)
	require.Equal(t, reasons.StateOperationFailed, reason)
	hints, _ := errx.Hints(err)
	require.NotEmpty(t, hints)
}

// An error that already has a reason keeps it.
func TestFailReportOnRecordError_KeepsAnExistingReason(t *testing.T) {
	report := automa.StepSuccessReport("install")
	err := errx.Decorate(errors.New("not synced"), reasons.PreconditionNotMet, "check the disk")

	FailReportOnRecordError(report, err)

	reason, _ := errx.ReasonOf(steps.FailureOf(report))
	require.Equal(t, reasons.PreconditionNotMet, reason)
}
