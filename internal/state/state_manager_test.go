// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// newTestState returns a State with the StateFile pointing to tmp.
func newTestState(tmp string) State {
	s := NewState("")
	s.StateFile = tmp
	return s
}

// newTestFileManager returns an fsx.Manager backed by a mock principal.Manager that
// resolves the "hedera" service-account user/group to the current OS user.
// This avoids the "user hedera not found" error that occurs when WriteFile tries to
// chown the written file to the service account.
func newTestFileManager(t *testing.T) fsx.Manager {
	t.Helper()
	ctrl := gomock.NewController(t)

	currentUser, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current OS user: %v", err)
	}

	mockUser := principal.NewMockUser(ctrl)
	mockUser.EXPECT().Uid().Return(currentUser.Uid).AnyTimes()
	mockUser.EXPECT().Name().Return(currentUser.Username).AnyTimes()

	mockGroup := principal.NewMockGroup(ctrl)
	mockGroup.EXPECT().Gid().Return(currentUser.Gid).AnyTimes()

	mockUser.EXPECT().PrimaryGroup().Return(mockGroup).AnyTimes()

	pm := principal.NewMockManager(ctrl)
	pm.EXPECT().LookupUserByName(config.WeaverUserName()).Return(mockUser, nil).AnyTimes()
	pm.EXPECT().LookupGroupByName(config.WeaverGroupName()).Return(mockGroup, nil).AnyTimes()

	fm, err := fsx.NewManager(fsx.WithPrincipalManager(pm))
	if err != nil {
		t.Fatalf("failed to create test file manager: %v", err)
	}
	return fm
}

// TestFlushWritesFile checks that FlushState writes each component's YAML
// representation to its own file, keyed by the profile (which lives in the
// profile component's file).
func TestFlushWritesFile(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "state.yaml")

	s := newTestState(tmp)
	s.MachineState.Profile = "v1-test-flush"

	m, err := NewStateManager(WithState(s), WithFileManager(newTestFileManager(t)))
	if err != nil {
		t.Fatalf("NewStateManager returned error: %v", err)
	}

	err = m.Refresh()
	require.NoError(t, err)

	if err := m.Commit(ActionHistory{}, AllComponentIDs...); err != nil {
		t.Fatalf("FlushState returned error: %v", err)
	}

	data, err := os.ReadFile(componentFilePath(dir, ComponentProfile))
	if err != nil {
		t.Fatalf("failed to read persisted profile state file: %v", err)
	}

	var loaded State
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal persisted YAML: %v", err)
	}

	if loaded.MachineState.Profile != "v1-test-flush" {
		t.Fatalf("unexpected profile in persisted state: got %q want %q", loaded.MachineState.Profile, "v1-test-flush")
	}
}

// TestRefreshLoadsFile checks that Refresh loads each component's on-disk file
// into the manager, keyed by the profile.
func TestRefreshLoadsFile(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "state.yaml")

	// Seed a state on disk through a real flush, the way production writes it.
	onDisk := newTestState(tmp)
	onDisk.MachineState.Profile = "v2-test-refresh"
	fm := newTestFileManager(t)
	seedMgr, err := NewStateManager(WithState(onDisk), WithFileManager(fm))
	if err != nil {
		t.Fatalf("NewStateManager for seed returned error: %v", err)
	}
	if err := seedMgr.Commit(ActionHistory{}, AllComponentIDs...); err != nil {
		t.Fatalf("failed to seed on-disk state: %v", err)
	}

	// Create a manager with a different in-memory state, pointing to the same directory.
	mem := newTestState(tmp)
	mem.MachineState.Profile = "before-refresh"

	m, err := NewStateManager(WithState(mem), WithFileManager(fm))
	if err != nil {
		t.Fatalf("NewStateManager returned error: %v", err)
	}

	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}

	if got := m.State().MachineState.Profile; got != "v2-test-refresh" {
		t.Fatalf("Refresh did not update the profile: got %q want %q", got, "v2-test-refresh")
	}
}

