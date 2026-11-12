// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/state"
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

	orig := uninstallStartupLockWait
	t.Cleanup(func() { uninstallStartupLockWait = orig })
	uninstallStartupLockWait = 50 * time.Millisecond

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

// TestLockAllState_WaitsForARunningStartupPass: while a migration is deferred
// every command, and the DNS-refresh timer every few minutes, runs a startup
// pass under the startup lock. The uninstall waits that out instead of failing,
// holding no other lock meanwhile so the pass can still stamp and record.
func TestLockAllState_WaitsForARunningStartupPass(t *testing.T) {
	orig := uninstallStartupLockWait
	t.Cleanup(func() { uninstallStartupLockWait = orig })
	uninstallStartupLockWait = 5 * time.Second
	dir := t.TempDir()
	releasePass, err := state.AcquireStartupMigrationsLock(dir, 0)
	require.NoError(t, err)

	componentFree := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		release, err := state.AcquireComponentLocks(dir, state.AllComponentIDs, 0)
		if err == nil {
			release()
		}
		componentFree <- err
		releasePass()
	}()

	all, err := lockAllState(dir)

	require.NoError(t, err, "the uninstall must wait for the pass to finish")
	all()
	require.NoError(t, <-componentFree, "no component lock may be held while waiting")
}
