// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bytes"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
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
// machine component's file).
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

	if err := m.FlushState(); err != nil {
		t.Fatalf("FlushState returned error: %v", err)
	}

	data, err := os.ReadFile(componentFilePath(dir, ComponentMachine))
	if err != nil {
		t.Fatalf("failed to read persisted machine state file: %v", err)
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
	if err := seedMgr.FlushState(); err != nil {
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
	if err := seedMgr.FlushState(); err != nil {
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
	if err := os.WriteFile(componentFilePath(dir, ComponentMachine), []byte("dummy"), 0o644); err != nil {
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

// TestActionHistory_SingleFlush verifies that a single FlushState call writes all pending
// entries to the history file as individual YAML documents and clears the in-memory buffer.
func TestActionHistory_SingleFlush(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.yaml")
	historyFile := filepath.Join(dir, "action_history.yaml")

	s := newTestState(stateFile)
	m, err := NewStateManager(WithState(s), WithFileManager(newTestFileManager(t)))
	if err != nil {
		t.Fatalf("NewStateManager: %v", err)
	}

	m.AddActionHistory(ActionHistory{
		Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode},
		Inputs: map[string]any{"profile": "local", "version": "0.1.0"},
	})
	m.AddActionHistory(ActionHistory{
		Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetBlockNode},
		Inputs: map[string]any{"profile": "local"},
	})

	if err := m.FlushAll(); err != nil {
		t.Fatalf("FlushState: %v", err)
	}

	entries := decodeHistoryFile(t, historyFile)
	if len(entries) != 2 {
		t.Fatalf("expected 2 history entries after flush, got %d", len(entries))
	}

	if entries[0].Intent.Action != models.ActionInstall {
		t.Errorf("entry[0] action: got %q, want %q", entries[0].Intent.Action, models.ActionInstall)
	}

	// After YAML round-trip, Inputs is decoded as map[string]interface{}.
	inputs0, ok := entries[0].Inputs.(map[string]interface{})
	if !ok {
		t.Fatalf("entry[0] Inputs: expected map[string]interface{}, got %T", entries[0].Inputs)
	}
	if inputs0["version"] != "0.1.0" {
		t.Errorf("entry[0] version input: got %v, want 0.1.0", inputs0["version"])
	}
	if entries[1].Intent.Action != models.ActionUninstall {
		t.Errorf("entry[1] action: got %q, want %q", entries[1].Intent.Action, models.ActionUninstall)
	}

	// Timestamps must be set by AddActionHistory (non-zero)
	if entries[0].Timestamp.IsZero() {
		t.Error("entry[0] Timestamp is zero, expected it to be set by AddActionHistory")
	}
	if entries[1].Timestamp.IsZero() {
		t.Error("entry[1] Timestamp is zero, expected it to be set by AddActionHistory")
	}
}

// TestActionHistory_FlushClearsInMemoryBuffer verifies that after a FlushState the in-memory
// action list is cleared so a second FlushState with no new entries appends nothing.
func TestActionHistory_FlushClearsInMemoryBuffer(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.yaml")
	historyFile := filepath.Join(dir, "action_history.yaml")

	s := newTestState(stateFile)
	m, err := NewStateManager(WithState(s), WithFileManager(newTestFileManager(t)))
	if err != nil {
		t.Fatalf("NewStateManager: %v", err)
	}

	m.AddActionHistory(ActionHistory{
		Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode},
		Inputs: map[string]any{"step": "first"},
	})

	if err := m.FlushAll(); err != nil {
		t.Fatalf("first FlushState: %v", err)
	}

	// Second FlushState with nothing new — file should still have exactly 1 entry.
	if err := m.FlushAll(); err != nil {
		t.Fatalf("second FlushState: %v", err)
	}

	entries := decodeHistoryFile(t, historyFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry after two flushes (second was empty), got %d", len(entries))
	}
}

// TestActionHistory_MultipleFlushes_Accumulates verifies that successive FlushState calls
// accumulate entries incrementally in the history file without re-writing earlier entries.
func TestActionHistory_MultipleFlushes_Accumulates(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.yaml")
	historyFile := filepath.Join(dir, "action_history.yaml")

	s := newTestState(stateFile)
	m, err := NewStateManager(WithState(s), WithFileManager(newTestFileManager(t)))
	if err != nil {
		t.Fatalf("NewStateManager: %v", err)
	}

	// First flush — 1 entry
	m.AddActionHistory(ActionHistory{
		Intent: models.Intent{Action: models.ActionSetup, Target: models.TargetCluster},
		Inputs: map[string]any{"round": "1"},
	})
	if err := m.FlushAll(); err != nil {
		t.Fatalf("first FlushState: %v", err)
	}

	sizeAfterFirst, err := os.Stat(historyFile)
	if err != nil {
		t.Fatalf("stat after first flush: %v", err)
	}

	// Second flush — 2 more entries
	m.AddActionHistory(ActionHistory{
		Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode},
		Inputs: map[string]any{"round": "2a"},
	})
	m.AddActionHistory(ActionHistory{
		Intent: models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlockNode},
		Inputs: map[string]any{"round": "2b"},
	})
	if err := m.FlushAll(); err != nil {
		t.Fatalf("second FlushState: %v", err)
	}

	sizeAfterSecond, err := os.Stat(historyFile)
	if err != nil {
		t.Fatalf("stat after second flush: %v", err)
	}

	// File must have grown (entries were appended, not overwritten)
	if sizeAfterSecond.Size() <= sizeAfterFirst.Size() {
		t.Fatalf("history file did not grow: size after first=%d, after second=%d",
			sizeAfterFirst.Size(), sizeAfterSecond.Size())
	}

	entries := decodeHistoryFile(t, historyFile)
	if len(entries) != 3 {
		t.Fatalf("expected 3 accumulated history entries, got %d", len(entries))
	}

	wantActions := []models.ActionType{models.ActionSetup, models.ActionInstall, models.ActionUpgrade}
	for i, want := range wantActions {
		if entries[i].Intent.Action != want {
			t.Errorf("entry[%d] action: got %q, want %q", i, entries[i].Intent.Action, want)
		}
	}
}

