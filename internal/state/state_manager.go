// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
	htime "helm.sh/helm/v3/pkg/time"
)

// Reader is the read-only view of managed application state.
// Consumers that only need to inspect state (e.g. resolvers, reality checkers)
// should depend on this narrow interface rather than the full DefaultStateManager.
type Reader interface {
	// State returns a snapshot of the current in-memory state.
	State() State
	// HasPersistedState reports whether a state file already exists on disk.
	HasPersistedState() (os.FileInfo, bool, error)
}

// Writer is the mutation-only view of managed application state.
// Consumers that record side-effects (e.g. the BLL after a workflow run) should
// depend on this narrow interface so that their dependencies are explicit.
//
// Set and AddActionHistory return Writer (not DefaultStateManager) so that
// callers depending only on Writer can chain calls without importing the full
// DefaultStateManager type.
type Writer interface {
	// Set replaces the entire in-memory state and returns the Writer for chaining.
	Set(s State) Writer
	// AddActionHistory appends an entry to the pending action log and updates
	// State.LastAction. Entries are flushed to disk on the next Flush() call.
	// Returns the Writer for chaining.
	AddActionHistory(entry ActionHistory) Writer
	// Flush persists the current state and any pending action history to disk.
	Flush() error
}

// Persister groups lifecycle operations (load + save) that are needed at the
// composition root (cmd layer) but not inside domain logic.
type Persister interface {
	// Refresh reloads the persisted state from disk, overwriting in-memory state.
	Refresh() error
	// FileManager returns the underlying file-system abstraction.
	FileManager() fsx.Manager
}

// Manager defines the interface for managing the application state with IO operations.
// It is just a thin wrapper around State with added thread-safe disk persistence & refresh operations.
// However, the State itself is not thread-safe for mutations since State is a data model.
// It composes Reader, Writer and Persister so existing callers need no changes.
type Manager interface {
	Reader
	Writer
	Persister
}

// DefaultStateManager encapsulates a State and all IO operations (flush/refresh).
type stateManager struct {
	mu            sync.Mutex
	flushMu       sync.Mutex // serializes Flush() calls
	state         State
	actions       []ActionHistory
	fm            fsx.Manager
	lastStateHash string // canonical hash of state as last read from / written to disk
}

type ManagerOption func(*stateManager) error

func WithFileManager(fm fsx.Manager) ManagerOption {
	return func(m *stateManager) error {
		if fm == nil {
			return errorx.IllegalArgument.New("file manager cannot be nil")
		}
		m.fm = fm
		return nil
	}
}

func WithState(s State) ManagerOption {
	return func(m *stateManager) error {
		m.state = s
		return nil
	}
}

// NewStateManager creates a Manager with the provided options.
// Caller must call Refresh() to load the persisted state from disk before accessing the state.
func NewStateManager(opts ...ManagerOption) (Manager, error) {
	m := &stateManager{
		state:   NewState(),
		actions: []ActionHistory{},
	}

	for _, opt := range opts {
		if err := opt(m); err != nil {
			return nil, err
		}
	}

	if m.fm == nil {
		fm, err := fsx.NewManager()
		if err != nil {
			return nil, errorx.InternalError.Wrap(err, "failed to create file manager for state manager")
		}
		m.fm = fm
	}

	return m, nil
}

