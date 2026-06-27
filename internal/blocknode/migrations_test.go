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
			// 0.36.x still requires verification: the volume retires only at
			// 0.37.0. The Applies override defers to RequiredByVersion which
			// returns true for [0.26.2, 0.37.0).
			name:             "upgrade from 0.25.0 to 0.36.0 SHOULD apply (verification still present)",
			installedVersion: "0.25.0",
			targetVersion:    "0.36.0",
			expectApplies:    true,
		},
		{
			// Regression for the MaxVersion guard on StorageMigration.Applies.
			// Without it, the generic VersionMigration check would report this
			// upgrade as applicable (installed < 0.26.2 && target >= 0.26.2),
			// creating an orphan verification PV/PVC at a chart version that
			// has truly retired the verification volume (>= 0.37.0).
			name:             "upgrade across retirement boundary (0.25.0 -> 0.37.0) should NOT apply",
			installedVersion: "0.25.0",
			targetVersion:    "0.37.0",
			expectApplies:    false,
		},
		{
			name:             "upgrade across retirement boundary (0.25.0 -> 1.0.0) should NOT apply",
			installedVersion: "0.25.0",
			targetVersion:    "1.0.0",
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

// TestApplicationStateMigration_Applies tests the version boundary detection
// for the application-state storage migration. The boundary is
// BlockNodeApplicationStateRequiredVersion (0.37.0) — the chart version where
// the new volume first appears (and verification simultaneously retires).
func TestApplicationStateMigration_Applies(t *testing.T) {
	m := NewApplicationStateMigration()

	tests := []struct {
		name             string
		installedVersion string
		targetVersion    string
		expectApplies    bool
		expectError      bool
	}{
		{
			name:             "upgrade from 0.36.0 to 0.37.0 should apply (cutover crossed)",
			installedVersion: "0.36.0",
			targetVersion:    "0.37.0",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.28.1 to 0.37.0 should apply",
			installedVersion: "0.28.1",
			targetVersion:    "0.37.0",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.35.1 to 0.37.0 should apply",
			installedVersion: "0.35.1",
			targetVersion:    "0.37.0",
			expectApplies:    true,
		},
		{
			name:             "upgrade from 0.35.1 to 0.36.0 should NOT apply (still pre-cutover)",
			installedVersion: "0.35.1",
			targetVersion:    "0.36.0",
			expectApplies:    false,
		},
		{
			name:             "upgrade from 0.37.0 to 0.37.5 should NOT apply (already past boundary)",
			installedVersion: "0.37.0",
			targetVersion:    "0.37.5",
			expectApplies:    false,
		},
		{
			name:             "no-version-change upgrade at 0.37.0 should NOT apply",
			installedVersion: "0.37.0",
			targetVersion:    "0.37.0",
			expectApplies:    false,
		},
		{
			name:             "downgrade from 0.37.0 to 0.36.0 should NOT apply",
			installedVersion: "0.37.0",
			targetVersion:    "0.36.0",
			expectApplies:    false,
		},
		{
			name:             "fresh install at 0.37.0 (empty installed) should NOT apply",
			installedVersion: "",
			targetVersion:    "0.37.0",
			expectApplies:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &migration.Context{Data: &automa.SyncStateBag{}}
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
	migration.Register(migration.ScopeBlockNode, NewVerificationStorageMigration())
	migration.Register(migration.ScopeBlockNode, NewPluginsStorageMigration())
	migration.Register(migration.ScopeBlockNode, NewApplicationStateMigration())
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
			name:             "upgrade from 0.35.1 to 0.36.0 requires no migration (still pre-cutover)",
			installedVersion: "0.35.1",
			targetVersion:    "0.36.0",
			expectCount:      0,
		},
		{
			name:             "upgrade from 0.35.1 to 0.37.0 requires application-state migration only",
			installedVersion: "0.35.1",
			targetVersion:    "0.37.0",
			expectCount:      1,
		},
		{
			name:             "upgrade from 0.27.0 to 0.36.0 requires plugins migration only",
			installedVersion: "0.27.0",
			targetVersion:    "0.36.0",
			expectCount:      1,
		},
		{
			// Skip from pre-verification to 0.36.x triggers verification + plugins
			// (application-state isn't yet at 0.36.x).
			name:             "skip from 0.25.0 to 0.36.0 requires verification + plugins migrations",
			installedVersion: "0.25.0",
			targetVersion:    "0.36.0",
			expectCount:      2,
		},
		{
			// Regression: skipping past the 0.37.0 cutover must NOT pull in the
			// verification migration (it has retired) but MUST pull in plugins
			// and application-state.
			name:             "skip across cutover (0.25.0 -> 0.37.0) requires plugins + application-state",
			installedVersion: "0.25.0",
			targetVersion:    "0.37.0",
			expectCount:      2,
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

			migrations, err := migration.GetApplicableMigrations(migration.ScopeBlockNode, mctx)

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
	migration.Register(migration.ScopeBlockNode, NewVerificationStorageMigration())
	migration.Register(migration.ScopeBlockNode, NewPluginsStorageMigration())
	defer migration.ClearRegistry()

	// Check that verification migration is registered
	mctx := &migration.Context{Data: &automa.SyncStateBag{}}
	mctx.Data.Set(migration.CtxKeyInstalledVersion, "0.26.0")
	mctx.Data.Set(migration.CtxKeyTargetVersion, "0.26.2")
	migrations, err := migration.GetApplicableMigrations(migration.ScopeBlockNode, mctx)
	require.NoError(t, err)
	require.Len(t, migrations, 1)
	assert.Equal(t, "verification-storage-v0.26.2", migrations[0].ID())

	// Check that plugins migration is registered
	mctx2 := &migration.Context{Data: &automa.SyncStateBag{}}
	mctx2.Data.Set(migration.CtxKeyInstalledVersion, "0.27.0")
	mctx2.Data.Set(migration.CtxKeyTargetVersion, "0.28.1")
	migrations2, err := migration.GetApplicableMigrations(migration.ScopeBlockNode, mctx2)
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
