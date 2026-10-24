// SPDX-License-Identifier: Apache-2.0

package state

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFlushScoped_DisjointComponentsBothSucceedConcurrently is the
// acceptance criterion: a daemon writing consensus state concurrently with a
// CLI writing teleport state must both succeed, because they touch disjoint
// files with independent hashes and locks — not the one shared state.yaml
// hash the pre-split design serialized everything through.
func TestFlushScoped_DisjointComponentsBothSucceedConcurrently(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.yaml")
	fm := newTestFileManager(t)

	daemonSM, err := NewStateManager(WithState(NewState(stateFile)), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, daemonSM.Refresh())

	cliSM, err := NewStateManager(WithState(NewState(stateFile)), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, cliSM.Refresh())

	releaseConsensus, err := AcquireComponentLocks(dir, []ComponentID{ComponentConsensus}, 0)
	require.NoError(t, err)
	defer releaseConsensus()

	releaseTeleport, err := AcquireComponentLocks(dir, []ComponentID{ComponentTeleport}, 0)
	require.NoError(t, err)
	defer releaseTeleport()

	daemonState := daemonSM.State()
	daemonState.ConsensusNodes = map[string]ConsensusNodeState{"node1": {NodeId: 1}}
	require.NoError(t, daemonSM.Set(daemonState).FlushScoped(ComponentConsensus),
		"daemon's consensus write must succeed while the CLI holds the teleport lock")

	cliState := cliSM.State()
	cliState.TeleportState.NodeAgent = TeleportNodeAgentState{Installed: true, Configured: true}
	require.NoError(t, cliSM.Set(cliState).FlushScoped(ComponentTeleport),
		"the CLI's teleport write must succeed while the daemon holds the consensus lock")

	final, err := NewStateManager(WithState(NewState(stateFile)), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, final.Refresh())
	require.Contains(t, final.State().ConsensusNodes, "node1")
	require.True(t, final.State().TeleportState.NodeAgent.Configured)
}

// TestFlushScoped_ConsensusComponentPersistsOrbits verifies that the consensus component's
// file carries the orbits next to the nodes, so a consensus write is not lost on re-read.
func TestFlushScoped_ConsensusComponentPersistsOrbits(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	fm := newTestFileManager(t)

	sm, err := NewStateManager(WithState(NewState(stateFile)), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	s := sm.State()
	s.ConsensusOrbits = map[string]ConsensusOrbitState{"orbit1": {LedgerId: "0x01"}}
	require.NoError(t, sm.Set(s).FlushScoped(ComponentConsensus))

	reread, err := NewStateManager(WithState(NewState(stateFile)), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, reread.Refresh())
	require.Equal(t, "0x01", reread.State().ConsensusOrbits["orbit1"].LedgerId)
}
