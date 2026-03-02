// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"sync"

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

// DefaultStateManager defines the interface for managing the application state with IO operations.
// It is just a thin wrapper around State with added thread-safe disk persistence & refresh operations.
// However, the State itself is not thread-safe for mutations since State is a data model.
// It composes Reader, Writer and Persister so existing callers need no changes.
type DefaultStateManager interface {
	Reader
	Writer
	Persister
}

// DefaultStateManager encapsulates a State and all IO operations (flush/refresh).
type stateManager struct {
	mu      sync.Mutex
	state   State
	actions []ActionHistory
	fm      fsx.Manager
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

// NewStateManager creates a DefaultStateManager with the provided options.
// Caller must call Refresh() to load the persisted state from disk before accessing the state.
func NewStateManager(opts ...ManagerOption) (DefaultStateManager, error) {
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

// Flush persists the current state to disk.
// It captures the in-memory state and pending actions under the lock, then
// performs all I/O outside the lock so that State() and Set() are not blocked
// during the (potentially slow) disk write.
func (m *stateManager) Flush() error {
	// Capture state and pending actions under lock, then release before I/O.
	m.mu.Lock()
	m.state.LastSync = htime.Now()
	snapshot := m.state
	pendingActions := make([]ActionHistory, len(m.actions))
	copy(pendingActions, m.actions)
	m.actions = []ActionHistory{} // clear in-memory buffer while still holding the lock
	m.mu.Unlock()

	b, err := yaml.Marshal(snapshot)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal state to YAML")
	}

	err = m.fm.WriteFile(snapshot.StateFile, b)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to write state file to %s", snapshot.StateFile)
	}

	// Append each pending action history entry as its own YAML document so that we
	// never need to load the full history file into memory.  Each entry is prefixed
	// with the standard YAML document-start marker ("---") which makes the file a
	// valid stream of independent YAML documents that can be decoded incrementally.
	actionHistoryFile := filepath.Join(filepath.Dir(snapshot.StateFile), "action_history.yaml")
	for _, entry := range pendingActions {
		entryBytes, marshalErr := yaml.Marshal(entry)
		if marshalErr != nil {
			return errorx.InternalError.Wrap(marshalErr, "failed to marshal action history entry to YAML")
		}

		// Prepend the YAML document-start marker so readers can split on "---".
		doc := append([]byte("---\n"), entryBytes...)
		if appendErr := m.fm.AppendToFile(actionHistoryFile, doc); appendErr != nil {
			return errorx.InternalError.Wrap(appendErr, "failed to append action history entry to %s", actionHistoryFile)
		}
	}

	return nil
}

// Refresh reloads the persisted state from disk with write lock
func (m *stateManager) Refresh() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	b, err := m.fm.ReadFile(m.state.StateFile, -1)
	if err != nil {
		if errorx.IsOfType(err, fsx.FileNotFound) {
			return NotFoundError.Wrap(err, "state file does not exist at %s", m.state.StateFile)
		}

		return errorx.InternalError.Wrap(err, "failed to read state file from %s", m.state.StateFile)
	}

	newState := NewState() // create an instance of State to unmarshal into without changing the original
	if err = yaml.Unmarshal(b, &newState); err != nil {
		return errorx.InternalError.Wrap(err, "failed to unmarshal state from YAML")
	}

	m.state = newState

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