// TestNewStateManager_DoesNotAutoRefresh verifies that NewStateManager does NOT
// automatically load state from disk during construction.
// Callers are expected to call Refresh() explicitly when they want to load persisted state.
func TestNewStateManager_DoesNotAutoRefresh(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "state.yaml")
	fm := newTestFileManager(t)

	// Seed an on-disk state with a recognisable profile through a real flush.
	onDisk := newTestState(tmp)
	onDisk.MachineState.Profile = "on-disk-version"
	seedMgr, err := NewStateManager(WithState(onDisk), WithFileManager(fm))
	if err != nil {
		t.Fatalf("NewStateManager for seed returned error: %v", err)
	}
	if err := seedMgr.Commit(ActionHistory{}, AllComponentIDs...); err != nil {
		t.Fatalf("failed to seed on-disk state: %v", err)
	}

	// Construct a manager with a different in-memory profile and the same directory.
	mem := newTestState(tmp)
	mem.MachineState.Profile = "in-memory-version"

	m, err := NewStateManager(WithState(mem), WithFileManager(fm))
	if err != nil {
		t.Fatalf("NewStateManager returned error: %v", err)
	}

	// Construction must NOT have loaded the on-disk state — the in-memory profile must be unchanged.
	if got := m.State().MachineState.Profile; got != "in-memory-version" {
		t.Fatalf("NewStateManager auto-refreshed state from disk: got profile %q, want %q", got, "in-memory-version")
	}
}

// TestHasPersistedState verifies HasPersistedState reports the presence of
// any component's file.
func TestHasPersistedState(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "state.yaml")

	// write a component file directly
	if err := os.WriteFile(componentFilePath(dir, ComponentProfile), []byte("dummy"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	s := newTestState(tmp)

	m, err := NewStateManager(WithState(s), WithFileManager(newTestFileManager(t)))
	if err != nil {
		t.Fatalf("NewStateManager returned error: %v", err)
	}

	fi, ok, err := m.HasPersistedState()
	if err != nil {
		t.Fatalf("HasPersistedState returned error: %v", err)
	}
	if !ok {
		t.Fatalf("HasPersistedState returned ok=false, expected true")
	}
	if fi == nil {
		t.Fatalf("HasPersistedState returned nil FileInfo")
	}
	if fi.Size() == 0 {
		t.Fatalf("expected non-zero file size")
	}
}

// decodeHistoryFile reads the action_history.yaml stream and returns all decoded entries.
// Each entry is stored as an independent YAML document separated by "---".
func decodeHistoryFile(t *testing.T, path string) []ActionHistory {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read history file %s: %v", path, err)
	}

	var entries []ActionHistory
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var entry ActionHistory
		if err := dec.Decode(&entry); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("failed to decode history entry: %v", err)
		}
		entries = append(entries, entry)
	}
	return entries
}

// TestCommit_AppendsEachEntryWithItsTimestamp verifies that every Commit
// appends its entry to the history file as its own YAML document, stamped with
// the time it was recorded.
func TestCommit_AppendsEachEntryWithItsTimestamp(t *testing.T) {
	dir := t.TempDir()
	historyFile := filepath.Join(dir, "action_history.yaml")

	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)

	require.NoError(t, m.Commit(ActionHistory{
		Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode},
		Inputs: map[string]any{"profile": "local", "version": "0.1.0"},
	}, ComponentBlockNode))
	require.NoError(t, m.Commit(ActionHistory{
		Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetBlockNode},
		Inputs: map[string]any{"profile": "local"},
	}, ComponentBlockNode))

	entries := decodeHistoryFile(t, historyFile)
	require.Len(t, entries, 2)
	require.Equal(t, models.ActionInstall, entries[0].Intent.Action)
	require.Equal(t, models.ActionUninstall, entries[1].Intent.Action)
	// After YAML round-trip, Inputs is decoded as map[string]interface{}.
	inputs0, ok := entries[0].Inputs.(map[string]interface{})
	require.True(t, ok, "entry[0] Inputs: got %T", entries[0].Inputs)
	require.Equal(t, "0.1.0", inputs0["version"])
	require.False(t, entries[0].Timestamp.IsZero(), "Commit stamps the timestamp")
	require.False(t, entries[1].Timestamp.IsZero(), "Commit stamps the timestamp")
}

