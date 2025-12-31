package core

import (
	"os"
	"sync"

	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
	htime "helm.sh/helm/v3/pkg/time"
)

// StateManager defines the interface for managing the application state with IO operations.
// It is just a thin wrapper around State with added thread-safe disk persistence & refresh operations.
// However, the State itself is not thread-safe for mutations since State is a data model.
type StateManager interface {
	State() *State
	FileManager() fsx.Manager
	Flush() error
	Refresh() error
	HasPersistedState() (os.FileInfo, bool, error)
}

// StateManager encapsulates a State and all IO operations (flush/refresh).
type stateManager struct {
	mu    sync.Mutex
	state *State
	fm    fsx.Manager
}

type StateManagerOption func(*stateManager) error

func WithFileManager(fm fsx.Manager) StateManagerOption {
	return func(m *stateManager) error {
		if fm == nil {
			return errorx.IllegalArgument.New("file manager cannot be nil")
		}
		m.fm = fm
		return nil
	}
}

func WithState(s *State) StateManagerOption {
	return func(m *stateManager) error {
		if s == nil {
			return errorx.IllegalArgument.New("state cannot be nil")
		}
		m.state = s
		return nil
	}
}

// NewStateManager creates a StateManager with the provided options.
func NewStateManager(opts ...StateManagerOption) (StateManager, error) {
	m := &stateManager{}

	for _, opt := range opts {
		if err := opt(m); err != nil {
			return nil, err
		}
	}

	if m.state == nil {
		m.state = NewState()
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

// State returns the current state (thread-safe for read operations)
// It returns a pointer to the state, allowing callers to be able to mutat it directly and invoke Flush to persist changes.
// This is because cloning the state for every read would be inefficient.
// Callers should ensure proper synchronization when mutating the state.
// For read-only operations, callers can use the returned pointer without additional locking.
// The disk persistence & refresh operations (Flush/Refresh) are thread-safe, however mutations to the state itself are not synchronized.
func (m *stateManager) State() *State {
	return m.state
}

// FileManager returns the file manager used by the state manager
func (m *stateManager) FileManager() fsx.Manager {
	return m.fm
}

// Flush persists the current state to disk with write lock
func (m *stateManager) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.LastSync = htime.Now()

	b, err := yaml.Marshal(m.state)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal state to YAML")
	}

	err = m.fm.WriteFile(m.state.File, b)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to write state file to %s", m.state.File)
	}

	return nil
}

// Refresh reloads the persisted state from disk with write lock
func (m *stateManager) Refresh() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	b, err := m.fm.ReadFile(m.state.File, -1)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to read state file from %s", m.state.File)
	}

	newState := NewState()
	if err = yaml.Unmarshal(b, newState); err != nil {
		return errorx.InternalError.Wrap(err, "failed to unmarshal state from YAML")
	}

	// copy fields while preserving the mutex
	m.state.Version = newState.Version
	m.state.Commit = newState.Commit
	m.state.File = newState.File
	m.state.Paths = newState.Paths
	m.state.Machine = newState.Machine
	m.state.Cluster = newState.Cluster
	m.state.BlockNode = newState.BlockNode
	m.state.LastSync = newState.LastSync

	return nil
}

// HasPersistedState checks if the state file exists on disk
func (m *stateManager) HasPersistedState() (os.FileInfo, bool, error) {
	return m.fm.PathExists(m.state.File)
}
