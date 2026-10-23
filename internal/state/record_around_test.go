// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

var uninstallCluster = ActionHistory{Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetCluster}}

// seedStateDir writes a block node file, provisioner.yaml and a legacy
// state.yaml into a temp state dir.
func seedStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sm, err := NewStateManager(WithState(newTestState(filepath.Join(dir, StateFileName))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.Commit(ActionHistory{}, ComponentBlockNode))
	require.NoError(t, os.Remove(ActionHistoryFilePath(dir)))
	require.NoError(t, writeProvisionerRecord(dir, ProvisionerRecord{CLI: ProvisionerInfo{Version: "0.15.0"}}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, StateFileName), []byte(legacySSHPortYAML), 0o644))
	return dir
}

// removeState removes every file in dir but the action history, as the
// uninstall cleanup does.
func removeState(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if e.Name() != ActionHistoryFileName {
			require.NoError(t, os.RemoveAll(filepath.Join(dir, e.Name())))
		}
	}
}

// TestRecordAround_RecordsWhatTheActionRemoved: an action that removes state
// files is recorded with every file it removed, after they are gone.
func TestRecordAround_RecordsWhatTheActionRemoved(t *testing.T) {
	dir := seedStateDir(t)

	require.NoError(t, RecordAround(dir, uninstallCluster, func() (Outcome, error) {
		removeState(t, dir)
		return OutcomeSucceeded, nil
	}))

	entries := decodeHistoryFile(t, ActionHistoryFilePath(dir))
	require.Len(t, entries, 1)
	require.Equal(t, uninstallCluster.Intent, entries[0].Intent)
	require.Equal(t, OutcomeSucceeded, entries[0].Outcome)
	require.Equal(t, []ChangedFile{ComponentBlockNode.File(), ChangedProvisioner, ChangedLegacy}, entries[0].Components)
}

// TestRecordAround_RecordsAFailedAction: an action that fails is recorded as
// failed with its error, and the error is returned.
func TestRecordAround_RecordsAFailedAction(t *testing.T) {
	dir := seedStateDir(t)

	err := RecordAround(dir, uninstallCluster, func() (Outcome, error) {
		return OutcomeSucceeded, errors.New("kubeadm reset failed")
	})

	require.EqualError(t, err, "kubeadm reset failed")
	entries := decodeHistoryFile(t, ActionHistoryFilePath(dir))
	require.Len(t, entries, 1)
	require.Equal(t, OutcomeFailed, entries[0].Outcome, "an error with a succeeded outcome records failed")
	require.Equal(t, "kubeadm reset failed", entries[0].Error)
	require.Empty(t, entries[0].Components)
}

// TestRecordAround_LostEntryAfterASuccessIsEntryNotAppended: when the action
// succeeded but its entry cannot be appended, the error says exactly that.
func TestRecordAround_LostEntryAfterASuccessIsEntryNotAppended(t *testing.T) {
	dir := seedStateDir(t)
	require.NoError(t, os.Mkdir(ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails

	err := RecordAround(dir, uninstallCluster, func() (Outcome, error) { return OutcomeSucceeded, nil })

	require.True(t, errorx.IsOfType(err, EntryNotAppendedError), "got %v", err)
	require.ErrorContains(t, err, "failed to append action history entries")
}

// TestRecordAround_LostEntryOfAFailedActionIsAWarning: when fn fails and the
// entry can't be appended either, RecordAround returns fn's own error, reason
// and hints intact, and the lost entry is a state warning.
func TestRecordAround_LostEntryOfAFailedActionIsAWarning(t *testing.T) {
	emptyWarnings(t)
	dir := seedStateDir(t)
	require.NoError(t, os.Mkdir(ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails

	err := RecordAround(dir, uninstallCluster, func() (Outcome, error) { return OutcomeFailed, actionFailure() })

	requireActionFailure(t, err)
	requireLostEntryWarning(t, dir)
}

// TestRecordAround_UnsyncedEntryIsNotALostEntry: an entry appended but not
// synced is in the history, so RecordAround returns the sync error, not
// EntryNotAppendedError.
func TestRecordAround_UnsyncedEntryIsNotALostEntry(t *testing.T) {
	orig := historyFileManager
	t.Cleanup(func() { historyFileManager = orig })
	historyFileManager = func() (fsx.Manager, error) {
		return &unsyncedAppender{Manager: newTestFileManager(t), err: fsx.FileSyncError.New("disk gone")}, nil
	}
	dir := t.TempDir()
	entry := ActionHistory{Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetCluster}}

	err := RecordAround(dir, entry, func() (Outcome, error) { return OutcomeSucceeded, nil })

	require.True(t, errorx.IsOfType(err, fsx.FileSyncError), "got %v", err)
	require.False(t, errorx.IsOfType(err, EntryNotAppendedError))
	require.ErrorContains(t, err, "appended, but not synced to disk")
	require.Len(t, decodeHistoryFile(t, ActionHistoryFilePath(dir)), 1)
}

// TestRecordAround_UnreadableSnapshotStillRecordsAndWarns: when the state files
// can't be read, the entry is appended naming none of them, and the operator is
// warned.
func TestRecordAround_UnreadableSnapshotStillRecordsAndWarns(t *testing.T) {
	emptyWarnings(t)
	dir := seedStateDir(t)
	// A directory where a legacy file's glob matches cannot be read as a file.
	require.NoError(t, os.Mkdir(filepath.Join(dir, StateFileName+".diverged-x"), 0o755))

	require.NoError(t, RecordAround(dir, uninstallCluster, func() (Outcome, error) { return OutcomeSucceeded, nil }))

	entries := decodeHistoryFile(t, ActionHistoryFilePath(dir))
	require.Len(t, entries, 1)
	require.Empty(t, entries[0].Components)
	warnings := TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "the state files it changed could not be read")
}

// TestRecordAround_ChangedFilesComeFirst: as in Commit, the files the action
// changed come before the ones the caller named.
func TestRecordAround_ChangedFilesComeFirst(t *testing.T) {
	dir := seedStateDir(t)
	entry := uninstallCluster
	entry.Components = []ChangedFile{ChangedProvisioner}

	require.NoError(t, RecordAround(dir, entry, func() (Outcome, error) {
		return OutcomeSucceeded, os.Remove(componentFilePath(dir, ComponentBlockNode))
	}))

	entries := decodeHistoryFile(t, ActionHistoryFilePath(dir))
	require.Len(t, entries, 1)
	require.Equal(t, []ChangedFile{ComponentBlockNode.File(), ChangedProvisioner}, entries[0].Components)
}
