// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// newOwnedTestManager returns a manager over a temp state directory.
func newOwnedTestManager(t *testing.T) (Manager, string) {
	t.Helper()
	dir := t.TempDir()
	sm, err := NewStateManager(WithState(newTestState(filepath.Join(dir, StateFileName))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	return sm, dir
}

// TestRunOwning_FailsFastWhenLocked: a command that cannot take one of its
// locks stops before run, so it changes nothing on the host or on disk.
func TestRunOwning_FailsFastWhenLocked(t *testing.T) {
	sm, dir := newOwnedTestManager(t)
	release, err := AcquireComponentLocks(dir, []ComponentID{ComponentFirewall}, 0)
	require.NoError(t, err)
	defer release()

	ran := false
	err = RunOwning(sm, []ComponentID{ComponentFirewall},
		func() error { ran = true; return nil },
		func(st *State) error { st.MachineState.Firewall = &HostFirewallState{Disabled: true}; return nil })

	require.ErrorContains(t, err, "firewall is being modified")
	require.False(t, ran, "run must not start while a lock is held")
	_, statErr := os.Stat(componentFilePath(dir, ComponentFirewall))
	require.True(t, os.IsNotExist(statErr), "nothing may be written")
}

// TestRunOwning_WritesOnlyItsComponents: after run, the patch reaches disk for
// the owned components alone, whatever else it touched.
func TestRunOwning_WritesOnlyItsComponents(t *testing.T) {
	sm, dir := newOwnedTestManager(t)

	require.NoError(t, RunOwning(sm, []ComponentID{ComponentFirewall},
		func() error { return nil },
		func(st *State) error {
			st.MachineState.Firewall = &HostFirewallState{Disabled: true}
			st.MachineState.Profile = "not-owned"
			return nil
		}))

	for _, id := range AllComponentIDs {
		_, err := os.Stat(componentFilePath(dir, id))
		require.Equalf(t, id == ComponentFirewall, err == nil, "%s.yaml written", id)
	}
	reread, err := NewStateManager(WithState(newTestState(filepath.Join(dir, StateFileName))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, reread.Refresh())
	require.True(t, reread.State().MachineState.Firewall.Disabled)
}

// TestRunOwning_FailedRunRecordsNothing: when run fails, the host may be half
// changed, but nothing is recorded as if it had succeeded.
func TestRunOwning_FailedRunRecordsNothing(t *testing.T) {
	sm, dir := newOwnedTestManager(t)

	err := RunOwning(sm, []ComponentID{ComponentFirewall},
		func() error { return errors.New("nft failed") },
		func(st *State) error { st.MachineState.Firewall = &HostFirewallState{}; return nil })

	require.ErrorContains(t, err, "nft failed")
	_, statErr := os.Stat(componentFilePath(dir, ComponentFirewall))
	require.True(t, os.IsNotExist(statErr))
}

// TestRunOwning_UnreadableStateStopsBeforeRun: state is read before run changes
// the host, so a state file that cannot be parsed stops the command with
// nothing changed, and the error names that file.
func TestRunOwning_UnreadableStateStopsBeforeRun(t *testing.T) {
	sm, dir := newOwnedTestManager(t)
	broken := componentFilePath(dir, ComponentBlockNode)
	require.NoError(t, os.WriteFile(broken, []byte("state: [not, a, mapping"), 0o644))

	ran := false
	err := RunOwning(sm, []ComponentID{ComponentFirewall},
		func() error { ran = true; return nil },
		func(st *State) error { st.MachineState.Firewall = &HostFirewallState{}; return nil })

	require.ErrorContains(t, err, broken)
	require.False(t, ran, "run must not change the host when state cannot be read")
	_, statErr := os.Stat(componentFilePath(dir, ComponentFirewall))
	require.True(t, os.IsNotExist(statErr))
}
