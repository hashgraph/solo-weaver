// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/stretchr/testify/require"
)

type funcChecker[T any] func() (T, error)

func (f funcChecker[T]) RefreshState(context.Context) (T, error) { return f() }

// liveCluster is the cluster the install runtime reads: created, with MetalLB
// deployed.
var liveCluster = state.ClusterState{
	Created:  true,
	Releases: map[string]state.HelmReleaseInfo{"metallb": {Name: "metallb", Namespace: "metallb-system", ChartVersion: "0.16.1", Status: "deployed"}},
}

// newInstallRuntime is a runtime over a fresh state dir whose live host has
// kubeadm installed, or whose machine read fails with machineErr.
func newInstallRuntime(t *testing.T, machineErr error) (*rsl.RuntimeResolver, string) {
	return newInstallRuntimeWithCluster(t, machineErr, nil)
}

// newInstallRuntimeWithCluster is newInstallRuntime whose cluster read fails
// with clusterErr.
func newInstallRuntimeWithCluster(t *testing.T, machineErr, clusterErr error) (*rsl.RuntimeResolver, string) {
	t.Helper()
	dir := t.TempDir()
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(principal.NewMockManager(gomock.NewController(t))))
	require.NoError(t, err)
	sm, err := state.NewStateManager(state.WithState(state.NewState(filepath.Join(dir, state.StateFileName))), state.WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	checkers := reality.Checkers{
		Cluster: funcChecker[state.ClusterState](func() (state.ClusterState, error) { return liveCluster, clusterErr }),
		Machine: funcChecker[state.MachineState](func() (state.MachineState, error) {
			return state.MachineState{Software: map[string]state.SoftwareState{"kubeadm": {Name: "kubeadm", Installed: true}}}, machineErr
		}),
		BlockNode: funcChecker[state.BlockNodeState](func() (state.BlockNodeState, error) { return state.BlockNodeState{}, nil }),
		Consensus: funcChecker[map[string]state.ConsensusNodeState](func() (map[string]state.ConsensusNodeState, error) {
			return map[string]state.ConsensusNodeState{}, nil
		}),
		Teleport: funcChecker[state.TeleportState](func() (state.TeleportState, error) { return state.TeleportState{}, nil }),
	}
	rt, err := rsl.NewRuntimeResolver(models.Config{}, sm, checkers, time.Minute)
	require.NoError(t, err)
	return rt, dir
}

// readHistory decodes every entry of dir's action history.
func readHistory(t *testing.T, dir string) []state.ActionHistory {
	t.Helper()
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	return entries
}

var owned = []state.ComponentID{state.ComponentSystem, state.ComponentCluster}

var clusterInstallIntent = models.Intent{Action: models.ActionInstall, Target: models.TargetCluster}

// installWorkflow is a one-step install in mode, ending in the RecordState step
// the command appends. rolledBack, when set, is told whether the step was
// rolled back.
func installWorkflow(rt clusterRecorder, fails bool, mode automa.TypeMode, rolledBack *bool) *automa.WorkflowBuilder {
	step := automa.NewStepBuilder().WithId("kubeadm-init").
		WithExecute(func(context.Context, automa.Step) *automa.Report {
			if fails {
				return automa.StepFailureReport("kubeadm-init", automa.WithError(errors.New("kubeadm init failed")))
			}
			return automa.StepSuccessReport("kubeadm-init")
		}).
		WithRollback(func(context.Context, automa.Step) *automa.Report {
			if rolledBack != nil {
				*rolledBack = true
			}
			return automa.StepSuccessReport("kubeadm-init", automa.WithActionType(automa.ActionRollback))
		})
	return automa.NewWorkflowBuilder().WithId("install").WithExecutionMode(mode).
		Steps(step, steps.RecordState(recordClusterInstall(rt, owned)))
}

// runInstall runs installRun over rt and returns its report and the one entry
// it appended.
func runInstall(t *testing.T, rt clusterRecorder, dir string, wb *automa.WorkflowBuilder) (*automa.Report, state.ActionHistory) {
	t.Helper()
	report, err := installRun(context.Background(), wb, rt, clusterInstallIntent, nil)()
	require.NoError(t, err)
	entries := readHistory(t, dir)
	require.Len(t, entries, 1)
	return report, entries[0]
}

