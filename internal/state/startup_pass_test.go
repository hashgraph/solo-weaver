// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

// passDir is a state dir holding a legacy state.yaml, with a pending pass that
// declares it.
func passDir(t *testing.T) (string, *StartupPass) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, StateFileName), []byte(legacySSHPortYAML), 0o644))
	return dir, &StartupPass{FromVersion: "0.14.0", ToVersion: "0.15.0", Pending: []migration.Migration{NewMgmtPortsV1Migration()}}
}

func passHistory(t *testing.T, dir string) []ActionHistory {
	t.Helper()
	if _, err := os.Stat(ActionHistoryFilePath(dir)); os.IsNotExist(err) {
		return nil
	}
	return decodeHistoryFile(t, ActionHistoryFilePath(dir))
}

// failedCleanly is a pass whose migration failed and whose rollback put every
// state file back.
func failedCleanly(failed string) func() (StartupPassRun, error) {
	return func() (StartupPassRun, error) {
		return StartupPassRun{Outcome: OutcomeFailed, Migrations: []string{"mgmt-ports-v1"}, Failed: failed}, errors.New(failed + " failed")
	}
}

// TestRecordStartupPass_RecordsOnePassEntry: a pass that ran a migration and
// stamped the version is one migrate/system entry, naming what it ran and the
// files it changed.
func TestRecordStartupPass_RecordsOnePassEntry(t *testing.T) {
	dir, pass := passDir(t)
	stamp := func() (bool, error) {
		return true, writeProvisionerRecord(dir, ProvisionerRecord{CLI: ProvisionerInfo{Version: "0.15.0"}})
	}

	require.NoError(t, RecordStartupPass(dir, pass, stamp, func() (StartupPassRun, error) {
		return StartupPassRun{Migrations: []string{"mgmt-ports-v1"}}, os.WriteFile(filepath.Join(dir, StateFileName), []byte(mgmtPortsYAML), 0o644)
	}))

	entries := passHistory(t, dir)
	require.Len(t, entries, 1)
	require.Equal(t, models.Intent{Action: models.ActionMigrate, Target: models.TargetSystem}, entries[0].Intent)
	require.Equal(t, OutcomeSucceeded, entries[0].Outcome)
	require.Equal(t, []ChangedFile{ChangedProvisioner, ChangedLegacy}, entries[0].Components)
	inputs, ok := entries[0].Inputs.(map[string]any)
	require.True(t, ok, "inputs: got %T", entries[0].Inputs)
	require.Equal(t, "0.14.0", inputs["fromVersion"])
	require.Equal(t, []any{"mgmt-ports-v1"}, inputs["migrations"])
	require.NotContains(t, inputs, "pending")
}

// TestRecordStartupPass_NothingChangedRecordsNothing: a clean pass that changed
// no state file, such as host-only migrations on a host with nothing to stamp,
// is no state update.
func TestRecordStartupPass_NothingChangedRecordsNothing(t *testing.T) {
	dir, pass := passDir(t)

	require.NoError(t, RecordStartupPass(dir, pass, nil, func() (StartupPassRun, error) { return StartupPassRun{}, nil }))

	require.Empty(t, passHistory(t, dir))
}

// TestRecordStartupPass_RecordsOnlyFailuresThatChangedAFile: a failed pass
// whose rollback put every state file back changed no state, so it records
// nothing, on every attempt; one that left a file changed records failed.
func TestRecordStartupPass_RecordsOnlyFailuresThatChangedAFile(t *testing.T) {
	dir, pass := passDir(t)
	leftChanged := func() (StartupPassRun, error) {
		state, err := os.ReadFile(filepath.Join(dir, StateFileName))
		require.NoError(t, err)
		return StartupPassRun{Outcome: OutcomeFailed, Migrations: []string{"mgmt-ports-v1"}, Failed: "per-component-state"}, errors.Join(errors.New("rollback failed"),
			os.WriteFile(filepath.Join(dir, StateFileName), append(state, '\n'), 0o644))
	}

	for range 3 {
		require.Error(t, RecordStartupPass(dir, pass, nil, failedCleanly("cilium-host-legacy-routing")))
	}
	require.Empty(t, passHistory(t, dir), "a failure that changed no state file is no state update")

	require.Error(t, RecordStartupPass(dir, pass, nil, leftChanged))
	entries := passHistory(t, dir)
	require.Len(t, entries, 1)
	require.Equal(t, OutcomeFailed, entries[0].Outcome)
	require.Equal(t, []ChangedFile{ChangedLegacy}, entries[0].Components)
	require.Contains(t, entries[0].Error, "rollback failed")
	inputs, ok := entries[0].Inputs.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "per-component-state", inputs["failed"])
}