// State returns a copy of the current in-memory state (thread-safe).
// Returns a value copy so callers cannot mutate the manager's internals
// through the returned value.
func (m *stateManager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Set replaces the entire in-memory state (thread-safe) and returns the Writer
// for chaining.
func (m *stateManager) Set(s State) Writer {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
	return m
}

// FileManager returns the file manager used by the state manager
func (m *stateManager) FileManager() fsx.Manager {
	return m.fm
}

// Refresh reloads the persisted state from disk with write lock
func (m *stateManager) Refresh() error {
	// Prevent Refresh from interleaving with an in-progress Flush.
	m.flushMu.Lock()
	defer m.flushMu.Unlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	b, err := m.fm.ReadFile(m.state.StateFile, -1)
	if err != nil {
		if errorx.IsOfType(err, fsx.FileNotFound) {
			m.lastStateHash = "" // no file on disk
			return NotFoundError.Wrap(err, "state file does not exist at %s", m.state.StateFile)
		}
		return errorx.InternalError.Wrap(err, "failed to read state file from %s", m.state.StateFile)
	}

	newState := NewState()
	if err = yaml.Unmarshal(b, &newState); err != nil {
		return errorx.InternalError.Wrap(err, "failed to unmarshal state from YAML")
	}

	// Use the stored hash as the baseline if available (written by Flush).
	// Fall back to recomputing for hand-edited or legacy files without a hash.
	if newState.Hash != "" {
		m.lastStateHash = newState.Hash
	} else {
		noHash := newState
		noHash.Hash = ""
		noHash.HashAlgo = ""
		noHash.LastSync = htime.Time{}
		canonical, err := canonicalJSON(noHash)
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to canonicalize refreshed state for baseline hash")
		}
		sum := sha256.Sum256(canonical)
		m.lastStateHash = hex.EncodeToString(sum[:])
	}

	m.state = newState
	return nil
}

// Flush persists the current state to disk with canonical hashing and atomic write.
func (m *stateManager) Flush() error {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()

	// Capture state and pending actions under lock, then release before I/O.
	m.mu.Lock()
	snapshot := m.state
	pendingActions := make([]ActionHistory, len(m.actions))
	copy(pendingActions, m.actions)
	lastStateHash := m.lastStateHash
	m.mu.Unlock()

	logx.As().Debug().Any("snapshot", snapshot).Msg("Flushing state to disk")

	// Prepare state for hashing: zero metadata fields that should not affect the digest.
	noHash := snapshot
	noHash.Hash = ""
	noHash.HashAlgo = ""
	noHash.LastSync = htime.Time{}

	// Compute deterministic canonical JSON and hash it.
	canonical, err := canonicalJSON(noHash)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to create canonical representation of state for hashing")
	}
	sum := sha256.Sum256(canonical)
	newHashHex := hex.EncodeToString(sum[:])

	// Attach computed hash and real LastSync to the copy we will write.
	toWrite := snapshot
	toWrite.Hash = newHashHex
	toWrite.HashAlgo = "sha256"
	now := htime.Now()
	toWrite.LastSync = now

	// Marshal YAML to write to disk.
	b, err := yaml.Marshal(toWrite)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal state to YAML")
	}

	// Optimistic concurrency: compare on-disk state against the BASELINE (what we
	// last read/wrote), not against the new state. This detects external changes
	// without false-positives after Refresh() + Set() cycles.
	if _, exists, err := m.fm.PathExists(snapshot.StateFile); err != nil {
		return errorx.InternalError.Wrap(err, "failed to stat state file before flush")
	} else if exists {
		if lastStateHash == "" {
			return errorx.IllegalState.New("cannot flush without a baseline; call Refresh() first")
		}

		existing, err := m.fm.ReadFile(snapshot.StateFile, -1)
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to read state file before flush")
		}

		var existingState State
		if err := yaml.Unmarshal(existing, &existingState); err != nil {
			return errorx.IllegalState.New("state file at %s is not parseable YAML; aborting flush to avoid overwrite", snapshot.StateFile)
		}

		var diskHash string
		if existingState.Hash != "" {
			diskHash = existingState.Hash
		} else {
			// Fallback for hand-edited or legacy files without a hash.
			existingState.Hash = ""
			existingState.HashAlgo = ""
			existingState.LastSync = htime.Time{}
			canonicalExisting, err := canonicalJSON(existingState)
			if err != nil {
				return errorx.InternalError.Wrap(err, "failed to canonicalize existing state on disk")
			}
			sumExisting := sha256.Sum256(canonicalExisting)
			diskHash = hex.EncodeToString(sumExisting[:])
		}

		// Compare on-disk hash against baseline, NOT against the new hash.
		if diskHash != lastStateHash {
			return errorx.IllegalState.New(
				"state file changed externally on disk at %s (expected baseline %s, found %s); aborting flush to avoid overwrite",
				snapshot.StateFile, lastStateHash, diskHash,
			)
		}
	}

	// Append action history entries.
	// Do this before writing the state file to ensure history is preserved even if the state file flush fails (since pending actions are cleared on successful flush).
	actionHistoryFile := filepath.Join(filepath.Dir(snapshot.StateFile), "action_history.yaml")
	for _, entry := range pendingActions {
		entryBytes, marshalErr := yaml.Marshal(entry)
		if marshalErr != nil {
			return errorx.InternalError.Wrap(marshalErr, "failed to marshal action history entry to YAML")
		}
		doc := append([]byte("---\n"), entryBytes...)
		if appendErr := m.fm.AppendToFile(actionHistoryFile, doc); appendErr != nil {
			return errorx.InternalError.Wrap(appendErr, "failed to append action history entry to %s", actionHistoryFile)
		}
	}

	// Atomic write: write to temp file in same directory and rename.
	if err := writeAtomicFile(snapshot.StateFile, b); err != nil {
		return errorx.InternalError.Wrap(err, "failed to write state file to %s", snapshot.StateFile)
	}

	// Update in-memory state, clear pending actions, and advance the baseline hash.
	m.mu.Lock()
	m.actions = []ActionHistory{}
	m.state = toWrite
	m.lastStateHash = newHashHex // the new state is now the on-disk baseline
	m.mu.Unlock()

	return nil
}

