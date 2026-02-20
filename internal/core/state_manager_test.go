// SPDX-License-Identifier: Apache-2.0

package core

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestNewStateManager_Default ensures a manager can be created with defaults.
func TestNewStateManager_Default(t *testing.T) {
	m, err := NewStateManager()
	if err != nil {
		t.Fatalf("NewStateManager returned error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil StateManager")
	}
	if m.State() == nil {
		t.Fatal("expected State() to return non-nil state")
	}
}

// TestFlushWritesFile checks that Flush writes the YAML representation to disk.
func TestFlushWritesFile(t *testing.T) {
	tmp := filepath.Join(os.TempDir(), "sw_state_flush_test.yaml")
	t.Cleanup(func() { _ = os.Remove(tmp) })

	s := NewState()
	s.File = tmp
	s.Version = "v1-test-flush"

	m, err := NewStateManager(WithState(s))
	if err != nil {
		t.Fatalf("NewStateManager returned error: %v", err)
	}

	if err := m.Flush(); err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}

	// Verify file exists and contains YAML with expected Version
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
	tmp := filepath.Join(os.TempDir(), "sw_state_refresh_test.yaml")
	t.Cleanup(func() { _ = os.Remove(tmp) })

	// create a state on disk
	onDisk := NewState()
	onDisk.File = tmp
	onDisk.Version = "v2-test-refresh"

	b, err := yaml.Marshal(onDisk)
	if err != nil {
		t.Fatalf("failed to marshal on-disk state: %v", err)
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		t.Fatalf("failed to write on-disk state file: %v", err)
	}

	// Create a manager with a different in-memory state, pointing to same file
	mem := NewState()
	mem.File = tmp
	mem.Version = "before-refresh"

	m, err := NewStateManager(WithState(mem))
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

// TestHasPersistedState verifies HasPersistedState reports the presence of the state file.
func TestHasPersistedState(t *testing.T) {
	tmp := filepath.Join(os.TempDir(), "sw_state_exists_test.yaml")
	t.Cleanup(func() { _ = os.Remove(tmp) })

	// write a file
	if err := os.WriteFile(tmp, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	s := NewState()
	s.File = tmp

	m, err := NewStateManager(WithState(s))
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
