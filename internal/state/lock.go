// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joomcode/errorx"
)

// lockPollInterval is how often a waiting AcquireComponentLocks call retries a
// contended lock.
const lockPollInterval = 100 * time.Millisecond

// AcquireComponentLocks takes an exclusive advisory lock on every id's lock
// file, acquired in sorted-by-name order so two commands that each own
// multiple components can never deadlock on each other. The returned release
// func unwinds every lock this call acquired (in reverse order) and is always
// safe to call, including after a failed acquisition.
//
// wait bounds how long to block on a lock already held elsewhere; zero tries
// once and fails immediately rather than waiting.
//
// Locking only binds callers that go through this function: it stops two
// commands racing each other, but does not stop a hand-edited file or any
// write that bypasses it. The per-component hash check in prepareComponentFlush
// is the mechanism that catches changes made outside the lock — both exist
// because they protect against different things, not because one is a
// fallback for the other.
func AcquireComponentLocks(dir string, ids []ComponentID, wait time.Duration) (release func(), err error) {
	ordered := dedupeComponentIDs(ids)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })

	var held []func()
	release = func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i]()
		}
	}

	for _, id := range ordered {
		unlock, lockErr := acquireOneLock(lockFilePath(dir, id), id, wait)
		if lockErr != nil {
			release()
			return func() {}, lockErr
		}
		held = append(held, unlock)
	}
	return release, nil
}

// dedupeComponentIDs returns ids with duplicates removed, preserving the
// first occurrence's order. A caller that accidentally lists the same
// component twice would otherwise try to flock it twice in the same call —
// two separate open file descriptions on the same file conflict with each
// other even within one process, so the second attempt would see its own
// first lock as already held and fail or hang on itself.
func dedupeComponentIDs(ids []ComponentID) []ComponentID {
	seen := make(map[ComponentID]struct{}, len(ids))
	deduped := make([]ComponentID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	return deduped
}

func lockFilePath(dir string, id ComponentID) string {
	return filepath.Join(dir, "."+string(id)+".lock")
}

// acquireOneLock tries to lock path, retrying every lockPollInterval until
// wait has elapsed. wait == 0 still tries exactly once.
func acquireOneLock(path string, id ComponentID, wait time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)
	for {
		unlock, acquired, err := tryFlock(path)
		if err != nil {
			return nil, errorx.ExternalError.Wrap(err, "failed to acquire lock for %s", id)
		}
		if acquired {
			return unlock, nil
		}
		if time.Now().After(deadline) {
			return nil, errorx.IllegalState.New(
				"%s is being modified (held by %s); re-run with a longer wait or try again once it finishes",
				id, readLockHolder(path))
		}
		time.Sleep(lockPollInterval)
	}
}

// tryFlock takes a single non-blocking exclusive lock on path, creating it if
// needed. A lock held elsewhere returns (noop, false, nil); only a filesystem
// failure returns an error.
func tryFlock(path string) (func(), bool, error) {
	noop := func() {}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return noop, false, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return noop, false, err
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return noop, false, nil
		}
		return noop, false, err
	}

	// Best-effort diagnostic: record our pid so a contending waiter can report
	// who holds the lock. Failure here does not affect the lock itself.
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	}

	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true, nil
}

// readLockHolder returns a diagnostic-only description of who holds path's
// lock. Best-effort: the pid recorded there may belong to a process that has
// since exited without updating it.
func readLockHolder(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return "another process"
	}
	return "pid " + strings.TrimSpace(string(b))
}
