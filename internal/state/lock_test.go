// SPDX-License-Identifier: Apache-2.0

package state

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

func TestAcquireComponentLocks_DisjointComponentsNeverContend(t *testing.T) {
	dir := t.TempDir()

	releaseA, err := AcquireComponentLocks(dir, []ComponentID{ComponentConsensus}, 0)
	require.NoError(t, err)
	defer releaseA()

	// A second caller locking a different component must succeed immediately,
	// even while the first lock is still held — this is the daemon-writes-
	// consensus / CLI-writes-teleport concurrency case the per-component split exists for.
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

// TestAcquireComponentLocks_DuplicateIDsDoNotSelfConflict verifies a caller
// that accidentally lists the same component twice still succeeds. Without
// deduping, the second attempt to flock an already-open file would see its
// own first lock (a different open file description, even in the same
// process) as held and fail or hang on itself.
func TestAcquireComponentLocks_DuplicateIDsDoNotSelfConflict(t *testing.T) {
	dir := t.TempDir()

	release, err := AcquireComponentLocks(dir, []ComponentID{ComponentBlockNode, ComponentBlockNode}, 0)
	require.NoError(t, err)
	release()
}

// TestLockErrorsAreDecorated pins the reason and hint each lock failure carries
// to the operator, and that decoration kept the errorx type.
func TestLockErrorsAreDecorated(t *testing.T) {
	dir := t.TempDir()
	pid := strconv.Itoa(os.Getpid())

	holdBlockNode, err := AcquireComponentLocks(dir, []ComponentID{ComponentBlockNode}, 0)
	require.NoError(t, err)
	defer holdBlockNode()
	_, heldErr := AcquireComponentLocks(dir, []ComponentID{ComponentBlockNode}, 0)

	// A holder whose pid was never recorded.
	holdTeleport, err := AcquireComponentLocks(dir, []ComponentID{ComponentTeleport}, 0)
	require.NoError(t, err)
	defer holdTeleport()
	require.NoError(t, os.Truncate(lockFilePath(dir, ComponentTeleport), 0))
	_, unknownHolderErr := AcquireComponentLocks(dir, []ComponentID{ComponentTeleport}, 0)

	holdStartup, err := AcquireStartupMigrationsLock(dir, 0)
	require.NoError(t, err)
	defer holdStartup()
	_, startupErr := AcquireStartupMigrationsLock(dir, 0)
	require.NotContains(t, startupErr.Error(), "after waiting", "a zero wait has nothing to report")

	// Root ignores file modes, so the permission case is built directly.
	lockPath := filepath.Join(dir, ".blocknode.lock")
	deniedErr := lockOpenError("blocknode", lockPath,
		&fs.PathError{Op: "open", Path: lockPath, Err: syscall.EACCES})

	tests := []struct {
		name       string
		err        error
		wantType   *errorx.Type
		wantReason errx.Reason
		wantMsg    string
		hintPart   string
	}{
		{"component lock held", heldErr, errorx.IllegalState, reasons.PreconditionNotMet,
			"blocknode is being modified (held by pid " + pid + ")",
			"Wait for pid " + pid + " to finish (check with: ps -p " + pid + ")"},
		{"component lock held, holder unknown", unknownHolderErr, errorx.IllegalState, reasons.PreconditionNotMet,
			"teleport is being modified (held by another process)",
			"Wait for the other solo-provisioner command to finish"},
		{"startup migrations lock held", startupErr, errorx.IllegalState, reasons.PreconditionNotMet,
			"the state directory is still being migrated by another command (held by pid " + pid + ")",
			"Wait for pid " + pid + " to finish its startup migrations"},
		{"lock file permission denied", deniedErr, errorx.ExternalError, reasons.PermissionDenied,
			"failed to acquire lock for blocknode",
			"Check the permissions on " + lockPath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.err)
			require.True(t, errorx.IsOfType(tt.err, tt.wantType), "errorx type changed: %v", tt.err)
			require.Contains(t, tt.err.Error(), tt.wantMsg)

			reason, ok := errx.ReasonOf(tt.err)
			require.True(t, ok, "no reason attached")
			require.Equal(t, tt.wantReason, reason)

			hints, ok := errx.Hints(tt.err)
			require.True(t, ok, "no hints attached")
			require.Len(t, hints, 1)
			require.Contains(t, hints[0], tt.hintPart)
		})
	}
}

// TestAcquireComponentLocks_OtherOpenFailuresAreNotDecorated: a lock file that
// cannot be created for a reason other than permissions has no operator fix,
// so it keeps the plain ExternalError and gets no reason.
func TestAcquireComponentLocks_OtherOpenFailuresAreNotDecorated(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(notADir, nil, 0o600))

	_, err := AcquireComponentLocks(notADir, []ComponentID{ComponentBlockNode}, 0)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, errorx.ExternalError))
	require.Contains(t, err.Error(), "failed to acquire lock for blocknode")
	_, ok := errx.ReasonOf(err)
	require.False(t, ok, "an unexplained open failure must not get a reason")
}
