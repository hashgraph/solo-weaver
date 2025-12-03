// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/pkg/fsx"
)

func TestManager_RecordAndCheckInstallState(t *testing.T) {
	// Setup
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	manager := NewManager(fsxManager)
	softwareName := "test-software"
	version := "1.0.0"

	// Ensure clean state
	stateDir := core.Paths().StateDir
	_ = os.MkdirAll(stateDir, 0755)
	statePath := path.Join(stateDir, softwareName+".installed")
	_ = os.Remove(statePath)

	// Test: Initially state should not exist
	exists, err := manager.Exists(softwareName, TypeInstalled)
	require.NoError(t, err)
	require.False(t, exists)

	// Test: Record install state
	err = manager.RecordState(softwareName, TypeInstalled, version)
	require.NoError(t, err)

	// Test: State should now exist
	exists, err = manager.Exists(softwareName, TypeInstalled)
	require.NoError(t, err)
	require.True(t, exists)

	// Test: Remove install state
	err = manager.RemoveState(softwareName, TypeInstalled)
	require.NoError(t, err)

	// Test: State should no longer exist
	exists, err = manager.Exists(softwareName, TypeInstalled)
	require.NoError(t, err)
	require.False(t, exists)

	// Cleanup
	_ = os.Remove(statePath)
}

func TestManager_RecordAndCheckConfigureState(t *testing.T) {
	// Setup
	fsxManager, err := fsx.NewManager()
	require.NoError(t, err)

	manager := NewManager(fsxManager)
	softwareName := "test-software"
	version := "2.0.0"

	// Ensure clean state
	stateDir := core.Paths().StateDir
	_ = os.MkdirAll(stateDir, 0755)
	statePath := path.Join(stateDir, softwareName+".configured")
	_ = os.Remove(statePath)

	// Test: Initially state should not exist
	exists, err := manager.Exists(softwareName, TypeConfigured)
	require.NoError(t, err)
	require.False(t, exists)

	// Test: Record configure state
	err = manager.RecordState(softwareName, TypeConfigured, version)
	require.NoError(t, err)

	// Test: State should now exist
	exists, err = manager.Exists(softwareName, TypeConfigured)
	require.NoError(t, err)
	require.True(t, exists)

	// Test: Remove configure state
	err = manager.RemoveState(softwareName, TypeConfigured)
	require.NoError(t, err)

	// Test: State should no longer exist
	exists, err = manager.Exists(softwareName, TypeConfigured)
	require.NoError(t, err)
	require.False(t, exists)

	// Cleanup
	_ = os.Remove(statePath)
}
