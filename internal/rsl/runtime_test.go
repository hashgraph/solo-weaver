// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package rsl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/automa-saga/errx"
	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

type funcChecker[T any] func() (T, error)

func (f funcChecker[T]) RefreshState(context.Context) (T, error) { return f() }

// newFileManager skips the host user lookup, which nothing here needs and which
// is slow on macOS.
func newFileManager(t *testing.T) fsx.Manager {
	t.Helper()
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(principal.NewMockManager(gomock.NewController(t))))
	require.NoError(t, err)
	return fm
}

func persistedTeleport() state.TeleportState {
	return state.TeleportState{
		NodeAgent: state.TeleportNodeAgentState{Installed: true, Configured: true},
	}
}

// writePersistedState writes a state file holding persistedTeleport and a
// shaping record, and returns a manager that has loaded it.
func writePersistedState(t *testing.T, stateFile string) state.Manager {
	t.Helper()
	fm := newFileManager(t)
	s := state.NewState(stateFile)
	s.TeleportState = persistedTeleport()
	s.BlockNodeState.Shaping = &state.ShapingState{EgressInterface: "eth0"}
	writer, err := state.NewStateManager(state.WithState(s), state.WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, writer.Commit(state.ActionHistory{}, state.AllComponentIDs...))

	sm, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	return sm
}

// newResolver builds a RuntimeResolver whose checkers report liveTeleport and
// otherwise echo the persisted state. midRefresh runs inside the machine
// checker, which calls sm.Refresh() like the real one does.
func newResolver(t *testing.T, sm state.Manager, liveTeleport state.TeleportState, midRefresh func()) *RuntimeResolver {
	t.Helper()
	checkers := reality.Checkers{
		Cluster: funcChecker[state.ClusterState](func() (state.ClusterState, error) {
			return state.ClusterState{Created: true}, nil
		}),
		Machine: funcChecker[state.MachineState](func() (state.MachineState, error) {
			if midRefresh != nil {
				midRefresh()
			}
			if err := sm.Refresh(); err != nil {
				return state.MachineState{}, err
			}
			return sm.State().MachineState, nil
		}),
		BlockNode: funcChecker[state.BlockNodeState](func() (state.BlockNodeState, error) {
			return sm.State().BlockNodeState, nil
		}),
		Consensus: funcChecker[map[string]state.ConsensusNodeState](func() (map[string]state.ConsensusNodeState, error) {
			return map[string]state.ConsensusNodeState{}, nil
		}),
		Teleport: funcChecker[state.TeleportState](func() (state.TeleportState, error) {
			return liveTeleport, nil
		}),
	}
	r, err := NewRuntimeResolver(models.Config{}, sm, checkers, time.Minute)
	require.NoError(t, err)
	return r
}

func liveTeleport() state.TeleportState {
	return state.TeleportState{
		NodeAgent: state.TeleportNodeAgentState{Installed: true, Configured: false},
	}
}

func TestRefreshWithBaseline_BaselineIsPersistedAndCurrentIsLive(t *testing.T) {
	sm := writePersistedState(t, filepath.Join(t.TempDir(), "state.yaml"))
	r := newResolver(t, sm, liveTeleport(), nil)

	baseline, current, err := r.RefreshWithBaseline(context.Background(), true)

	require.NoError(t, err)
	require.NotNil(t, baseline)
	require.Equal(t, persistedTeleport().NodeAgent, baseline.TeleportState.NodeAgent)
	require.Equal(t, liveTeleport().NodeAgent, current.TeleportState.NodeAgent)
}

func TestRefreshWithBaseline_CurrentMatchesRefresh(t *testing.T) {
	sm := writePersistedState(t, filepath.Join(t.TempDir(), "state.yaml"))
	r := newResolver(t, sm, liveTeleport(), nil)

	_, withBaseline, err := r.RefreshWithBaseline(context.Background(), true)
	require.NoError(t, err)
	plain, err := r.Refresh(context.Background(), true)
	require.NoError(t, err)

	require.Equal(t, plain.Hashable(), withBaseline.Hashable())
}

// A checker's sm.Refresh() decodes onto shallow clones, writing through pointers
// that a plain copy of the persisted state would share.
func TestRefreshWithBaseline_BaselineSurvivesARefreshDuringTheRuntimeRefresh(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	sm := writePersistedState(t, stateFile)
	rewrite := func() {
		s := sm.State()
		s.BlockNodeState.Shaping = &state.ShapingState{EgressInterface: "eth9"}
		s.TeleportState = state.TeleportState{}
		require.NoError(t, sm.Set(s).Commit(state.ActionHistory{}, state.ComponentBlockNode, state.ComponentTeleport))
	}
	r := newResolver(t, sm, liveTeleport(), rewrite)

	baseline, _, err := r.RefreshWithBaseline(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, "eth9", sm.State().BlockNodeState.Shaping.EgressInterface, "precondition: the manager refreshed mid-run")
	require.Equal(t, "eth0", baseline.BlockNodeState.Shaping.EgressInterface)
	require.Equal(t, persistedTeleport().NodeAgent, baseline.TeleportState.NodeAgent)
}

