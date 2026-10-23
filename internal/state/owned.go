// SPDX-License-Identifier: Apache-2.0

package state

import (
	"path/filepath"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// RunOwning is the lock-and-flush for a command that does not go through
// internal/bll.BaseHandler. It holds ids' locks, reads state from disk, lets
// run change the host and patch record the outcome, then writes only ids'
// files. One set drives both the lock and the flush, so the command cannot
// write a file it has not locked. A lock already held, or state that cannot be
// read, fails before run starts and records nothing; once run has started,
// entry is appended to the action history however the command ends.
func RunOwning(sm Manager, entry ActionHistory, ids []ComponentID, run func() error, patch func(*State) error) error {
	release, err := AcquireComponentLocks(filepath.Dir(sm.State().StateFile), ids, 0)
	if err != nil {
		return err
	}
	defer release()

	if err := sm.Refresh(); err != nil && !errorx.IsOfType(err, NotFoundError) {
		return errx.Decorate(errorx.IllegalState.Wrap(err, "failed to read state"),
			reasons.StateOperationFailed,
			"Fix or move aside the state file named in the error, then re-run this command.")
	}

	if err := run(); err != nil {
		entry.Outcome = OutcomeFailed
		_ = sm.Record(entry, err) // with a cause, a lost entry is a state warning
		return err
	}

	entry.Outcome = OutcomeSucceeded
	st := sm.State()
	if err := patch(&st); err != nil {
		entry.NotPersisted = true
		_ = sm.Record(entry, err) // with a cause, a lost entry is a state warning
		return err
	}
	if err := sm.Set(st).Commit(entry, ids...); err != nil {
		if errorx.IsOfType(err, EntryNotAppendedError) || errorx.IsOfType(err, fsx.FileSyncError) {
			return err // the state did land; the caller says so
		}
		return errorx.IllegalState.Wrap(err, "failed to persist state")
	}
	return nil
}
