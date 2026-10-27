// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// firewallEntry is the entry a firewall verb hands RunOwning.
var firewallEntry = ActionHistory{Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetFirewall}}

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
	err = RunOwning(sm, firewallEntry, []ComponentID{ComponentFirewall},
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

	require.NoError(t, RunOwning(sm, firewallEntry, []ComponentID{ComponentFirewall},
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

// TestRunOwning_FailedRunRecordsAFailedEntry: when run fails, the host may be
// half changed, so the action is recorded as failed, but no state is written as
// if it had succeeded.
func TestRunOwning_FailedRunRecordsAFailedEntry(t *testing.T) {
	sm, dir := newOwnedTestManager(t)

	err := RunOwning(sm, firewallEntry, []ComponentID{ComponentFirewall},
		func() error { return errors.New("nft failed") },
		func(st *State) error { st.MachineState.Firewall = &HostFirewallState{}; return nil })

	require.ErrorContains(t, err, "nft failed")
	_, statErr := os.Stat(componentFilePath(dir, ComponentFirewall))
	require.True(t, os.IsNotExist(statErr))

	entries := decodeHistoryFile(t, ActionHistoryFilePath(dir))
	require.Len(t, entries, 1)
	require.Equal(t, firewallEntry.Intent, entries[0].Intent)
	require.Equal(t, OutcomeFailed, entries[0].Outcome)
	require.Equal(t, "nft failed", entries[0].Error)
	require.False(t, entries[0].NotPersisted, "nothing was meant to land after a failed run")
	require.Empty(t, entries[0].Components)
}

// TestRunOwning_RecordsEntryOnEveryExit: a run that went ahead is recorded with
// the files it changed, a patch that fails after run is recorded as not
// persisted, and a command stopped before run records nothing.
func TestRunOwning_RecordsEntryOnEveryExit(t *testing.T) {
	sm, dir := newOwnedTestManager(t)
	disable := func(st *State) error { st.MachineState.Firewall = &HostFirewallState{Disabled: true}; return nil }

	require.NoError(t, RunOwning(sm, firewallEntry, []ComponentID{ComponentFirewall}, func() error { return nil }, disable))
	// The same decision again changes no file.
	require.NoError(t, RunOwning(sm, firewallEntry, []ComponentID{ComponentFirewall}, func() error { return nil }, disable))
	err := RunOwning(sm, firewallEntry, []ComponentID{ComponentFirewall},
		func() error { return nil },
		func(*State) error { return errors.New("cannot patch") })
	require.ErrorContains(t, err, "cannot patch")

	release, err := AcquireComponentLocks(dir, []ComponentID{ComponentFirewall}, 0)
	require.NoError(t, err)
	require.Error(t, RunOwning(sm, firewallEntry, []ComponentID{ComponentFirewall}, func() error { return nil }, disable))
	release()

	entries := decodeHistoryFile(t, ActionHistoryFilePath(dir))
	require.Len(t, entries, 3, "the locked run records nothing")
	require.Equal(t, OutcomeSucceeded, entries[0].Outcome)
	require.Equal(t, []ChangedFile{ChangedFile(ComponentFirewall)}, entries[0].Components)
	require.Equal(t, OutcomeSucceeded, entries[1].Outcome)
	require.Empty(t, entries[1].Components, "an unchanged decision changes no file")
	require.Equal(t, OutcomeSucceeded, entries[2].Outcome, "run succeeded; only its record did not land")
	require.True(t, entries[2].NotPersisted)
	require.Equal(t, "cannot patch", entries[2].Error)
}

// TestRunOwning_UnreadableStateStopsBeforeRun: state is read before run changes
// the host, so a state file that cannot be parsed stops the command with
// nothing changed, and the error names that file.
func TestRunOwning_UnreadableStateStopsBeforeRun(t *testing.T) {
	sm, dir := newOwnedTestManager(t)
	broken := componentFilePath(dir, ComponentBlockNode)
	require.NoError(t, os.WriteFile(broken, []byte("state: [not, a, mapping"), 0o644))

	ran := false
	err := RunOwning(sm, firewallEntry, []ComponentID{ComponentFirewall},
		func() error { ran = true; return nil },
		func(st *State) error { st.MachineState.Firewall = &HostFirewallState{}; return nil })

	require.ErrorContains(t, err, broken)
	require.False(t, ran, "run must not change the host when state cannot be read")
	_, statErr := os.Stat(componentFilePath(dir, ComponentFirewall))
	require.True(t, os.IsNotExist(statErr))
}

// actionFailure is an action's own failure, with the reason and hint the
// operator must still see when its entry is lost too.
func actionFailure() error {
	return errx.Decorate(errorx.ExternalError.New("nft -f failed"), reasons.StateOperationFailed, "free some disk space")
}

// requireActionFailure checks that err is actionFailure's: once wrapped as the
// root command wraps it, its reason and hint still reach the operator, and no
// printable reason leaks into the message.
func requireActionFailure(t *testing.T, err error) {
	t.Helper()
	wrapped := errorx.IllegalState.Wrap(err, "failed to execute command")
	reason, ok := errx.ReasonOf(wrapped)
	require.True(t, ok, "the action's reason is lost: %v", err)
	require.Equal(t, reasons.StateOperationFailed, reason)
	hints, ok := errx.Hints(wrapped)
	require.True(t, ok)
	require.Contains(t, hints, "free some disk space")
	require.NotContains(t, doctor.OperatorMessage(wrapped), "{reason:")
	require.NotContains(t, err.Error(), "action history")
}

// requireLostEntryWarning checks that the only warning queued is the one for
// an entry that could not be appended to dir's history.
func requireLostEntryWarning(t *testing.T, dir string) {
	t.Helper()
	warnings := TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "action history entry was not appended")
	require.Contains(t, warnings[0], "Check that "+ActionHistoryFilePath(dir)+" is writable.")
}

// TestRunOwning_LostEntryOfAFailedActionIsAWarning: when run or patch fails and
// the entry can't be appended either, the command returns the action's own
// error, reason and hints intact, and the lost entry is a state warning.
func TestRunOwning_LostEntryOfAFailedActionIsAWarning(t *testing.T) {
	disable := func(st *State) error { st.MachineState.Firewall = &HostFirewallState{Disabled: true}; return nil }
	for name, step := range map[string]struct {
		run   func() error
		patch func(*State) error
	}{
		"run fails":   {run: actionFailure, patch: disable},
		"patch fails": {run: func() error { return nil }, patch: func(*State) error { return actionFailure() }},
	} {
		t.Run(name, func(t *testing.T) {
			emptyWarnings(t)
			sm, dir := newOwnedTestManager(t)
			require.NoError(t, os.Mkdir(ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails

			err := RunOwning(sm, firewallEntry, []ComponentID{ComponentFirewall}, step.run, step.patch)

			requireActionFailure(t, err)
			requireLostEntryWarning(t, dir)
		})
	}
}

// requireWarning checks that the only warning queued contains every part.
func requireWarning(t *testing.T, parts ...string) {
	t.Helper()
	warnings := TakeWarnings()
	require.Len(t, warnings, 1, "%v", warnings)
	for _, p := range parts {
		require.Contains(t, warnings[0], p)
	}
}
