// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
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
	ordered := DedupeComponentIDs(ids)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })

	var held []func()
	release = func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i]()
		}
	}

	for _, id := range ordered {
		path := lockFilePath(dir, id)
		unlock, acquired, pid, lockErr := acquireOneLock(path, wait)
		if lockErr != nil {
			release()
			return func() {}, lockOpenError(string(id), path, lockErr)
		}
		if !acquired {
			release()
			return func() {}, errx.Decorate(
				errorx.IllegalState.New("%s is being modified (held by %s)", id, lockHolder(pid)),
				reasons.PreconditionNotMet,
				waitForHolderHint(pid, "to finish"))
		}
		held = append(held, unlock)
	}
	return release, nil
}

func lockFilePath(dir string, id ComponentID) string {
	return filepath.Join(dir, "."+string(id)+".lock")
}

// AcquireStartupMigrationsLock takes an exclusive lock on dir that serializes
// startup migrations across concurrent invocations, waiting up to wait for
// one already running elsewhere. Several startup migrations (the
// per-component split in particular) delete and rename state files, so two
// running at once can fail spuriously or leave a reader seeing missing files.
func AcquireStartupMigrationsLock(dir string, wait time.Duration) (release func(), err error) {
	const label = "the state directory"
	path := filepath.Join(dir, ".startup-migrations.lock")
	unlock, acquired, pid, err := acquireOneLock(path, wait)
	if err != nil {
		return func() {}, lockOpenError(label, path, err)
	}
	if !acquired {
		waited := ""
		if wait > 0 {
			waited = fmt.Sprintf(" after waiting %s", wait)
		}
		return func() {}, errx.Decorate(
			errorx.IllegalState.New("%s is still being migrated by another command (held by %s)%s",
				label, lockHolder(pid), waited),
			reasons.PreconditionNotMet,
			waitForHolderHint(pid, "to finish its startup migrations"))
	}
	return unlock, nil
}

// AcquireProvisionerLock takes the lock that guards provisioner.yaml. It is
// never part of a command's owned components: each writer holds it only
// around its own write, except `kube cluster uninstall`, which deletes the file.
func AcquireProvisionerLock(dir string, wait time.Duration) (release func(), err error) {
	path := filepath.Join(dir, ".provisioner.lock")
	unlock, acquired, pid, err := acquireOneLock(path, wait)
	if err != nil {
		return func() {}, lockOpenError(ProvisionerFileName, path, err)
	}
	if !acquired {
		return func() {}, errx.Decorate(
			errorx.IllegalState.New("%s is being modified (held by %s)", ProvisionerFileName, lockHolder(pid)),
			reasons.PreconditionNotMet,
			waitForHolderHint(pid, "to finish"))
	}
	return unlock, nil
}

// acquireOneLock tries to lock path, retrying every lockPollInterval until
// wait has elapsed. wait == 0 still tries exactly once. A lock still held
// elsewhere once wait runs out is not an error: acquired is false and pid is
// the holder's recorded pid ("" when unknown), so each caller can say what to
// do. err is only a failure to create or open the lock file.
func acquireOneLock(path string, wait time.Duration) (unlock func(), acquired bool, pid string, err error) {
	deadline := time.Now().Add(wait)
	for {
		unlock, acquired, err := tryFlock(path)
		if err != nil {
			return unlock, false, "", err
		}
		if acquired {
			return unlock, true, "", nil
		}
		if time.Now().After(deadline) {
			return unlock, false, readLockPID(path), nil
		}
		time.Sleep(lockPollInterval)
	}
}

// lockOpenError wraps a failure to create or open a lock file. Only a
// permission error has an operator fix, so only it is decorated.
func lockOpenError(label, path string, err error) error {
	wrapped := errorx.ExternalError.Wrap(err, "failed to acquire lock for %s", label)
	if errors.Is(err, fs.ErrPermission) {
		return errx.Decorate(wrapped, reasons.PermissionDenied,
			fmt.Sprintf("Check the permissions on %s and its parent directory, or re-run with sudo.", path))
	}
	return wrapped
}

// lockHolder names a lock's holder for an error message.
func lockHolder(pid string) string {
	if pid == "" {
		return "another process"
	}
	return "pid " + pid
}

// waitForHolderHint asks the operator to wait for a lock's holder; done says
// what to wait for, e.g. "to finish".
func waitForHolderHint(pid, done string) string {
	if pid == "" {
		return fmt.Sprintf("Wait for the other solo-provisioner command %s, then re-run this command.", done)
	}
	return fmt.Sprintf("Wait for pid %s %s (check with: ps -p %s), then re-run this command.", pid, done, pid)
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

// readLockPID returns the pid recorded in path's lock file, or "" when none is
// recorded. Diagnostic only: the pid may belong to a process that has since
// exited without updating it.
func readLockPID(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