// TestCommit_AccumulatesWithoutRewriting verifies that successive commits
// append to the history file without rewriting earlier entries.
func TestCommit_AccumulatesWithoutRewriting(t *testing.T) {
	dir := t.TempDir()
	historyFile := filepath.Join(dir, "action_history.yaml")

	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)

	require.NoError(t, m.Commit(ActionHistory{Intent: models.Intent{Action: models.ActionSetup, Target: models.TargetCluster}}, ComponentCluster))
	first, err := os.ReadFile(historyFile)
	require.NoError(t, err)

	require.NoError(t, m.Commit(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}}, ComponentBlockNode))
	require.NoError(t, m.Record(ActionHistory{Intent: models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlockNode}}, nil))
	second, err := os.ReadFile(historyFile)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(second, first), "earlier entries must not be rewritten")

	entries := decodeHistoryFile(t, historyFile)
	wantActions := []models.ActionType{models.ActionSetup, models.ActionInstall, models.ActionUpgrade}
	require.Len(t, entries, len(wantActions))
	for i, want := range wantActions {
		require.Equal(t, want, entries[i].Intent.Action, "entry[%d]", i)
	}
}

// TestCommit_RecordsTheFilesItChanged verifies that an entry names the files
// whose content the commit changed, first writes included, and none it only
// rewrote; files the caller names that belong to no component are kept.
func TestCommit_RecordsTheFilesItChanged(t *testing.T) {
	dir := t.TempDir()
	historyFile := filepath.Join(dir, "action_history.yaml")

	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)

	install := ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}}
	require.NoError(t, m.Commit(install, ComponentProfile, ComponentBlockNode))
	require.NoError(t, m.Commit(install, ComponentProfile, ComponentBlockNode))
	changed := m.State()
	changed.MachineState.Profile = "testnet"
	stamped := install
	stamped.Components = []ChangedFile{ChangedProvisioner}
	require.NoError(t, m.Set(changed).Commit(stamped, ComponentProfile, ComponentBlockNode))

	entries := decodeHistoryFile(t, historyFile)
	require.Len(t, entries, 3)
	require.Equal(t, []ChangedFile{ComponentProfile.File(), ComponentBlockNode.File()}, entries[0].Components,
		"a first write changes the file")
	require.Empty(t, entries[1].Components, "rewriting the same content changes nothing")
	require.Equal(t, []ChangedFile{ComponentProfile.File(), ChangedProvisioner}, entries[2].Components)
}

// TestRecord_RendersTheCauseForTheOperator verifies that a recorded error keeps
// the operator's message and moves its reason code into its own field.
func TestRecord_RendersTheCauseForTheOperator(t *testing.T) {
	dir := t.TempDir()
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)

	cause := errx.Decorate(errorx.IllegalState.New("kubeadm init failed"), reasons.StateOperationFailed, "Re-run.")
	require.NoError(t, m.Record(ActionHistory{
		Intent:  models.Intent{Action: models.ActionInstall, Target: models.TargetCluster},
		Outcome: OutcomeFailed,
	}, cause))

	entries := decodeHistoryFile(t, ActionHistoryFilePath(dir))
	require.Len(t, entries, 1)
	require.Equal(t, OutcomeFailed, entries[0].Outcome)
	require.Contains(t, entries[0].Error, "kubeadm init failed")
	require.NotContains(t, entries[0].Error, "reason", "the reason code belongs in its own field")
	require.Equal(t, reasons.StateOperationFailed, entries[0].Reason)
	require.False(t, entries[0].NotPersisted)
}

// TestFlushState_DirectorySyncFailureAdvancesBaseline verifies that a flush whose directory
// sync fails still replaced the file, so the next flush doesn't see it as an external change.
func TestFlushState_DirectorySyncFailureAdvancesBaseline(t *testing.T) {
	m, err := newStateManager(WithState(newTestState(filepath.Join(t.TempDir(), "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, m.writeUnrecorded(AllComponentIDs...))

	syncErr := fsx.FileSyncError.New("disk gone")
	recordDirSyncs(t, syncErr)
	s := m.State()
	s.MachineState.Profile = "local"
	m.Set(s)
	require.ErrorIs(t, m.writeUnrecorded(AllComponentIDs...), syncErr)

	recordDirSyncs(t, nil)
	s.MachineState.Profile = "perfnet"
	m.Set(s)
	require.NoError(t, m.writeUnrecorded(AllComponentIDs...))
}

// TestFlushAll_DirectorySyncFailureStillAppendsHistory verifies that a state flush whose
// directory sync fails still records its action, since the new state is on disk.
func TestFlushAll_DirectorySyncFailureStillAppendsHistory(t *testing.T) {
	dir := t.TempDir()
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	syncErr := fsx.FileSyncError.New("disk gone")
	recordDirSyncs(t, syncErr)

	require.ErrorIs(t, m.Commit(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}}, AllComponentIDs...), syncErr)

	require.Len(t, decodeHistoryFile(t, filepath.Join(dir, "action_history.yaml")), 1)
}

// unsyncedAppender is an fsx.Manager whose first AppendToFile writes the payload and
// then reports a failed sync.
type unsyncedAppender struct {
	fsx.Manager
	err error
}

func (a *unsyncedAppender) AppendToFile(path string, payload []byte) error {
	if err := a.Manager.AppendToFile(path, payload); err != nil {
		return err
	}
	err := a.err
	a.err = nil
	return err
}

// TestFlushActionHistory_SyncFailureClearsPending verifies that entries appended before a
// failed sync are not appended again by the next flush.
func TestFlushActionHistory_SyncFailureClearsPending(t *testing.T) {
	dir := t.TempDir()
	syncErr := fsx.FileSyncError.New("disk gone")
	fm := &unsyncedAppender{Manager: newTestFileManager(t), err: syncErr}
	m, err := newStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(fm))
	require.NoError(t, err)

	require.ErrorIs(t, m.Record(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}}, nil), syncErr)
	require.NoError(t, m.flushActionHistory())

	require.Len(t, decodeHistoryFile(t, filepath.Join(dir, "action_history.yaml")), 1)
}

