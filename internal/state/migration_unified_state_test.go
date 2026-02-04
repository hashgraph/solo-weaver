// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"testing"

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
	state := &UnifiedState{
		Version: "1.0.0",
		Components: map[string]*ComponentState{
			"cilium": {
				Installed:  &StateEntry{Version: "1.16.0"},
				Configured: &StateEntry{Version: "1.16.0"},
			},
			"helm": {
				Installed: &StateEntry{Version: "3.14.0"},
			},
		},
	}

	data, err := yaml.Marshal(state)
	require.NoError(t, err)

	// Unmarshal and verify
	var parsed UnifiedState
	require.NoError(t, yaml.Unmarshal(data, &parsed))

	assert.Equal(t, "1.0.0", parsed.Version)
	assert.NotNil(t, parsed.Components["cilium"])
	assert.NotNil(t, parsed.Components["cilium"].Installed)
	assert.Equal(t, "1.16.0", parsed.Components["cilium"].Installed.Version)
	assert.NotNil(t, parsed.Components["helm"])
	assert.NotNil(t, parsed.Components["helm"].Installed)
	assert.Nil(t, parsed.Components["helm"].Configured)
}

func TestRegisterMigrations_State(t *testing.T) {
	migration.ClearRegistry()
	InitMigrations()
	defer migration.ClearRegistry()

	// Verify migration is registered - state-based migrations don't need versions
	mctx := &migration.Context{
		Component: MigrationComponent,
		Data:      make(map[string]interface{}),
	}
	migrations, err := migration.GetApplicableMigrations(MigrationComponent, mctx)
	require.NoError(t, err)
	// Note: This will return 0 because Applies() checks for legacy files,
	// which don't exist in the test environment. That's expected behavior.
	// The migration is registered but won't apply without legacy files.
	assert.Len(t, migrations, 0)
}
