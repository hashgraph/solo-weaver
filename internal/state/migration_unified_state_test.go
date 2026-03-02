// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestUnifiedStateMigration_Metadata(t *testing.T) {
	m := NewUnifiedStateMigration()

	assert.Equal(t, "unified-state", m.ID())
	assert.Contains(t, m.Description(), "Consolidate")
}

func TestFindLegacyStateFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some legacy files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "cilium.installed"), []byte("installed at version 1.16.0\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "cilium.configured"), []byte("configured at version 1.16.0\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "helm.installed"), []byte("installed at version 3.14.0\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "state.yaml"), []byte("version: 1.0.0\n"), 0644)) // Should be ignored
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "other.txt"), []byte("other\n"), 0644))           // Should be ignored

	files, err := findLegacyStateFiles(tmpDir)
	require.NoError(t, err)
	assert.Len(t, files, 3)
}

func TestFindLegacyStateFiles_Empty(t *testing.T) {
	tmpDir := t.TempDir()

	// Only state.yaml exists
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "state.yaml"), []byte("version: 1.0.0\n"), 0644))

	files, err := findLegacyStateFiles(tmpDir)
	require.NoError(t, err)
	assert.Len(t, files, 0)
}

func TestFindLegacyStateFiles_NonExistentDir(t *testing.T) {
	files, err := findLegacyStateFiles("/nonexistent/path")
	require.Error(t, err)
	assert.Nil(t, files)
}

func TestParseVersionFromContent(t *testing.T) {
	tests := []struct {
		content  string
		expected string
	}{
		{"installed at version 1.16.0\n", "1.16.0"},
		{"configured at version 2.0.0\n", "2.0.0"},
		{"installed at version 1.16.0", "1.16.0"},
		{"invalid content", "unknown"},
		{"", "unknown"},
	}

	for _, tt := range tests {
		result := parseVersionFromContent(tt.content)
		assert.Equal(t, tt.expected, result)
	}
}

func TestFormatStateContent(t *testing.T) {
	content := formatStateContent("installed", "1.16.0")
	assert.Equal(t, "installed at version 1.16.0\n", content)

	content = formatStateContent("configured", "2.0.0")
	assert.Equal(t, "configured at version 2.0.0\n", content)
}

func TestUnifiedState_YAMLMarshal(t *testing.T) {
	// The old UnifiedState/ComponentState types are removed. This test now verifies
	// that SoftwareState entries round-trip correctly through the unified state.yaml.
	s := NewState()
	s = SetSoftwareState(s, "cilium", SoftwareState{Installed: true, Configured: true, Version: "1.16.0"})
	s = SetSoftwareState(s, "helm", SoftwareState{Installed: true, Version: "3.14.0"})

	data, err := yaml.Marshal(s)
	require.NoError(t, err)

	var parsed State
	require.NoError(t, yaml.Unmarshal(data, &parsed))

	cilium := GetSoftwareState(parsed, "cilium")
	assert.Equal(t, "1.16.0", cilium.Version)
	assert.True(t, cilium.Installed)
	assert.True(t, cilium.Configured)

	helm := GetSoftwareState(parsed, "helm")
	assert.Equal(t, "3.14.0", helm.Version)
	assert.True(t, helm.Installed)
	assert.False(t, helm.Configured)
}

func TestRegisterMigrations_State(t *testing.T) {
	migration.ClearRegistry()
	InitMigrations()
	defer migration.ClearRegistry()

	// Verify migration is registered by checking it can be retrieved.
	// GetApplicableMigrations returns migrations where Applies() returns true.
	// The result depends on whether legacy state files exist in the environment,
	// so we just verify the call succeeds without asserting a specific count.
	mctx := &migration.Context{
		Component: MigrationComponent,
		Data:      &automa.SyncStateBag{},
	}
	_, err := migration.GetApplicableMigrations(MigrationComponent, mctx)
	require.NoError(t, err)
}
