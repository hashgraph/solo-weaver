// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"sync"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
	htime "helm.sh/helm/v3/pkg/time"
)

// DefaultStateManager defines the interface for managing the application state with IO operations.
// It is just a thin wrapper around State with added thread-safe disk persistence & refresh operations.
// However, the State itself is not thread-safe for mutations since State is a data model.
type DefaultStateManager interface {
	State() State
	Set(s State) DefaultStateManager
	FileManager() fsx.Manager
	Flush() error
	Refresh() error
	HasPersistedState() (os.FileInfo, bool, error)
	AddIntent(intent models.Intent, inputs map[string]any) DefaultStateManager
}

// DefaultStateManager encapsulates a State and all IO operations (flush/refresh).
type stateManager struct {
	mu    sync.Mutex
	state State
	fm    fsx.Manager
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
		state: NewState(),
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

	// if state is not provided, create a new one and try to refresh from disk
	if m.state.LastSync.IsZero() {
		err := m.Refresh()
		if err != nil {
			if errorx.IsOfType(err, NotFoundError) {
				logx.As().Debug().Any("state_file", m.state.StateFile).
					Msg("No existing state file found, starting with a default state")
				return m, nil
			}

			logx.As().Warn().Err(err).Any("state", m.state).
				Msg("Failed to refresh state from disk, starting with a default state")
		}
	}

	return m, nil
}

// State returns the current state (thread-safe for read operations)
func (m *stateManager) State() State {
	return m.state
}

// Set sets the current state (thread-safe for write operations)
func (m *stateManager) Set(s State) DefaultStateManager {
	m.state = s
	return m
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

	err = m.fm.WriteFile(m.state.StateFile, b)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to write state file to %s", m.state.StateFile)
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
	return m.fm.PathExists(m.state.StateFile)
}

func (m *stateManager) AddIntent(intent models.Intent, inputs map[string]any) DefaultStateManager {
	entry := IntentHistory{
		Intent:    intent,
		Inputs:    inputs,
		Timestamp: htime.Now(),
	}
	m.state.IntentHistory = append(m.state.IntentHistory, entry)
	return m
}