// TestRecordStartupPass_LoneFailedStampOnlyWarns: a pass that changed nothing
// and could not stamp the version is no state update, so it appends nothing;
// the operator is warned, and the pass itself does not fail. Otherwise a stamp
// that keeps failing would add the same entry on every invocation.
func TestRecordStartupPass_LoneFailedStampOnlyWarns(t *testing.T) {
	emptyWarnings(t)
	dir := t.TempDir()
	failedStamp := func() (bool, error) { return false, errors.New("disk full") }

	require.NoError(t, RecordStartupPass(dir, &StartupPass{}, failedStamp, func() (StartupPassRun, error) { return StartupPassRun{}, nil }))

	require.Empty(t, passHistory(t, dir))
	warnings := TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "The provisioner version was not recorded")
}

// TestRecordStartupPass_FailedStampAfterChangedFilesIsNotPersisted: a pass that
// changed files but could not stamp the version is recorded, marked
// NotPersisted with the stamp's error, and the operator is warned.
func TestRecordStartupPass_FailedStampAfterChangedFilesIsNotPersisted(t *testing.T) {
	emptyWarnings(t)
	dir, pass := passDir(t)
	failedStamp := func() (bool, error) { return false, errors.New("disk full") }

	require.NoError(t, RecordStartupPass(dir, pass, failedStamp, func() (StartupPassRun, error) {
		return StartupPassRun{Migrations: []string{"mgmt-ports-v1"}}, os.WriteFile(filepath.Join(dir, StateFileName), []byte(mgmtPortsYAML), 0o644)
	}))

	entries := passHistory(t, dir)
	require.Len(t, entries, 1)
	require.Equal(t, OutcomeSucceeded, entries[0].Outcome)
	require.True(t, entries[0].NotPersisted)
	require.Contains(t, entries[0].Error, "disk full")
	require.Equal(t, []ChangedFile{ChangedLegacy}, entries[0].Components)
	warnings := TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "The provisioner version was not recorded")
}

// TestRecordStartupPass_LostEntryIsAWarning: when the entry cannot be appended,
// the operator is warned and the pass's own result is returned unchanged.
func TestRecordStartupPass_LostEntryIsAWarning(t *testing.T) {
	emptyWarnings(t)
	dir, pass := passDir(t)
	require.NoError(t, os.Mkdir(ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails

	err := RecordStartupPass(dir, pass, nil, func() (StartupPassRun, error) {
		return StartupPassRun{}, os.WriteFile(filepath.Join(dir, StateFileName), []byte(mgmtPortsYAML), 0o644)
	})

	require.NoError(t, err, "a lost entry does not fail the pass")
	warnings := TakeWarnings()
	require.Len(t, warnings, 1)
	require.True(t, strings.HasPrefix(warnings[0], "The startup migrations ran, but their action history entry was not recorded"))
}

// TestStateMigrations_TouchOnlyTheirDeclaredFiles runs each state migration's
// Execute and Rollback on a state dir and fails if it creates, changes or
// removes a file it does not declare: the pass compares only declared files.
func TestStateMigrations_TouchOnlyTheirDeclaredFiles(t *testing.T) {
	for name, tc := range map[string]struct {
		mig interface {
			migration.Migration
			fileDeclarer
		}
		files map[string]string
	}{
		"unified-state into state.yaml": {NewUnifiedStateMigration(), map[string]string{
			StateFileName: mgmtPortsYAML, "kubelet.installed": "installed at version 1.0.0\n",
		}},
		"unified-state into system.yaml": {NewUnifiedStateMigration(), map[string]string{
			"kubelet.installed": "installed at version 1.0.0\n", "kubelet.configured": "configured at version 1.0.0\n",
		}},
		"helm-release-schema-v2": {NewHelmReleaseSchemaV2Migration(), map[string]string{StateFileName: v1StateYAML}},
		"mgmt-ports-v1":          {NewMgmtPortsV1Migration(), map[string]string{StateFileName: legacySSHPortYAML}},
		"per-component-state":    {NewPerComponentStateMigration(), map[string]string{StateFileName: mgmtPortsYAML}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(models.SetPaths(t.TempDir()))
			dir := models.Paths().StateDir
			require.NoError(t, os.MkdirAll(dir, 0o755))
			for f, content := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte(content), 0o644))
			}
			mctx := &migration.Context{Component: migration.ScopeStartup}

			before := snapshotDir(t, dir)
			require.NoError(t, tc.mig.Execute(context.Background(), mctx))
			executed := snapshotDir(t, dir)
			require.NotEqual(t, before, executed, "the fixture must make Execute write something")
			requireDeclared(t, tc.mig.stateFiles(dir), before, executed)

			require.NoError(t, tc.mig.Rollback(context.Background(), mctx))
			requireDeclared(t, tc.mig.stateFiles(dir), executed, snapshotDir(t, dir))
		})
	}
}

