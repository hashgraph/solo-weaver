// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bytes"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/security"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"gopkg.in/yaml.v3"
)

// newTestState returns a State with the StateFile pointing to tmp.
func newTestState(tmp string) State {
	s := NewState()
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
	pm.EXPECT().LookupUserByName(security.ServiceAccountUserName()).Return(mockUser, nil).AnyTimes()
	pm.EXPECT().LookupGroupByName(security.ServiceAccountGroupName()).Return(mockGroup, nil).AnyTimes()

	fm, err := fsx.NewManager(fsx.WithPrincipalManager(pm))
	if err != nil {
		t.Fatalf("failed to create test file manager: %v", err)
	}
	return fm
}

// TestFlushWritesFile checks that Flush writes the YAML representation to disk.
func TestFlushWritesFile(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "state.yaml")

	s := newTestState(tmp)
	s.Version = "v1-test-flush"

	m, err := NewStateManager(WithState(s), WithFileManager(newTestFileManager(t)))
	if err != nil {
		t.Fatalf("NewStateManager returned error: %v", err)
	}

	if err := m.Flush(); err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}

	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("failed to read persisted file: %v", err)
	}

	var loaded State
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal persisted YAML: %v", err)
	}

	if loaded.Version != "v1-test-flush" {
		t.Fatalf("unexpected Version in persisted state: got %q want %q", loaded.Version, "v1-test-flush")
	}
}

// TestRefreshLoadsFile checks that Refresh loads an on-disk state into the manager.
func TestRefreshLoadsFile(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "state.yaml")

	// create a state on disk
	onDisk := newTestState(tmp)
	onDisk.Version = "v2-test-refresh"

	b, err := yaml.Marshal(onDisk)
	if err != nil {
		t.Fatalf("failed to marshal on-disk state: %v", err)
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		t.Fatalf("failed to write on-disk state file: %v", err)
	}

	// Create a manager with a different in-memory state, pointing to the same file.
	mem := newTestState(tmp)
	mem.Version = "before-refresh"

	m, err := NewStateManager(WithState(mem), WithFileManager(newTestFileManager(t)))
	if err != nil {
		t.Fatalf("NewStateManager returned error: %v", err)
	}

	if err := m.Refresh(); err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}

	if got := m.State().Version; got != "v2-test-refresh" {
		t.Fatalf("Refresh did not update state Version: got %q want %q", got, "v2-test-refresh")
	}
}

// TestNewStateManager_DoesNotAutoRefresh verifies that NewStateManager does NOT
// automatically load state from disk during construction.
// Callers are expected to call Refresh() explicitly when they want to load persisted state.
func TestNewStateManager_DoesNotAutoRefresh(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "state.yaml")

	// Write a state file with a recognisable version to disk.
	onDisk := newTestState(tmp)
	onDisk.Version = "on-disk-version"
	b, err := yaml.Marshal(onDisk)
	if err != nil {
		t.Fatalf("failed to marshal on-disk state: %v", err)
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		t.Fatalf("failed to write on-disk state file: %v", err)
	}

	// Construct a manager with a different in-memory version and the same file path.
	mem := newTestState(tmp)
	mem.Version = "in-memory-version"

	m, err := NewStateManager(WithState(mem), WithFileManager(newTestFileManager(t)))
	if err != nil {
		t.Fatalf("NewStateManager returned error: %v", err)
	}

	// Construction must NOT have loaded the on-disk state — the in-memory version must be unchanged.
	if got := m.State().Version; got != "in-memory-version" {
		t.Fatalf("NewStateManager auto-refreshed state from disk: got version %q, want %q", got, "in-memory-version")
	}
}

// TestHasPersistedState verifies HasPersistedState reports the presence of the state file.
func TestHasPersistedState(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "state.yaml")

	// write a file
	if err := os.WriteFile(tmp, []byte("dummy"), 0o644); err != nil {
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

// TestActionHistory_SingleFlush verifies that a single Flush call writes all pending
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
		Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode},
		Inputs: map[string]any{"profile": "local", "version": "0.1.0"},
	})
	m.AddActionHistory(ActionHistory{
		Intent: models.Intent{Action: models.ActionUninstall, Target: models.TargetBlocknode},
		Inputs: map[string]any{"profile": "local"},
	})

	if err := m.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
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

// TestActionHistory_FlushClearsInMemoryBuffer verifies that after a Flush the in-memory
// action list is cleared so a second Flush with no new entries appends nothing.
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
		Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode},
		Inputs: map[string]any{"step": "first"},
	})

	if err := m.Flush(); err != nil {
		t.Fatalf("first Flush: %v", err)
	}

	// Second Flush with nothing new — file should still have exactly 1 entry.
	if err := m.Flush(); err != nil {
		t.Fatalf("second Flush: %v", err)
	}

	entries := decodeHistoryFile(t, historyFile)
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry after two flushes (second was empty), got %d", len(entries))
	}
}

// TestActionHistory_MultipleFlushes_Accumulates verifies that successive Flush calls
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
	if err := m.Flush(); err != nil {
		t.Fatalf("first Flush: %v", err)
	}

	sizeAfterFirst, err := os.Stat(historyFile)
	if err != nil {
		t.Fatalf("stat after first flush: %v", err)
	}

	// Second flush — 2 more entries
	m.AddActionHistory(ActionHistory{
		Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode},
		Inputs: map[string]any{"round": "2a"},
	})
	m.AddActionHistory(ActionHistory{
		Intent: models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlocknode},
		Inputs: map[string]any{"round": "2b"},
	})
	if err := m.Flush(); err != nil {
		t.Fatalf("second Flush: %v", err)
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

// TestActionHistory_LastActionUpdatedInState verifies that AddActionHistory also updates
// the LastAction field of the in-memory state so it can be flushed into state.yaml.
func TestActionHistory_LastActionUpdatedInState(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.yaml")

	s := newTestState(stateFile)
	m, err := NewStateManager(WithState(s), WithFileManager(newTestFileManager(t)))
	if err != nil {
		t.Fatalf("NewStateManager: %v", err)
	}

	m.AddActionHistory(ActionHistory{
		Intent: models.Intent{Action: models.ActionInstall, Target: models.TargetBlocknode},
		Inputs: map[string]any{"key": "value"},
	})

	if m.State().LastAction.Intent.Action != models.ActionInstall {
		t.Errorf("LastAction.Intent.Action: got %q, want %q",
			m.State().LastAction.Intent.Action, models.ActionInstall)
	}
	if m.State().LastAction.Timestamp.IsZero() {
		t.Error("LastAction.Timestamp is zero after AddActionHistory")
	}
}
