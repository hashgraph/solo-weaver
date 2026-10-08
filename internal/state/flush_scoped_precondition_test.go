// SPDX-License-Identifier: Apache-2.0

package state

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFlushScoped_FailedComponentLeavesEarlierComponentsUnwritten verifies that
// flushComponents validates every component's optimistic-concurrency
// precondition before writing any of them. Without that, writing one
// component at a time could succeed for an earlier component (e.g. machine)
// and then fail a later one (e.g. blocknode) partway through a single
// FlushScoped call, leaving machine.yaml referencing an action whose effects
// never reached blocknode.yaml or the action history.
func TestFlushScoped_FailedComponentLeavesEarlierComponentsUnwritten(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.yaml")
	fm := newTestFileManager(t)

	sm, err := NewStateManager(WithState(NewState(stateFile)), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())

	// Establish a baseline for both components.
	seed := sm.State()
	seed.MachineState.Profile = "before"
	seed.BlockNodeState.ReleaseInfo.Name = "before"
	require.NoError(t, sm.Set(seed).FlushScoped(ComponentMachine, ComponentBlockNode))
	require.NoError(t, sm.Refresh())

	// Simulate an external writer changing blocknode.yaml without going
	// through this manager, so this manager's baseline for it is now stale.
	external, err := NewStateManager(WithState(NewState(stateFile)), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, external.Refresh())
	extState := external.State()
	extState.BlockNodeState.ReleaseInfo.Name = "changed-externally"
	require.NoError(t, external.Set(extState).FlushScoped(ComponentBlockNode))

	// Now flush machine and blocknode together from the stale manager. machine
	// has no conflict; blocknode does. The whole call must fail, and machine.yaml
	// must be untouched — not updated to the new profile with a mismatched
	// blocknode still holding the external writer's content.
	changed := sm.State()
	changed.MachineState.Profile = "after"
	changed.BlockNodeState.ReleaseInfo.Name = "after"
	err = sm.Set(changed).FlushScoped(ComponentMachine, ComponentBlockNode)
	require.Error(t, err)

	reread, err := NewStateManager(WithState(NewState(stateFile)), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, reread.Refresh())
	require.Equal(t, "before", reread.State().MachineState.Profile,
		"machine.yaml must not have been written when blocknode's precondition failed")
	require.Equal(t, "changed-externally", reread.State().BlockNodeState.ReleaseInfo.Name)
}
