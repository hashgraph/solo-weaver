// SPDX-License-Identifier: Apache-2.0

package state

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/joomcode/errorx"
)

// RecordAround runs fn, an action that changes state files without Commit, and
// appends entry with Components set to the state files fn created, changed or
// removed, however fn ends. The outcome is fn's, and failed when fn returns an
// error with a succeeded outcome. The caller holds the locks for every state
// file fn writes. When fn failed, its error is returned and a failed append is
// a state warning. When fn succeeded, a failed append is an
// EntryNotAppendedError, or the fsx.FileSyncError of an append that landed but
// was not synced.
func RecordAround(dir string, entry ActionHistory, fn func() (Outcome, error)) error {
	files := append(legacyStateFiles(dir), provisionerFilePath(dir))
	files = append(files, componentFiles(dir)...)
	before, snapErr := snapshotFiles(files)

	outcome, err := fn()
	if err != nil && outcome == OutcomeSucceeded {
		outcome = OutcomeFailed
	}
	entry.Outcome = outcome

	if snapErr == nil {
		var after fileSnapshot
		if after, snapErr = snapshotFiles(files); snapErr == nil {
			entry.Components = append(changedFiles(before, after), entry.Components...)
		}
	}
	if snapErr != nil {
		logx.As().Warn().Err(snapErr).Msg("Failed to read the state files the action touched")
		addWarning("The action ran, but the state files it changed could not be read: " +
			doctor.OperatorMessage(snapErr) + ". Its action history entry names none of them.")
	}

	appendErr := appendEntry(dir, entry, err)
	if err != nil {
		return err
	}
	if errorx.IsOfType(appendErr, fsx.FileSyncError) {
		return errorx.Decorate(appendErr, "the action ran and its action history entry was appended, but not synced to disk")
	}
	if appendErr != nil {
		return EntryNotAppendedError.Wrap(appendErr, "the action ran but its action history entry was not appended")
	}
	return nil
}
