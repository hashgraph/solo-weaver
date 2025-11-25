package config

import (
	"os"
	"sync"
	"time"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/pkg/fsx"
	"gopkg.in/yaml.v3"
)

// StateManager encapsulates a State and all IO operations (flush/refresh).
type StateManager struct {
	mu    sync.Mutex
	state *State
	fm    fsx.Manager
}

type StateManagerOption func(*StateManager) error

func WithFileManager(fm fsx.Manager) StateManagerOption {
	return func(m *StateManager) error {
		if fm == nil {
			return errorx.IllegalArgument.New("file manager cannot be nil")
		}
		m.fm = fm
		return nil
	}
}

func WithState(s *State) StateManagerOption {
	return func(m *StateManager) error {
		if s == nil {
			return errorx.IllegalArgument.New("state cannot be nil")
		}
		m.state = s
		return nil
	}
}

// NewStateManager creates a StateManager with the provided options.
func NewStateManager(opts ...StateManagerOption) (*StateManager, error) {
	m := &StateManager{}

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
func (m *StateManager) State() *State {
	return m.state
}

// FileManager returns the file manager used by the state manager
func (m *StateManager) FileManager() fsx.Manager {
	return m.fm
}

// Flush persists the current state to disk with write lock
func (m *StateManager) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.LastSync = timePtr(time.Now())

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
func (m *StateManager) Refresh() error {
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
func (m *StateManager) HasPersistedState() (os.FileInfo, bool, error) {
	return m.fm.PathExists(m.state.File)
}
