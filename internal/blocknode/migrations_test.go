// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"

	"github.com/automa-saga/automa"
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
				Data: &automa.SyncStateBag{},
			}
			ctx.Data.Set(migration.CtxKeyInstalledVersion, tt.installedVersion)
			ctx.Data.Set(migration.CtxKeyTargetVersion, tt.targetVersion)
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

// TestPluginsStorageMigration_Applies tests the version boundary detection for plugins migration
func TestPluginsStorageMigration_Applies(t *testing.T) {
	m := NewPluginsStorageMigration()

	tests := []struct {
		name             string
		installedVersion string
		targetVersion    string
		expectApplies    bool
		expectError      bool
	}{
		{
			name:             "upgrade from 0.28.0 to 0.28.1 should apply",
			installedVersion: "0.28.0",
			targetVersion:    "0.28.1",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.27.0 to 0.28.1 should apply",
			installedVersion: "0.27.0",
			targetVersion:    "0.28.1",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.26.2 to 0.28.1 should apply",
			installedVersion: "0.26.2",
			targetVersion:    "0.28.1",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.27.0 to 0.29.0 should apply",
			installedVersion: "0.27.0",
			targetVersion:    "0.29.0",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.26.2 to 0.28.2 should apply",
			installedVersion: "0.26.2",
			targetVersion:    "0.28.2",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.28.1 to 0.28.2 should NOT apply",
			installedVersion: "0.28.1",
			targetVersion:    "0.28.2",
			expectApplies:    false,
		},
		{
			name:             "upgrade from 0.28.1 to 0.29.0 should NOT apply",
			installedVersion: "0.28.1",
			targetVersion:    "0.29.0",
			expectApplies:    false,
		},
		{
			name:             "downgrade from 0.28.1 to 0.27.0 should NOT apply",
			installedVersion: "0.28.1",
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
			name:             "not installed (empty version) should NOT apply",
			installedVersion: "",
			targetVersion:    "0.28.1",
			expectApplies:    false,
		},
		{
			name:             "same version upgrade should NOT apply",
			installedVersion: "0.27.0",
			targetVersion:    "0.27.0",
			expectApplies:    false,
		},
		{
			name:             "invalid installed version should error",
			installedVersion: "invalid",
			targetVersion:    "0.28.1",
			expectError:      true,
		},
		{
			name:             "invalid target version should error",
			installedVersion: "0.27.0",
			targetVersion:    "invalid",
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &migration.Context{
				Data: &automa.SyncStateBag{},
			}
			ctx.Data.Set(migration.CtxKeyInstalledVersion, tt.installedVersion)
			ctx.Data.Set(migration.CtxKeyTargetVersion, tt.targetVersion)
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

// TestGetApplicable_BlockNode tests finding applicable migrations using global registry
func TestGetApplicable_BlockNode(t *testing.T) {
	// Clear and register to ensure clean state
	migration.ClearRegistry()
	InitMigrations()
	defer migration.ClearRegistry()

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
			name:             "upgrade requiring plugins migration only",
			installedVersion: "0.27.0",
			targetVersion:    "0.28.1",
			expectCount:      1,
		},
		{
			name:             "upgrade from 0.26.2 to 0.28.0 should NOT require plugins migration",
			installedVersion: "0.26.2",
			targetVersion:    "0.28.0",
			expectCount:      0,
		},
		{
			name:             "upgrade requiring both verification and plugins migrations",
			installedVersion: "0.26.0",
			targetVersion:    "0.28.1",
			expectCount:      2,
		},
		{
			name:             "upgrade not requiring any migration",
			installedVersion: "0.28.1",
			targetVersion:    "0.28.2",
			expectCount:      0,
		},
		{
			name:             "fresh install (no installed version)",
			installedVersion: "",
			targetVersion:    "0.28.1",
			expectCount:      0,
		},
		{
			name:             "invalid installed version",
			installedVersion: "not-a-version",
			targetVersion:    "0.28.1",
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mctx := &migration.Context{Data: &automa.SyncStateBag{}}
			mctx.Data.Set(migration.CtxKeyInstalledVersion, tt.installedVersion)
			mctx.Data.Set(migration.CtxKeyTargetVersion, tt.targetVersion)

			migrations, err := migration.GetApplicableMigrations(ComponentBlockNode, mctx)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, migrations, tt.expectCount)
		})
	}
}

// TestMigrationRegistration tests that migrations can be registered correctly
func TestMigrationRegistration(t *testing.T) {
	migration.ClearRegistry()
	InitMigrations()
	defer migration.ClearRegistry()

	// Check that verification migration is registered
	mctx := &migration.Context{Data: &automa.SyncStateBag{}}
	mctx.Data.Set(migration.CtxKeyInstalledVersion, "0.26.0")
	mctx.Data.Set(migration.CtxKeyTargetVersion, "0.26.2")
	migrations, err := migration.GetApplicableMigrations(ComponentBlockNode, mctx)
	require.NoError(t, err)
	require.Len(t, migrations, 1)
	assert.Equal(t, "verification-storage-v0.26.2", migrations[0].ID())

	// Check that plugins migration is registered
	mctx2 := &migration.Context{Data: &automa.SyncStateBag{}}
	mctx2.Data.Set(migration.CtxKeyInstalledVersion, "0.27.0")
	mctx2.Data.Set(migration.CtxKeyTargetVersion, "0.28.1")
	migrations2, err := migration.GetApplicableMigrations(ComponentBlockNode, mctx2)
	require.NoError(t, err)
	require.Len(t, migrations2, 1)
	assert.Equal(t, "plugins-storage-v0.28.1", migrations2[0].ID())
}

// TestVerificationStorageMigration_Metadata tests migration metadata
func TestVerificationStorageMigration_Metadata(t *testing.T) {
	m := NewVerificationStorageMigration()

	assert.Equal(t, "verification-storage-v0.26.2", m.ID())
	assert.NotEmpty(t, m.Description())
}

// TestPluginsStorageMigration_Metadata tests plugins migration metadata
func TestPluginsStorageMigration_Metadata(t *testing.T) {
	m := NewPluginsStorageMigration()

	assert.Equal(t, "plugins-storage-v0.28.1", m.ID())
	assert.NotEmpty(t, m.Description())
}