// snapshotDir hashes every file in dir but the lock files.
func snapshotDir(t *testing.T, dir string) fileSnapshot {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && !strings.HasSuffix(e.Name(), ".lock") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	snap, err := snapshotFiles(paths)
	require.NoError(t, err)
	return snap
}

func requireDeclared(t *testing.T, declared []string, before, after fileSnapshot) {
	t.Helper()
	matches := func(path string) bool {
		for _, pattern := range declared {
			if ok, _ := filepath.Match(pattern, path); ok {
				return true
			}
		}
		return false
	}
	for path, h := range before {
		if after[path] != h {
			require.Truef(t, matches(path), "%s changed or was removed but is not declared", path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			require.Truef(t, matches(path), "%s was created but is not declared", path)
		}
	}
}

// TestRecordStartupPass_StampThatChangesNothingRecordsNothing: a second stamp of
// the same binary, such as a concurrent invocation's, leaves provisioner.yaml as
// it is, so the pass changed no state file and appends nothing.
func TestRecordStartupPass_StampThatChangesNothingRecordsNothing(t *testing.T) {
	dir := t.TempDir()
	stamp := func() (bool, error) { return persistProvisionerVersionIn(dir) }
	nothing := func() (StartupPassRun, error) { return StartupPassRun{}, nil }

	require.NoError(t, RecordStartupPass(dir, &StartupPass{}, stamp, nothing))
	require.NoError(t, RecordStartupPass(dir, &StartupPass{}, stamp, nothing))

	entries := passHistory(t, dir)
	require.Len(t, entries, 1, "only the stamp that wrote provisioner.yaml is recorded")
	require.Equal(t, []ChangedFile{ChangedProvisioner}, entries[0].Components)
}

// TestRecordStartupPass_UnreadableSnapshotStillRecordsAndWarns: when the files
// the pass touched can't be read, the entry is appended with no components and
// the operator is warned, rather than the pass going unrecorded.
func TestRecordStartupPass_UnreadableSnapshotStillRecordsAndWarns(t *testing.T) {
	emptyWarnings(t)
	dir, pass := passDir(t)
	// A directory where a legacy file's glob matches cannot be read as a file.
	require.NoError(t, os.Mkdir(filepath.Join(dir, StateFileName+".diverged-x"), 0o755))

	require.NoError(t, RecordStartupPass(dir, pass, nil, func() (StartupPassRun, error) { return StartupPassRun{}, nil }))

	entries := passHistory(t, dir)
	require.Len(t, entries, 1)
	require.Empty(t, entries[0].Components)
	warnings := TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "the state files they changed could not be read")
}

// TestRecordStartupPass_UnsyncedStampIsRecorded: a version stamp that wrote
// provisioner.yaml but could not sync it is on disk, so the entry is not
// NotPersisted, names provisioner, and the warning says it was not synced.
func TestRecordStartupPass_UnsyncedStampIsRecorded(t *testing.T) {
	emptyWarnings(t)
	dir := t.TempDir()
	recordDirSyncs(t, fsx.FileSyncError.New("disk gone"))
	stamp := func() (bool, error) { return persistProvisionerVersionIn(dir) }

	require.NoError(t, RecordStartupPass(dir, &StartupPass{}, stamp, func() (StartupPassRun, error) { return StartupPassRun{}, nil }))

	entries := passHistory(t, dir)
	require.Len(t, entries, 1)
	require.False(t, entries[0].NotPersisted)
	require.Equal(t, []ChangedFile{ChangedProvisioner}, entries[0].Components)
	warnings := TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "The provisioner version was recorded but not synced to disk")
}