// TestFlushActionHistory_NothingPendingWritesNothing verifies that a flush with no pending
// entries doesn't create the history file.
func TestFlushActionHistory_NothingPendingWritesNothing(t *testing.T) {
	dir := t.TempDir()
	m, err := newStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)

	require.NoError(t, m.flushActionHistory())

	require.NoFileExists(t, filepath.Join(dir, "action_history.yaml"))
}

// TestFlushActionHistory_AfterTornEntryStartsOnNewLine verifies that an entry appended after
// one a full disk cut short still decodes on its own.
func TestFlushActionHistory_AfterTornEntryStartsOnNewLine(t *testing.T) {
	dir := t.TempDir()
	historyFile := filepath.Join(dir, "action_history.yaml")
	require.NoError(t, os.WriteFile(historyFile, []byte("---\nintent:\n  action: [inst"), 0o644))
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)

	require.NoError(t, m.Record(ActionHistory{Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetBlockNode}}, nil))

	got, err := os.ReadFile(historyFile)
	require.NoError(t, err)
	docs := strings.Split(string(got), "\n---\n")
	var last ActionHistory
	require.NoError(t, yaml.Unmarshal([]byte(docs[len(docs)-1]), &last))
	require.Equal(t, models.ActionUninstall, last.Intent.Action)
}

// concurrentAdder is an fsx.Manager whose AppendToFile runs add after writing, as if
// another goroutine added an entry while the append ran.
type concurrentAdder struct {
	fsx.Manager
	add func()
}

func (a *concurrentAdder) AppendToFile(path string, payload []byte) error {
	err := a.Manager.AppendToFile(path, payload)
	if a.add != nil {
		a.add()
		a.add = nil
	}
	return err
}

// TestFlushActionHistory_KeepsEntriesAddedDuringAppend verifies that a flush clears only
// the entries it appended.
func TestFlushActionHistory_KeepsEntriesAddedDuringAppend(t *testing.T) {
	dir := t.TempDir()
	fm := &concurrentAdder{Manager: newTestFileManager(t)}
	m, err := newStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(fm))
	require.NoError(t, err)
	m.addActionHistory(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}})
	fm.add = func() {
		m.addActionHistory(ActionHistory{Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetBlockNode}})
	}

	require.NoError(t, m.flushActionHistory())
	require.NoError(t, m.flushActionHistory())

	entries := decodeHistoryFile(t, filepath.Join(dir, "action_history.yaml"))
	require.Len(t, entries, 2)
	require.Equal(t, models.ActionUninstall, entries[1].Intent.Action)
}

// An entry records the owned drift its action ran over; an entry with none
// carries no ownedDrift key.
func TestActionHistory_EntryRecordsOwnedDrift(t *testing.T) {
	dir := t.TempDir()
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	drifted := []OwnedDriftRecord{{
		Component: "teleport", Field: "clusterAgent.chartVersion", Persisted: "18.1.0", Live: "18.2.1",
		DestroyAllowed: true, Destroyed: true,
	}}

	require.NoError(t, m.Record(ActionHistory{
		Intent:     models.Intent{Action: models.ActionUninstall, Target: models.TargetTeleportCluster},
		OwnedDrift: drifted,
	}, nil))
	require.NoError(t, m.Record(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetTeleportCluster}}, nil))

	f, err := os.Open(ActionHistoryFilePath(dir))
	require.NoError(t, err)
	defer f.Close()
	dec := yaml.NewDecoder(f)
	var drifting ActionHistory
	require.NoError(t, dec.Decode(&drifting))
	var plain map[string]any
	require.NoError(t, dec.Decode(&plain))

	require.Equal(t, drifted, drifting.OwnedDrift)
	require.NotContains(t, plain, "ownedDrift")
}