func TestRefreshWithBaseline_NoStateFileHasNoBaseline(t *testing.T) {
	sm, err := state.NewStateManager(
		state.WithState(state.NewState(filepath.Join(t.TempDir(), "state.yaml"))),
		state.WithFileManager(newFileManager(t)))
	require.NoError(t, err)
	r := newResolver(t, sm, liveTeleport(), nil)

	baseline, current, err := r.RefreshWithBaseline(context.Background(), true)

	require.NoError(t, err)
	require.Nil(t, baseline)
	require.Equal(t, liveTeleport().NodeAgent, current.TeleportState.NodeAgent)
}

// TestCommit_PreservesUnderlyingErrorCause verifies Commit wraps
// (rather than stringifies via %v) the state manager's error, so the
// underlying errorx cause — here, the per-file hash-conflict error — survives
// in the error chain for diagnostics instead of being flattened to text.
func TestCommit_PreservesUnderlyingErrorCause(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	sm := writePersistedState(t, stateFile)
	r := newResolver(t, sm, liveTeleport(), nil)
	fm := newFileManager(t)

	// A second, independent manager on the same file writes teleport after sm
	// last refreshed, so sm's baseline is now stale.
	other, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, other.Refresh())
	otherState := other.State()
	otherState.TeleportState.NodeAgent.Configured = false // flips from persistedTeleport()'s true
	require.NoError(t, other.Set(otherState).Commit(state.ActionHistory{}, state.ComponentTeleport))

	staleState := sm.State()
	staleState.TeleportState.NodeAgent.Configured = false
	err = r.Commit(staleState, state.ActionHistory{}, state.ComponentTeleport)
	require.Error(t, err)

	// errorx.Wrap is opaque to errors.Is/As/Unwrap by design (see the errorx
	// doc comment on Type.Wrap) — the cause is reachable only via .Cause() on
	// the errorx.Error, which is exactly what diagnostics walk. New("...: %v",
	// err) would discard the cause object entirely, leaving only its string
	// baked into the outer message with no way back to the original error.
	cause := errorx.Cast(err).Cause()
	require.NotNil(t, cause, "the underlying errorx cause must still be reachable via .Cause(), not flattened to text by %%v")
	require.Contains(t, cause.Error(), "changed externally on disk")
}

// unsyncedAppender is an fsx.Manager whose appends land but report a failed sync.
type unsyncedAppender struct{ fsx.Manager }

func (a unsyncedAppender) AppendToFile(path string, payload []byte) error {
	if err := a.Manager.AppendToFile(path, payload); err != nil {
		return err
	}
	return fsx.FileSyncError.New("disk gone")
}

// requireStateOperationFailed checks err's reason and that one of its hints
// contains hint.
func requireStateOperationFailed(t *testing.T, err error, hint string) {
	t.Helper()
	reason, ok := errx.ReasonOf(err)
	require.True(t, ok, "no reason on %v", err)
	require.Equal(t, reasons.StateOperationFailed, reason)
	hints, ok := errx.Hints(err)
	require.True(t, ok)
	require.Contains(t, strings.Join(hints, "\n"), hint)
}

// TestCommit_LandedStateWithALostEntrySaysSo: state that landed with its entry
// lost keeps EntryNotAppendedError, says so once, and tells the operator what
// to check, rather than "failed to persist state".
func TestCommit_LandedStateWithALostEntrySaysSo(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	sm := writePersistedState(t, stateFile)
	r := newResolver(t, sm, liveTeleport(), nil)
	history := state.ActionHistoryFilePath(filepath.Dir(stateFile))
	require.NoError(t, os.RemoveAll(history))
	require.NoError(t, os.Mkdir(history, 0o755)) // appending to a directory fails

	s := sm.State()
	s.TeleportState.NodeAgent.Configured = false
	err := r.Commit(s, state.ActionHistory{}, state.ComponentTeleport)

	require.True(t, errorx.IsOfType(err, state.EntryNotAppendedError), "got %v", err)
	msg := doctor.OperatorMessage(err)
	require.Equal(t, 1, strings.Count(msg, "the state was written but its action history entry was not appended"), msg)
	require.NotContains(t, msg, "failed to persist state")
	requireStateOperationFailed(t, err, history+" is writable")
}

// TestCommit_UnsyncedStateSaysSo: state whose entry was appended but not synced
// keeps FileSyncError and says it was saved but not synced.
func TestCommit_UnsyncedStateSaysSo(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writePersistedState(t, stateFile)
	sm, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(unsyncedAppender{newFileManager(t)}))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	r := newResolver(t, sm, liveTeleport(), nil)

	s := sm.State()
	s.TeleportState.NodeAgent.Configured = false
	err = r.Commit(s, state.ActionHistory{}, state.ComponentTeleport)

	require.True(t, errorx.IsOfType(err, fsx.FileSyncError), "got %v", err)
	require.Contains(t, doctor.OperatorMessage(err), "the state was saved and its action history entry appended, but not synced to disk")
	requireStateOperationFailed(t, err, "Check the disk holding "+filepath.Dir(stateFile))
}
