// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package rsl

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
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
	require.NoError(t, writer.FlushState())

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
		require.NoError(t, sm.Set(s).FlushScoped(state.ComponentBlockNode, state.ComponentTeleport))
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

// TestFlushScoped_PreservesUnderlyingErrorCause verifies FlushScoped wraps
// (rather than stringifies via %v) the state manager's error, so the
// underlying errorx cause — here, the per-file hash-conflict error — survives
// in the error chain for diagnostics instead of being flattened to text.
func TestFlushScoped_PreservesUnderlyingErrorCause(t *testing.T) {
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
	require.NoError(t, other.Set(otherState).FlushScoped(state.ComponentTeleport))

	staleState := sm.State()
	staleState.TeleportState.NodeAgent.Configured = false
	err = r.FlushScoped(staleState, state.ComponentTeleport)
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
