// SPDX-License-Identifier: Apache-2.0

package state

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
)

// RecordAround runs fn, an action that changes state files without Commit, and
// appends entry with Components set to the state files fn created, changed or
// removed, however fn ends. The outcome is fn's, and failed when fn returns an
// error with a succeeded outcome. The caller holds the locks for every state
// file fn writes. fn's error is returned; an entry that can't be appended, or
// was appended but not synced, is a state warning.
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

	// With fn's error as the cause, Record has already warned.
	if appendErr := appendEntry(dir, entry, err); appendErr != nil {
		warnRecordFailure(dir, appendErr)
	}
	return err
}
