// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/stretchr/testify/require"
)

type funcChecker[T any] func() (T, error)

func (f funcChecker[T]) RefreshState(context.Context) (T, error) { return f() }

// newInstallRuntime is a runtime over a fresh state dir whose live host has
// kubeadm installed, or whose machine read fails with machineErr.
func newInstallRuntime(t *testing.T, machineErr error) (*rsl.RuntimeResolver, string) {
	t.Helper()
	dir := t.TempDir()
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(principal.NewMockManager(gomock.NewController(t))))
	require.NoError(t, err)
	sm, err := state.NewStateManager(state.WithState(state.NewState(filepath.Join(dir, state.StateFileName))), state.WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	checkers := reality.Checkers{
		Cluster: funcChecker[state.ClusterState](func() (state.ClusterState, error) { return state.ClusterState{Created: true}, nil }),
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

func installEntry(outcome state.Outcome) state.ActionHistory {
	return state.ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetCluster}, Outcome: outcome}
}

// readHistory decodes every entry of dir's action history.
func readHistory(t *testing.T, dir string) []state.ActionHistory {
	t.Helper()
	entries, err := state.ReadActionHistory(dir)
	require.NoError(t, err)
	return entries
}

var systemOnly = []state.ComponentID{state.ComponentSystem}

// TestRecordClusterInstall_FailedInstallRecordsAFailedEntry: a failed install
// returns its own error, writes no system record, and records that it failed.
func TestRecordClusterInstall_FailedInstallRecordsAFailedEntry(t *testing.T) {
	rt, dir := newInstallRuntime(t, nil)
	failed := errors.New("kubeadm init failed")

	err := recordClusterInstall(context.Background(), rt, installEntry(state.OutcomeFailed), failed, systemOnly)

	require.Same(t, failed, err)
	require.NoFileExists(t, state.ComponentFilePath(dir, state.ComponentSystem))
	entries := readHistory(t, dir)
	require.Len(t, entries, 1)
	require.Equal(t, state.OutcomeFailed, entries[0].Outcome)
	require.False(t, entries[0].NotPersisted, "it meant to write nothing")
	require.Equal(t, "kubeadm init failed", entries[0].Error)
}

// TestRecordClusterInstall_FailedRefreshIsNotPersisted: an install whose read of
// the host afterwards fails records its entry marked NotPersisted.
func TestRecordClusterInstall_FailedRefreshIsNotPersisted(t *testing.T) {
	rt, dir := newInstallRuntime(t, errors.New("cannot read the host"))

	err := recordClusterInstall(context.Background(), rt, installEntry(state.OutcomeSucceeded), nil, systemOnly)

	require.ErrorContains(t, err, "failed to refresh state after cluster install")
	entries := readHistory(t, dir)
	require.Len(t, entries, 1)
	require.True(t, entries[0].NotPersisted)
	require.Contains(t, entries[0].Error, "failed to refresh state after cluster install")
}

// TestRecordClusterInstall_LandedRecordWithALostEntryIsAWarning: the install
// ran and its system record landed, so a lost entry is a warning, not the
// command's error.
func TestRecordClusterInstall_LandedRecordWithALostEntryIsAWarning(t *testing.T) {
	state.TakeWarnings()
	t.Cleanup(func() { state.TakeWarnings() })
	rt, dir := newInstallRuntime(t, nil)
	require.NoError(t, os.Mkdir(state.ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails

	require.NoError(t, recordClusterInstall(context.Background(), rt, installEntry(state.OutcomeSucceeded), nil, systemOnly))

	require.FileExists(t, state.ComponentFilePath(dir, state.ComponentSystem))
	warnings := state.TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "Check that "+state.ActionHistoryFilePath(dir)+" is writable.")
}

// TestRecordClusterInstall_SuccessRecordsOneEntry: a successful install writes
// the system record and one entry naming it.
func TestRecordClusterInstall_SuccessRecordsOneEntry(t *testing.T) {
	rt, dir := newInstallRuntime(t, nil)

	require.NoError(t, recordClusterInstall(context.Background(), rt, installEntry(state.OutcomeSucceeded), nil, systemOnly))

	entries := readHistory(t, dir)
	require.Len(t, entries, 1)
	require.Equal(t, state.OutcomeSucceeded, entries[0].Outcome)
	require.Equal(t, []state.ChangedFile{state.ChangedFile(state.ComponentSystem)}, entries[0].Components)
}
