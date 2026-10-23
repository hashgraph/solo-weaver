// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// TestLockAllState_FailsFastOnEveryStateLock: the teardown deletes every state
// file, so any one of the locks guarding them being held stops it before it
// starts, and it releases whatever it had already taken.
func TestLockAllState_FailsFastOnEveryStateLock(t *testing.T) {
	holders := map[string]func(dir string) (func(), error){
		"provisioner":        func(dir string) (func(), error) { return state.AcquireProvisionerLock(dir, 0) },
		"startup migrations": func(dir string) (func(), error) { return state.AcquireStartupMigrationsLock(dir, 0) },
	}
	for _, id := range state.AllComponentIDs {
		holders[string(id)] = func(dir string) (func(), error) {
			return state.AcquireComponentLocks(dir, []state.ComponentID{id}, 0)
		}
	}

	for name, hold := range holders {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			release, err := hold(dir)
			require.NoError(t, err)

			_, err = lockAllState(dir)
			require.Error(t, err, "the teardown must not start while %s is locked", name)
			release()

			all, err := lockAllState(dir)
			require.NoError(t, err, "a failed attempt must release the locks it took")
			all()
		})
	}
}

// TestUninstallRecordError_LostEntryAfterAnUninstall: a successful uninstall
// whose entry was not appended says the cluster is gone and the entry is not,
// with a hint to check the history file rather than to re-run.
func TestUninstallRecordError_LostEntryAfterAnUninstall(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(state.ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails
	lost := state.RecordAround(dir, uninstallEntry(), func() (state.Outcome, error) { return state.OutcomeSucceeded, nil })
	require.True(t, errorx.IsOfType(lost, state.EntryNotAppendedError), "got %v", lost)

	err := uninstallRecordError(dir, lost)

	require.ErrorContains(t, err, "the cluster was uninstalled, but its action history entry was not appended")
	requireStateReason(t, err, "Check that "+state.ActionHistoryFilePath(dir)+" is writable.")
}

// TestUninstallRecordError_UnsyncedEntryAfterAnUninstall: a successful
// uninstall whose entry was appended but not synced says so, with a hint to
// check the disk.
func TestUninstallRecordError_UnsyncedEntryAfterAnUninstall(t *testing.T) {
	dir := t.TempDir()
	unsynced := errorx.Decorate(fsx.FileSyncError.New("disk gone"), "the action ran and its action history entry was appended, but not synced to disk")

	err := uninstallRecordError(dir, unsynced)

	require.True(t, errorx.IsOfType(err, fsx.FileSyncError))
	require.ErrorContains(t, err, "the cluster was uninstalled and its action history entry appended, but not synced to disk")
	requireStateReason(t, err, "Check the disk holding "+dir+".")
}

// TestUninstallRecordError_AFailedUninstallKeepsItsError: any other error is the
// uninstall's own, returned as is.
func TestUninstallRecordError_AFailedUninstallKeepsItsError(t *testing.T) {
	failed := errors.New("kubeadm reset failed")

	require.Same(t, failed, uninstallRecordError(t.TempDir(), failed))
}

func uninstallEntry() state.ActionHistory {
	return state.ActionHistory{Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetCluster}}
}

func requireStateReason(t *testing.T, err error, hint string) {
	t.Helper()
	reason, ok := errx.ReasonOf(err)
	require.True(t, ok)
	require.Equal(t, reasons.StateOperationFailed, reason)
	hints, ok := errx.Hints(err)
	require.True(t, ok)
	require.Contains(t, hints, hint)
	for _, h := range hints {
		require.NotContains(t, strings.ToLower(h), "re-run", "the uninstall already happened")
	}
}
