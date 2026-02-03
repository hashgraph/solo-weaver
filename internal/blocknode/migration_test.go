// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerificationStorageMigration_Applies tests the version boundary detection
func TestVerificationStorageMigration_Applies(t *testing.T) {
	m := NewVerificationStorageMigration()

	tests := []struct {
		name             string
		installedVersion string
		targetVersion    string
		expectApplies    bool
		expectError      bool
	}{
		{
			name:             "upgrade from 0.26.0 to 0.26.2 should apply",
			installedVersion: "0.26.0",
			targetVersion:    "0.26.2",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.25.0 to 0.26.2 should apply",
			installedVersion: "0.25.0",
			targetVersion:    "0.26.2",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.26.1 to 0.26.2 should apply",
			installedVersion: "0.26.1",
			targetVersion:    "0.26.2",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.26.0 to 0.27.0 should apply",
			installedVersion: "0.26.0",
			targetVersion:    "0.27.0",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.26.2 to 0.26.3 should NOT apply",
			installedVersion: "0.26.2",
			targetVersion:    "0.26.3",
			expectApplies:    false,
		},
		{
			name:             "upgrade from 0.26.2 to 0.27.0 should NOT apply",
			installedVersion: "0.26.2",
			targetVersion:    "0.27.0",
			expectApplies:    false,
		},
		{
			name:             "upgrade from 0.27.0 to 0.28.0 should NOT apply",
			installedVersion: "0.27.0",
			targetVersion:    "0.28.0",
			expectApplies:    false,
		},
		{
			name:             "downgrade from 0.26.2 to 0.26.0 should NOT apply",
			installedVersion: "0.26.2",
			targetVersion:    "0.26.0",
			expectApplies:    false,
		},
		{
			name:             "not installed (empty version) should NOT apply",
			installedVersion: "",
			targetVersion:    "0.26.2",
			expectApplies:    false,
		},
		{
			name:             "same version upgrade should NOT apply",
			installedVersion: "0.26.0",
			targetVersion:    "0.26.0",
			expectApplies:    false,
		},
		{
			name:             "invalid installed version should error",
			installedVersion: "invalid",
			targetVersion:    "0.26.2",
			expectError:      true,
		},
		{
			name:             "invalid target version should error",
			installedVersion: "0.26.0",
			targetVersion:    "invalid",
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &migration.Context{
				InstalledVersion: tt.installedVersion,
				TargetVersion:    tt.targetVersion,
			}
			applies, err := m.Applies(ctx)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectApplies, applies)
		})
	}
}

// TestBlockNodeMigrationManager_GetApplicable tests finding applicable migrations
func TestBlockNodeMigrationManager_GetApplicable(t *testing.T) {
	logger := testLogger()
	mm := NewBlockNodeMigrationManager(logger)

	tests := []struct {
		name             string
		installedVersion string
		targetVersion    string
		expectCount      int
		expectError      bool
	}{
		{
			name:             "upgrade requiring verification migration",
			installedVersion: "0.26.0",
			targetVersion:    "0.26.2",
			expectCount:      1,
		},
		{
			name:             "upgrade not requiring any migration",
			installedVersion: "0.26.2",
			targetVersion:    "0.26.3",
			expectCount:      0,
		},
		{
			name:             "fresh install (no installed version)",
			installedVersion: "",
			targetVersion:    "0.26.2",
			expectCount:      0,
		},
		{
			name:             "invalid installed version",
			installedVersion: "not-a-version",
			targetVersion:    "0.26.2",
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &migration.Context{
				InstalledVersion: tt.installedVersion,
				TargetVersion:    tt.targetVersion,
				Logger:           logger,
			}
			migrations, err := mm.GetApplicable(ctx)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, migrations, tt.expectCount)
		})
	}
}

// TestBlockNodeMigrationManager_RequiresMigration tests the summary generation
func TestBlockNodeMigrationManager_RequiresMigration(t *testing.T) {
	logger := testLogger()
	mm := NewBlockNodeMigrationManager(logger)

	tests := []struct {
		name             string
		installedVersion string
		targetVersion    string
		expectRequired   bool
		expectError      bool
	}{
		{
			name:             "migration required",
			installedVersion: "0.26.0",
			targetVersion:    "0.26.2",
			expectRequired:   true,
		},
		{
			name:             "no migration required",
			installedVersion: "0.26.2",
			targetVersion:    "0.26.3",
			expectRequired:   false,
		},
		{
			name:             "fresh install",
			installedVersion: "",
			targetVersion:    "0.26.2",
			expectRequired:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &migration.Context{
				InstalledVersion: tt.installedVersion,
				TargetVersion:    tt.targetVersion,
				Logger:           logger,
			}
			required, summary, err := mm.RequiresMigration(ctx)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectRequired, required)

			if tt.expectRequired {
				assert.NotEmpty(t, summary)
				assert.Contains(t, summary, "migration")
			} else {
				assert.Empty(t, summary)
			}
		})
	}
}

// TestBlockNodeMigrationManager_Registration tests that migrations can be registered
func TestBlockNodeMigrationManager_Registration(t *testing.T) {
	logger := testLogger()
	mm := NewBlockNodeMigrationManager(logger)

	// NewBlockNodeMigrationManager registers the verification storage migration by default
	ctx := &migration.Context{
		InstalledVersion: "0.26.0",
		TargetVersion:    "0.26.2",
		Logger:           logger,
	}
	migrations, err := mm.GetApplicable(ctx)
	require.NoError(t, err)
	require.Len(t, migrations, 1)
	assert.Equal(t, "verification-storage-v0.26.2", migrations[0].ID())
}

// TestVerificationStorageMigration_Metadata tests migration metadata
func TestVerificationStorageMigration_Metadata(t *testing.T) {
	m := NewVerificationStorageMigration()

	assert.Equal(t, "verification-storage-v0.26.2", m.ID())
	assert.NotEmpty(t, m.Description())
	assert.Equal(t, "0.26.2", m.MinVersion())
}