// TestFlushState_HandEditWithStaleHashFieldIsDetected verifies the optimistic-
// concurrency check recomputes the on-disk hash from content rather than
// trusting the file's own stored hash field. A hand edit naturally leaves
// that field untouched (nobody manually recomputes a sha256), so trusting it
// would make the edit compare equal to the stale baseline and get silently
// overwritten — exactly what this check exists to catch.
func TestFlushState_HandEditWithStaleHashFieldIsDetected(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.yaml")
	fm := newTestFileManager(t)

	s := newTestState(stateFile)
	s.MachineState.Profile = "before"
	m, err := NewStateManager(WithState(s), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, m.Refresh())
	require.NoError(t, m.Commit(ActionHistory{}, AllComponentIDs...))

	// Simulate a hand edit: change the content but leave the stored hash field
	// exactly as it was written.
	profileFile := componentFilePath(dir, ComponentProfile)
	raw, err := os.ReadFile(profileFile)
	require.NoError(t, err)
	edited := bytes.Replace(raw, []byte("profile: before"), []byte("profile: hand-edited"), 1)
	require.NotEqual(t, string(raw), string(edited), "precondition: the replacement must have matched something")
	require.NoError(t, os.WriteFile(profileFile, edited, 0o644))

	changed := m.State()
	changed.MachineState.Profile = "after"
	err = m.Set(changed).Commit(ActionHistory{}, AllComponentIDs...)
	require.Error(t, err, "a hand edit with an untouched hash field must still be detected")
	require.Contains(t, err.Error(), "changed externally on disk")
	entries := decodeHistoryFile(t, ActionHistoryFilePath(dir))
	refused := entries[len(entries)-1]
	require.True(t, refused.NotPersisted, "a commit whose files did not land is still recorded, marked")
	require.Contains(t, refused.Error, "changed externally on disk")
	require.Empty(t, refused.Components)

	reread, err := NewStateManager(WithState(newTestState(stateFile)), WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, reread.Refresh())
	require.Equal(t, "hand-edited", reread.State().MachineState.Profile,
		"the hand edit must survive — it must not be silently overwritten")
}

// An agent marked unobserved by a refresh is never written to its file, nor
// part of the content hash: the flag says what one run could read, not what the
// host holds.
func TestFlushState_UnobservedAgentIsNeverWrittenOrHashed(t *testing.T) {
	dir := t.TempDir()
	st := newTestState(filepath.Join(dir, "state.yaml"))
	st.TeleportState.ClusterAgent = TeleportClusterAgentState{Installed: true, ChartVersion: "18.6.4"}
	read := st
	st.TeleportState.ClusterAgent.Unobserved = true
	st.TeleportState.NodeAgent.Unobserved = true
	m, err := NewStateManager(WithState(st), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)

	require.NoError(t, m.Commit(ActionHistory{}, AllComponentIDs...))

	b, err := os.ReadFile(ComponentFilePath(dir, ComponentTeleport))
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(b)), "unobserved:")
	unread, err := stateContentHash(st)
	require.NoError(t, err)
	readHash, err := stateContentHash(read)
	require.NoError(t, err)
	require.Equal(t, readHash, unread)
}

// TestCommit_LandedStateWithALostEntryIsItsOwnError: when the files land but the
// entry can't be appended, Commit says exactly that, so a caller can tell a
// saved state from one that never reached disk. A re-run records its entry with
// no components: the files already hold its content.
func TestCommit_LandedStateWithALostEntryIsItsOwnError(t *testing.T) {
	dir := t.TempDir()
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails

	err = m.Commit(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}}, ComponentBlockNode)

	require.True(t, errorx.IsOfType(err, EntryNotAppendedError), "got %v", err)
	require.FileExists(t, componentFilePath(dir, ComponentBlockNode), "the state landed")

	require.NoError(t, os.Remove(ActionHistoryFilePath(dir)))
	fresh, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, fresh.Refresh())
	require.NoError(t, fresh.Commit(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}}, ComponentBlockNode))
	entries := decodeHistoryFile(t, ActionHistoryFilePath(dir))
	require.Len(t, entries, 1)
	require.Empty(t, entries[0].Components, "the accepted limit: the re-run can't name what the lost entry changed")
}

