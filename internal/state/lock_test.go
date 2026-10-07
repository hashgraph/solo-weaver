// SPDX-License-Identifier: Apache-2.0

package state

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAcquireComponentLocks_DisjointComponentsNeverContend(t *testing.T) {
	dir := t.TempDir()

	releaseA, err := AcquireComponentLocks(dir, []ComponentID{ComponentConsensus}, 0)
	require.NoError(t, err)
	defer releaseA()

	// A second caller locking a different component must succeed immediately,
	// even while the first lock is still held — this is the daemon-writes-
	// consensus / CLI-writes-teleport concurrency case the #1231 split exists for.
	releaseB, err := AcquireComponentLocks(dir, []ComponentID{ComponentTeleport}, 0)
	require.NoError(t, err)
	defer releaseB()
}

func TestAcquireComponentLocks_SameComponentFailsFastWithZeroWait(t *testing.T) {
	dir := t.TempDir()

	release, err := AcquireComponentLocks(dir, []ComponentID{ComponentBlockNode}, 0)
	require.NoError(t, err)
	defer release()

	_, err = AcquireComponentLocks(dir, []ComponentID{ComponentBlockNode}, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "blocknode")
	require.Contains(t, err.Error(), "is being modified")
}

func TestAcquireComponentLocks_WaitSucceedsOnceTheHolderReleases(t *testing.T) {
	dir := t.TempDir()

	release, err := AcquireComponentLocks(dir, []ComponentID{ComponentCluster}, 0)
	require.NoError(t, err)

	go func() {
		time.Sleep(50 * time.Millisecond)
		release()
	}()

	start := time.Now()
	release2, err := AcquireComponentLocks(dir, []ComponentID{ComponentCluster}, time.Second)
	require.NoError(t, err)
	defer release2()
	require.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond,
		"the waiter must not have succeeded before the holder released")
}

// TestAcquireComponentLocks_ReleaseIsIdempotentAndPartialFailureUnwinds verifies
// that a failed multi-component acquisition releases every lock it already
// took, so a caller is never left holding a partial set after an error.
func TestAcquireComponentLocks_PartialFailureReleasesWhatWasAcquired(t *testing.T) {
	dir := t.TempDir()

	// Hold "teleport" (sorted after "blocknode") so a multi-id acquisition that
	// starts with blocknode succeeds on it, then fails on teleport.
	holdTeleport, err := AcquireComponentLocks(dir, []ComponentID{ComponentTeleport}, 0)
	require.NoError(t, err)
	defer holdTeleport()

	_, err = AcquireComponentLocks(dir, []ComponentID{ComponentTeleport, ComponentBlockNode}, 0)
	require.Error(t, err)

	// blocknode must have been released by the failed call's unwind, so it is
	// immediately lockable again.
	releaseBlockNode, err := AcquireComponentLocks(dir, []ComponentID{ComponentBlockNode}, 0)
	require.NoError(t, err)
	releaseBlockNode()
}

// TestAcquireComponentLocks_CanonicalOrderingAvoidsDeadlock runs two commands
// that each own the same two components in opposite caller-supplied order,
// concurrently and repeatedly. If AcquireComponentLocks did not sort ids
// before acquiring, this reliably deadlocks; with sorting, both sides always
// make progress.
func TestAcquireComponentLocks_CanonicalOrderingAvoidsDeadlock(t *testing.T) {
	dir := t.TempDir()
	const rounds = 50

	var wg sync.WaitGroup
	wg.Add(2)

	run := func(ids []ComponentID) {
		defer wg.Done()
		for range rounds {
			release, err := AcquireComponentLocks(dir, ids, time.Second)
			require.NoError(t, err)
			release()
		}
	}

	go run([]ComponentID{ComponentCluster, ComponentConsensus})
	go run([]ComponentID{ComponentConsensus, ComponentCluster})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlocked: both commands should make progress under canonical lock ordering")
	}
}
