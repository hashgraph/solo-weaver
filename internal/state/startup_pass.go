// SPDX-License-Identifier: Apache-2.0

package state

import (
	"path/filepath"
	"slices"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// StartupPass is what one startup migration pass ran: its entry's inputs.
// RecordStartupPass fills Migrations and Failed from the pass's run.
type StartupPass struct {
	FromVersion string   `yaml:"fromVersion" json:"fromVersion"`
	ToVersion   string   `yaml:"toVersion" json:"toVersion"`
	Migrations  []string `yaml:"migrations" json:"migrations"`
	Failed      string   `yaml:"failed,omitempty" json:"failed,omitempty"`
	// Pending is the migrations the pass runs, re-checked under the
	// startup-migrations lock. Those in this package declare the state files
	// they can touch, which is all the pass compares.
	Pending []migration.Migration `yaml:"-" json:"-"`
}

// StartupPassRun is what a pass's migrations did. The zero value is a pass
// that ran nothing.
type StartupPassRun struct {
	Outcome    Outcome  // succeeded when empty and the run returned no error
	Migrations []string // applied, in order
	Failed     string   // the one that failed, if any
}

// fileDeclarer is a startup migration that names every state file it can write.
type fileDeclarer interface{ stateFiles(dir string) []string }

// RecordStartupPass runs fn, the pass's migrations, under the caller's
// startup-migrations lock, then stamps the running version with stamp when it
// is not nil and fn succeeded. It appends one migrate/system entry when the
// pass changed a state file: a pass that changed nothing, such as one rolled
// back cleanly, a stamp that found the version current, or a stamp that failed
// on its own, is no state update. The migrations' files are compared before
// and after; provisioner.yaml is credited to the stamp only when stamp reports
// it changed the file, since a stamp-only pass runs without the startup lock
// and a concurrent invocation may write the same files. A stamp that fails
// warns the operator, and marks the entry NotPersisted when the pass changed
// files; one that wrote but could not sync only warns. A snapshot that fails
// records the entry without the migrations' files and warns. Recording is
// best-effort: a failed or unsynced append becomes a warning, and the error
// returned is fn's.
func RecordStartupPass(dir string, pass *StartupPass, stamp func() (changed bool, err error), fn func() (StartupPassRun, error)) error {
	// A stamp-only pass compares no file: anything that changes one meanwhile
	// is another command's.
	var files []string
	if len(pass.Pending) > 0 {
		files = legacyStateFiles(dir)
	}
	for _, m := range pass.Pending {
		if d, ok := m.(fileDeclarer); ok {
			files = append(files, d.stateFiles(dir)...)
		}
	}
	before, snapErr := snapshotFiles(files)

	run, err := fn()
	outcome := run.Outcome
	if err != nil {
		outcome = OutcomeFailed
	} else if outcome == "" {
		outcome = OutcomeSucceeded
	}
	pass.Migrations, pass.Failed = run.Migrations, run.Failed
	entry := ActionHistory{
		Intent:  models.Intent{Action: models.ActionMigrate, Target: models.TargetSystem},
		Inputs:  pass,
		Outcome: outcome,
	}
	stamped := false
	if err != nil {
		entry = withError(entry, err)
	} else if stamp != nil {
		var stampErr error
		stamped, stampErr = stamp()
		switch {
		case stampErr == nil:
		case errorx.IsOfType(stampErr, fsx.FileSyncError):
			// The record is on disk, and stamped says the file changed.
			logx.As().Warn().Err(stampErr).Msg("Recorded provisioner version after startup migrations, but could not sync it to disk")
			addWarning("The provisioner version was recorded but not synced to disk: " + doctor.OperatorMessage(stampErr) + ".")
		default:
			logx.As().Warn().Err(stampErr).
				Msg("Failed to record provisioner version after startup migrations; boundary migrations may re-run next invocation")
			addWarning("The provisioner version was not recorded: " + doctor.OperatorMessage(stampErr) +
				". Startup migrations may run again on the next invocation.")
			entry.NotPersisted = true
			entry = withError(entry, stampErr)
		}
	}

	if snapErr == nil {
		var after fileSnapshot
		if after, snapErr = snapshotFiles(files); snapErr == nil {
			entry.Components = changedFiles(before, after)
		}
	}
	// A migration that declares provisioner.yaml may already have named it;
	// otherwise it goes in its usual place, before legacy.
	if stamped && !slices.Contains(entry.Components, ChangedProvisioner) {
		at := slices.Index(entry.Components, ChangedLegacy)
		if at < 0 {
			at = len(entry.Components)
		}
		entry.Components = slices.Insert(entry.Components, at, ChangedProvisioner)
	}
	if snapErr != nil {
		logx.As().Warn().Err(snapErr).Msg("Failed to read the state files the startup migrations touched")
		addWarning("The startup migrations ran, but the state files they changed could not be read: " +
			doctor.OperatorMessage(snapErr) + ". Their action history entry names none of them.")
	} else if len(entry.Components) == 0 {
		return err // no state update: nothing changed; a failed stamp has already warned
	}
	if recordErr := appendEntry(dir, entry, nil); errorx.IsOfType(recordErr, fsx.FileSyncError) {
		addWarning("The startup migrations' action history entry was recorded but not synced to disk: " +
			doctor.OperatorMessage(recordErr) + ".")
	} else if recordErr != nil {
		addWarning("The startup migrations ran, but their action history entry was not recorded: " +
			doctor.OperatorMessage(recordErr) + ". Check that " + ActionHistoryFilePath(dir) + " is writable.")
	}
	return err
}

// historyFileManager returns the file manager the history appends use. It is
// a variable so tests can make an append's sync fail.
var historyFileManager = func() (fsx.Manager, error) { return fsx.NewManager() }

// appendEntry appends entry to dir's action history through Record, with
// cause as the action's failure. An fsx.FileSyncError means the entry was
// appended but not synced.
func appendEntry(dir string, entry ActionHistory, cause error) error {
	fm, err := historyFileManager()
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to create file manager for the action history")
	}
	sm, err := newStateManager(WithStateFile(filepath.Join(dir, StateFileName)), WithFileManager(fm))
	if err != nil {
		return err
	}
	return sm.Record(entry, cause)
}