// canonicalJSON returns a deterministic JSON encoding of v where object keys are sorted.
// It marshals v to JSON, decodes with UseNumber to preserve numeric fidelity, then re-encodes canonically.
func canonicalJSON(v interface{}) ([]byte, error) {
	// Marshal typed value to JSON bytes.
	j, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	// Decode into interface{} using UseNumber so numbers become json.Number (stable textual form).
	var iface interface{}
	dec := json.NewDecoder(bytes.NewReader(j))
	dec.UseNumber()
	if err := dec.Decode(&iface); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := encodeCanonical(&buf, iface); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeCanonical writes a canonical JSON encoding of iface to buf with map keys sorted.
// Supports json.Number to preserve integer/float textual representation.
func encodeCanonical(buf *bytes.Buffer, iface interface{}) error {
	switch val := iface.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		// json.Number already preserves the original textual form.
		buf.WriteString(val.String())
	case float64:
		// Fallback if float64 reached (should be rare with UseNumber).
		b, _ := json.Marshal(val)
		buf.Write(b)
	case string:
		b, _ := json.Marshal(val)
		buf.Write(b)
	case []interface{}:
		buf.WriteByte('[')
		for i, elem := range val {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encodeCanonical(buf, elem); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]interface{}:
		// sort keys for deterministic output
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			buf.Write(kb)
			buf.WriteByte(':')
			if err := encodeCanonical(buf, val[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		// Fallback: marshal using encoding/json
		b, err := json.Marshal(val)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}

func writeAtomicFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".state.tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		tmp.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	success = true
	return nil
}

// HasPersistedState checks if the state file exists on disk
func (m *stateManager) HasPersistedState() (os.FileInfo, bool, error) {
	m.mu.Lock()
	stateFile := m.state.StateFile
	m.mu.Unlock()
	return m.fm.PathExists(stateFile)
}

// AddActionHistory adds an entry to the in-memory action history and updates the last action in the state.
// The action history is flushed to disk as part of the Flush() operation, and is stored in a separate history file to
// avoid unbounded growth of the main state file.
// The timestamp of the entry is set to the current time when adding to history to ensure consistency.
func (m *stateManager) AddActionHistory(entry ActionHistory) Writer {
	entry.Timestamp = htime.Now() // force the timestamp to be set to the current time when adding to history to ensure consistency
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, entry)
	m.state.LastAction = entry
	return m
}