// TestRecordStartupPass_UnsyncedEntryWarnsNotSynced: an entry appended but not
// synced is in the history, so the warning says it was not synced rather than
// that it was not recorded.
func TestRecordStartupPass_UnsyncedEntryWarnsNotSynced(t *testing.T) {
	emptyWarnings(t)
	orig := historyFileManager
	t.Cleanup(func() { historyFileManager = orig })
	historyFileManager = func() (fsx.Manager, error) {
		return &unsyncedAppender{Manager: newTestFileManager(t), err: fsx.FileSyncError.New("disk gone")}, nil
	}
	dir, pass := passDir(t)

	require.NoError(t, RecordStartupPass(dir, pass, nil, func() (StartupPassRun, error) {
		return StartupPassRun{}, os.Remove(filepath.Join(dir, StateFileName))
	}))

	require.Len(t, passHistory(t, dir), 1)
	warnings := TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "action history entry was recorded but not synced to disk")
	require.NotContains(t, warnings[0], "was not recorded")
}

// TestRecordStartupPass_ConcurrentStampsCreditTheVersionOnce: two stamp-only
// passes of the same binary, as two invocations right after an upgrade run,
// record provisioner.yaml once: the second waits out the first's lock and
// finds the version current.
func TestRecordStartupPass_ConcurrentStampsCreditTheVersionOnce(t *testing.T) {
	emptyWarnings(t)
	dir := t.TempDir()
	stamp := func() (bool, error) { return persistProvisionerVersionIn(dir) }
	nothing := func() (StartupPassRun, error) { return StartupPassRun{}, nil }

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = RecordStartupPass(dir, &StartupPass{}, stamp, nothing)
		}()
	}
	wg.Wait()
	require.NoError(t, errors.Join(errs...))

	entries := passHistory(t, dir)
	require.Len(t, entries, 1)
	require.Equal(t, []ChangedFile{ChangedProvisioner}, entries[0].Components)
	require.False(t, entries[0].NotPersisted)
	require.Empty(t, TakeWarnings())
}

// TestRecordStartupPass_StampOnlyPassCreditsNoConcurrentWrite: a stamp-only pass
// compares no file, so a command that removes state.yaml.legacy or
// provisioner.yaml while it runs, such as kube cluster uninstall, is not
// credited to it.
func TestRecordStartupPass_StampOnlyPassCreditsNoConcurrentWrite(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, StateFileName+legacyStateBackupSuffix)
	require.NoError(t, os.WriteFile(legacy, []byte(legacySSHPortYAML), 0o644))
	_, err := persistProvisionerVersionIn(dir)
	require.NoError(t, err)
	concurrentRemoval := func() (StartupPassRun, error) {
		require.NoError(t, os.Remove(legacy))
		require.NoError(t, os.Remove(provisionerFilePath(dir)))
		return StartupPassRun{}, nil
	}
	foundCurrent := func() (bool, error) { return false, nil }

	require.NoError(t, RecordStartupPass(dir, &StartupPass{}, foundCurrent, concurrentRemoval))

	require.Empty(t, passHistory(t, dir))
}

// TestRecordStartupPass_StampWaitsOutAShortHeldLock: a stamp that meets another
// invocation's provisioner lock waits for it rather than failing, finds the
// version that invocation recorded, and appends no entry.
func TestRecordStartupPass_StampWaitsOutAShortHeldLock(t *testing.T) {
	emptyWarnings(t)
	dir := t.TempDir()
	_, err := persistProvisionerVersionIn(dir)
	require.NoError(t, err)
	release, err := AcquireProvisionerLock(dir, 0)
	require.NoError(t, err)
	time.AfterFunc(300*time.Millisecond, release)
	stamp := func() (bool, error) { return persistProvisionerVersionIn(dir) }

	require.NoError(t, RecordStartupPass(dir, &StartupPass{}, stamp, func() (StartupPassRun, error) { return StartupPassRun{}, nil }))

	require.Empty(t, passHistory(t, dir), "the version was current once the lock was free")
	require.Empty(t, TakeWarnings())
}