// TestActionHistory_EntryRecordedWithProvenance verifies that AddActionHistory
// stamps the entry's timestamp and that FlushActionHistory persists it — with
// the components it wrote — to the append-only action_history.yaml, the single
// record of what changed the state after LastAction was dropped from State (#1242).
func TestActionHistory_EntryRecordedWithProvenance(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.yaml")

	s := newTestState(stateFile)
	m, err := NewStateManager(WithState(s), WithFileManager(newTestFileManager(t)))
	if err != nil {
		t.Fatalf("NewStateManager: %v", err)
	}

	m.AddActionHistory(ActionHistory{
		Intent:     models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode},
		Inputs:     map[string]any{"key": "value"},
		Components: []ComponentID{ComponentMachine, ComponentBlockNode},
	})
	if err := m.FlushActionHistory(); err != nil {
		t.Fatalf("FlushActionHistory: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "action_history.yaml"))
	if err != nil {
		t.Fatalf("read action_history.yaml: %v", err)
	}

	var entry ActionHistory
	if err := yaml.Unmarshal(b, &entry); err != nil {
		t.Fatalf("unmarshal action_history.yaml: %v", err)
	}
	if entry.Intent.Action != models.ActionInstall {
		t.Errorf("Intent.Action: got %q, want %q", entry.Intent.Action, models.ActionInstall)
	}
	if entry.Timestamp.IsZero() {
		t.Error("Timestamp is zero after AddActionHistory")
	}
	require.Equal(t, []ComponentID{ComponentMachine, ComponentBlockNode}, entry.Components,
		"action_history entry must record which components the action wrote")
}

// TestFlushState_DirectorySyncFailureAdvancesBaseline verifies that a flush whose directory
// sync fails still replaced the file, so the next flush doesn't see it as an external change.
func TestFlushState_DirectorySyncFailureAdvancesBaseline(t *testing.T) {
	m, err := NewStateManager(WithState(newTestState(filepath.Join(t.TempDir(), "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, m.FlushState())

	syncErr := fsx.FileSyncError.New("disk gone")
	recordDirSyncs(t, syncErr)
	s := m.State()
	s.MachineState.Profile = "local"
	require.ErrorIs(t, m.Set(s).FlushState(), syncErr)

	recordDirSyncs(t, nil)
	s.MachineState.Profile = "perfnet"
	require.NoError(t, m.Set(s).FlushState())
}

// TestFlushAll_DirectorySyncFailureStillAppendsHistory verifies that a state flush whose
// directory sync fails still records its action, since the new state is on disk.
func TestFlushAll_DirectorySyncFailureStillAppendsHistory(t *testing.T) {
	dir := t.TempDir()
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	m.AddActionHistory(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}})
	syncErr := fsx.FileSyncError.New("disk gone")
	recordDirSyncs(t, syncErr)

	require.ErrorIs(t, m.FlushAll(), syncErr)

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
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(fm))
	require.NoError(t, err)
	m.AddActionHistory(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}})

	require.ErrorIs(t, m.FlushActionHistory(), syncErr)
	require.NoError(t, m.FlushActionHistory())

	require.Len(t, decodeHistoryFile(t, filepath.Join(dir, "action_history.yaml")), 1)
}

