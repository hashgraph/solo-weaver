// SPDX-License-Identifier: Apache-2.0

package state

import (
	"path/filepath"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// RunOwning is the lock-and-flush for a command that does not go through
// internal/bll.BaseHandler. It holds ids' locks, reads state from disk, lets
// run change the host and patch record the outcome, then writes only ids'
// files. One set drives both the lock and the flush, so the command cannot
// write a file it has not locked. A lock already held, or state that cannot be
// read, fails before run starts.
func RunOwning(sm Manager, ids []ComponentID, run func() error, patch func(*State) error) error {
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
		return err
	}

	st := sm.State()
	if err := patch(&st); err != nil {
		return err
	}
	if err := sm.Set(st).FlushScoped(ids...); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to persist state")
	}
	return nil
}
