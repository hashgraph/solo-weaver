// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"path"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/joomcode/errorx"
)

// Type represents the type of state being managed
type Type string

const (
	// TypeInstalled indicates software installation state
	TypeInstalled Type = "installed"
	// TypeConfigured indicates software configuration state
	TypeConfigured Type = "configured"
)

// Manager handles state persistence for software installation and configuration
type Manager struct {
	fileManager fsx.Manager
}

// NewManager creates a new state manager
func NewManager(fileManager fsx.Manager) *Manager {
	return &Manager{
		fileManager: fileManager,
	}
}

// getStatePath returns the path to the state file for a given software and state type
func (m *Manager) getStatePath(softwareName string, stateType Type) string {
	return path.Join(core.Paths().StateDir, fmt.Sprintf("%s.%s", softwareName, stateType))
}

// Exists checks if the state file exists for the given software and state type
func (m *Manager) Exists(softwareName string, stateType Type) (bool, error) {
	statePath := m.getStatePath(softwareName, stateType)
	_, exists, err := m.fileManager.PathExists(statePath)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// RecordState creates a state file for the given software and state type
func (m *Manager) RecordState(softwareName string, stateType Type, version string) error {
	// Ensure state directory exists
	stateDir := core.Paths().StateDir
	if err := m.fileManager.CreateDirectory(stateDir, true); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state directory")
	}

	statePath := m.getStatePath(softwareName, stateType)
	content := []byte(fmt.Sprintf("%s at version %s\n", stateType, version))

	err := m.fileManager.WriteFile(statePath, content)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state file for type %s", stateType)
	}

	return nil
}

// RemoveState removes the state file for the given software and state type
func (m *Manager) RemoveState(softwareName string, stateType Type) error {
	statePath := m.getStatePath(softwareName, stateType)
	return m.fileManager.RemoveAll(statePath)
}
