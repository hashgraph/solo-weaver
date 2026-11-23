// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"testing"

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
