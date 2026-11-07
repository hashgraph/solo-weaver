// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
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

// A cluster that cannot be read after a successful run is not recorded, and
// the entry says so.
func TestRunRecordingCluster_UnreadableClusterIsNotPersisted(t *testing.T) {
	useCluster(t, nil, errors.New("connection refused"))
	sm, dir := newRecordingManager(t)

	_, err := runRecordingCluster(context.Background(), sm, alloyInstall, oneStepWorkflow(false))

	require.ErrorContains(t, err, "failed to read the cluster after the run")
	require.NoFileExists(t, state.ComponentFilePath(dir, state.ComponentCluster))
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.True(t, entries[0].NotPersisted)
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