// TestFlushActionHistory_NothingPendingWritesNothing verifies that a flush with no pending
// entries doesn't create the history file.
func TestFlushActionHistory_NothingPendingWritesNothing(t *testing.T) {
	dir := t.TempDir()
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)

	require.NoError(t, m.FlushActionHistory())

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
	m.AddActionHistory(ActionHistory{Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetBlockNode}})

	require.NoError(t, m.FlushActionHistory())

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
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, "state.yaml"))), WithFileManager(fm))
	require.NoError(t, err)
	m.AddActionHistory(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}})
	fm.add = func() {
		m.AddActionHistory(ActionHistory{Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetBlockNode}})
	}

	require.NoError(t, m.FlushActionHistory())
	require.NoError(t, m.FlushActionHistory())

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

	m.AddActionHistory(ActionHistory{
		Intent:     models.Intent{Action: models.ActionUninstall, Target: models.TargetTeleportCluster},
		OwnedDrift: drifted,
	})
	m.AddActionHistory(ActionHistory{Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetTeleportCluster}})
	require.NoError(t, m.FlushActionHistory())

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
	require.NoError(t, m.FlushState())

	// Simulate a hand edit: change the content but leave the stored hash field
	// exactly as it was written.
	machineFile := componentFilePath(dir, ComponentMachine)
	raw, err := os.ReadFile(machineFile)
	require.NoError(t, err)
	edited := bytes.Replace(raw, []byte("profile: before"), []byte("profile: hand-edited"), 1)
	require.NotEqual(t, string(raw), string(edited), "precondition: the replacement must have matched something")
	require.NoError(t, os.WriteFile(machineFile, edited, 0o644))

	changed := m.State()
	changed.MachineState.Profile = "after"
	err = m.Set(changed).FlushState()
	require.Error(t, err, "a hand edit with an untouched hash field must still be detected")
	require.Contains(t, err.Error(), "changed externally on disk")

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

	require.NoError(t, m.FlushState())

	b, err := os.ReadFile(ComponentFilePath(dir, ComponentTeleport))
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(b)), "unobserved:")
	unread, err := stateContentHash(st)
	require.NoError(t, err)
	readHash, err := stateContentHash(read)
	require.NoError(t, err)
	require.Equal(t, readHash, unread)
}