// TestRecord_UnsyncedEntryOfAFailedActionIsAWarning: with a cause, an entry
// appended but not synced is a warning that says so, and Record returns nil.
func TestRecord_UnsyncedEntryOfAFailedActionIsAWarning(t *testing.T) {
	emptyWarnings(t)
	dir := t.TempDir()
	fm := &unsyncedAppender{Manager: newTestFileManager(t), err: fsx.FileSyncError.New("disk gone")}
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(fm))
	require.NoError(t, err)

	require.NoError(t, m.Record(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetCluster}}, actionFailure()))

	require.Len(t, decodeHistoryFile(t, ActionHistoryFilePath(dir)), 1)
	warnings := TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "action history entry was appended but not synced to disk")
}

// TestCommit_LostEntryOfAStateThatDidNotLandIsAWarning: when a file does not
// land and the entry can't be appended either, Commit returns the write error
// and the lost entry is a state warning.
func TestCommit_LostEntryOfAStateThatDidNotLandIsAWarning(t *testing.T) {
	emptyWarnings(t)
	dir := t.TempDir()
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(componentFilePath(dir, ComponentBlockNode), 0o755)) // the component file can't be read or replaced
	require.NoError(t, os.Mkdir(ActionHistoryFilePath(dir), 0o755))                 // appending to a directory fails

	err = m.Commit(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}}, ComponentBlockNode)

	require.Error(t, err)
	require.NotContains(t, err.Error(), "action history")
	requireLostEntryWarning(t, dir)
}

// TestCommit_FailedActionWithAnUnsyncedWriteIsAWarning: a failed action whose
// state landed but was not synced returns nil, so the command reports the
// action's own failure, and the unsynced write is a warning.
func TestCommit_FailedActionWithAnUnsyncedWriteIsAWarning(t *testing.T) {
	emptyWarnings(t)
	dir := t.TempDir()
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	recordDirSyncs(t, fsx.FileSyncError.New("disk gone"))

	err = m.Commit(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}, Outcome: OutcomeFailed}, ComponentBlockNode)

	require.NoError(t, err)
	entries := decodeHistoryFile(t, ActionHistoryFilePath(dir))
	require.Len(t, entries, 1)
	require.False(t, entries[0].NotPersisted)
	requireWarning(t, "The state was written but not synced to disk", "Check the disk holding "+dir+".")
}

// TestCommit_UnsyncedWriteAndALostEntry: when the state lands unsynced and its
// entry is lost, Commit returns EntryNotAppendedError and the unsynced write is
// a warning, so the operator learns both.
func TestCommit_UnsyncedWriteAndALostEntry(t *testing.T) {
	emptyWarnings(t)
	dir := t.TempDir()
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails
	recordDirSyncs(t, fsx.FileSyncError.New("disk gone"))

	err = m.Commit(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}}, ComponentBlockNode)

	require.True(t, errorx.IsOfType(err, EntryNotAppendedError), "got %v", err)
	requireWarning(t, "The state was written but not synced to disk")
}

// failingAppender is an fsx.Manager whose first AppendToFile fails and writes
// nothing.
type failingAppender struct {
	fsx.Manager
	failed bool
}

func (a *failingAppender) AppendToFile(path string, payload []byte) error {
	if !a.failed {
		a.failed = true
		return errors.New("disk full")
	}
	return a.Manager.AppendToFile(path, payload)
}

// TestFlushActionHistory_AFailedAppendLosesItsEntries: an append takes the
// queued entries, so a failed one is not appended again by the next.
func TestFlushActionHistory_AFailedAppendLosesItsEntries(t *testing.T) {
	dir := t.TempDir()
	m, err := newStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(&failingAppender{Manager: newTestFileManager(t)}))
	require.NoError(t, err)

	require.Error(t, m.Record(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}}, nil))
	require.NoError(t, m.Record(ActionHistory{Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetBlockNode}}, nil))

	entries := decodeHistoryFile(t, ActionHistoryFilePath(dir))
	require.Len(t, entries, 1)
	require.Equal(t, models.ActionUninstall, entries[0].Intent.Action)
}