// A successful install writes the system and cluster records, the cluster one
// with its chart releases, and one entry naming both.
func TestInstallRun_SuccessRecordsTheHostAndCluster(t *testing.T) {
	rt, dir := newInstallRuntime(t, nil)

	report, entry := runInstall(t, rt, dir, installWorkflow(rt, false, automa.StopOnError, nil))

	require.True(t, report.IsSuccess())
	require.Equal(t, state.OutcomeSucceeded, entry.Outcome)
	require.ElementsMatch(t, []state.ChangedFile{state.ComponentSystem.File(), state.ComponentCluster.File()}, entry.Components)
	require.Equal(t, liveCluster.Releases, readCluster(t, dir).Releases)
}

// A failed install stops before the RecordState step: no record, and an entry
// with the install's own failure.
func TestInstallRun_FailedInstallRecordsAFailedEntry(t *testing.T) {
	rt, dir := newInstallRuntime(t, nil)

	report, entry := runInstall(t, rt, dir, installWorkflow(rt, true, automa.StopOnError, nil))

	require.ErrorContains(t, steps.FailureOf(report), "kubeadm init failed")
	require.NoFileExists(t, state.ComponentFilePath(dir, state.ComponentSystem))
	require.Equal(t, state.OutcomeFailed, entry.Outcome)
	require.False(t, entry.NotPersisted, "it meant to write nothing")
	require.Equal(t, "kubeadm init failed", entry.Error)
}

// A host that can't be read after the install leaves the install successful,
// with a warning and an entry marked NotPersisted.
func TestInstallRun_FailedRefreshIsAWarning(t *testing.T) {
	rt, dir := newInstallRuntime(t, errors.New("cannot read the host"))

	report, entry := runInstall(t, rt, dir, installWorkflow(rt, false, automa.StopOnError, nil))

	require.True(t, report.IsSuccess())
	require.Contains(t, strings.Join(ui.CollectWarnings(report), "\n"), "failed to refresh state after cluster install")
	require.NoFileExists(t, state.ComponentFilePath(dir, state.ComponentSystem))
	require.True(t, entry.NotPersisted)
	require.Contains(t, entry.Error, "failed to refresh state after cluster install")
}

// A cluster that can't be read right after the install keeps its recorded
// state; the system record still lands.
func TestInstallRun_UnreadableClusterStillRecordsTheHost(t *testing.T) {
	rt, dir := newInstallRuntimeWithCluster(t, nil, errors.New("connection refused"))

	report, _ := runInstall(t, rt, dir, installWorkflow(rt, false, automa.StopOnError, nil))

	require.True(t, report.IsSuccess())
	require.FileExists(t, state.ComponentFilePath(dir, state.ComponentSystem))
	require.Empty(t, readCluster(t, dir).Releases)
}

// failingPersist is the install runtime with writes that fail.
type failingPersist struct{ *rsl.RuntimeResolver }

func (failingPersist) Persist(state.State, ...state.ComponentID) ([]state.ChangedFile, error) {
	return nil, errors.New("disk full")
}

// Records that can't be written leave the install successful, with a warning
// and an entry marked NotPersisted, and roll nothing back under
// --rollback-on-error.
func TestInstallRun_RecordFailureIsAWarningAndRollsNothingBack(t *testing.T) {
	rt, dir := newInstallRuntime(t, nil)
	rec := failingPersist{rt}
	rolledBack := false

	report, entry := runInstall(t, rec, dir, installWorkflow(rec, false, automa.RollbackOnError, &rolledBack))

	require.True(t, report.IsSuccess())
	require.False(t, rolledBack)
	require.Contains(t, strings.Join(ui.CollectWarnings(report), "\n"), "The state was not recorded: disk full")
	require.True(t, entry.NotPersisted)
	require.Equal(t, reasons.StateOperationFailed, entry.Reason)
}

// The install ran and its records landed, so an entry that can't be appended
// is a warning, not the command's error.
func TestInstallRun_LostEntryIsAWarning(t *testing.T) {
	rt, dir := newInstallRuntime(t, nil)
	require.NoError(t, os.Mkdir(state.ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails

	report, err := installRun(context.Background(), installWorkflow(rt, false, automa.StopOnError, nil), rt, clusterInstallIntent, nil)()

	require.NoError(t, err)
	require.True(t, report.IsSuccess())
	require.FileExists(t, state.ComponentFilePath(dir, state.ComponentSystem))
	require.Contains(t, strings.Join(ui.CollectWarnings(report), "\n"), "action history entry was not appended")
}

// readCluster loads dir's persisted cluster record.
func readCluster(t *testing.T, dir string) state.ClusterState {
	t.Helper()
	sm, err := state.NewStateManager(state.WithStateFile(filepath.Join(dir, state.StateFileName)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	return sm.State().ClusterState
}
