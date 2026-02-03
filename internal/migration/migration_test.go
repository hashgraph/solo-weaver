// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *zerolog.Logger {
	l := zerolog.Nop()
	return &l
}

// ============================================================================
// Context Tests
// ============================================================================

func TestContext_GetSet(t *testing.T) {
	ctx := &Context{}

	// Test Set and Get
	ctx.Set("key1", "value1")
	ctx.Set("key2", 42)

	v1, ok := ctx.Get("key1")
	require.True(t, ok)
	assert.Equal(t, "value1", v1)

	v2, ok := ctx.Get("key2")
	require.True(t, ok)
	assert.Equal(t, 42, v2)

	// Test missing key
	_, ok = ctx.Get("missing")
	assert.False(t, ok)
}

func TestContext_GetString(t *testing.T) {
	ctx := &Context{}
	ctx.Set("str", "hello")
	ctx.Set("int", 42)

	assert.Equal(t, "hello", ctx.GetString("str"))
	assert.Equal(t, "", ctx.GetString("int"))     // wrong type
	assert.Equal(t, "", ctx.GetString("missing")) // missing key
}

// ============================================================================
// BaseMigration Tests
// ============================================================================

func TestBaseMigration_Metadata(t *testing.T) {
	m := NewBaseMigration("test-migration-v1.0.0", "Test migration", "1.0.0")

	assert.Equal(t, "test-migration-v1.0.0", m.ID())
	assert.Equal(t, "Test migration", m.Description())
	assert.Equal(t, "1.0.0", m.MinVersion())
}

func TestBaseMigration_Applies(t *testing.T) {
	m := &BaseMigration{
		id:          "test-v1.0.0",
		description: "Test",
		minVersion:  "1.0.0",
	}

	tests := []struct {
		name             string
		installedVersion string
		targetVersion    string
		expectApplies    bool
		expectError      bool
	}{
		{
			name:             "upgrade across boundary should apply",
			installedVersion: "0.9.0",
			targetVersion:    "1.0.0",
			expectApplies:    true,
		},
		{
			name:             "upgrade from below to above boundary should apply",
			installedVersion: "0.5.0",
			targetVersion:    "2.0.0",
			expectApplies:    true,
		},
		{
			name:             "upgrade within old versions should NOT apply",
			installedVersion: "0.8.0",
			targetVersion:    "0.9.0",
			expectApplies:    false,
		},
		{
			name:             "upgrade within new versions should NOT apply",
			installedVersion: "1.0.0",
			targetVersion:    "1.1.0",
			expectApplies:    false,
		},
		{
			name:             "downgrade should NOT apply",
			installedVersion: "1.0.0",
			targetVersion:    "0.9.0",
			expectApplies:    false,
		},
		{
			name:             "not installed should NOT apply",
			installedVersion: "",
			targetVersion:    "1.0.0",
			expectApplies:    false,
		},
		{
			name:             "same version should NOT apply",
			installedVersion: "0.9.0",
			targetVersion:    "0.9.0",
			expectApplies:    false,
		},
		{
			name:             "invalid installed version should error",
			installedVersion: "invalid",
			targetVersion:    "1.0.0",
			expectError:      true,
		},
		{
			name:             "invalid target version should error",
			installedVersion: "0.9.0",
			targetVersion:    "invalid",
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{
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

func TestBaseMigration_Execute_NotImplemented(t *testing.T) {
	m := &BaseMigration{}
	err := m.Execute(context.Background(), &Context{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

func TestBaseMigration_Rollback_Default(t *testing.T) {
	m := &BaseMigration{}
	err := m.Rollback(context.Background(), &Context{})
	require.NoError(t, err) // Default returns nil
}

// ============================================================================
// Mock Migration for Testing
// ============================================================================

type MockMigration struct {
	BaseMigration
	appliesOverride func(*Context) (bool, error)
	executeFunc     func(context.Context, *Context) error
	rollbackFunc    func(context.Context, *Context) error
	executeCalls    int
	rollbackCalls   int
}

func NewMockMigration(id, minVersion string) *MockMigration {
	return &MockMigration{
		BaseMigration: NewBaseMigration(id, "Mock: "+id, minVersion),
	}
}

func (m *MockMigration) Applies(ctx *Context) (bool, error) {
	if m.appliesOverride != nil {
		return m.appliesOverride(ctx)
	}
	return m.BaseMigration.Applies(ctx)
}

func (m *MockMigration) Execute(ctx context.Context, mctx *Context) error {
	m.executeCalls++
	if m.executeFunc != nil {
		return m.executeFunc(ctx, mctx)
	}
	return nil
}

func (m *MockMigration) Rollback(ctx context.Context, mctx *Context) error {
	m.rollbackCalls++
	if m.rollbackFunc != nil {
		return m.rollbackFunc(ctx, mctx)
	}
	return nil
}

// ============================================================================
// Manager Tests
// ============================================================================

func TestNewManager(t *testing.T) {
	logger := testLogger()

	t.Run("with defaults", func(t *testing.T) {
		m := NewManager()
		assert.NotNil(t, m)
		assert.Equal(t, "unknown", m.component)
	})

	t.Run("with options", func(t *testing.T) {
		m := NewManager(
			WithLogger(logger),
			WithComponent("test-component"),
		)
		assert.Equal(t, "test-component", m.component)
	})
}

func TestManager_Register(t *testing.T) {
	m := NewManager(WithComponent("test"))

	mock1 := NewMockMigration("migration-1", "1.0.0")
	mock2 := NewMockMigration("migration-2", "2.0.0")

	m.Register(mock1)
	m.Register(mock2)

	assert.Len(t, m.migrations, 2)
}

func TestManager_GetApplicable(t *testing.T) {
	logger := testLogger()
	m := NewManager(WithLogger(logger), WithComponent("test"))

	mock1 := NewMockMigration("migration-1", "1.0.0")
	mock2 := NewMockMigration("migration-2", "2.0.0")
	mock3 := NewMockMigration("migration-3", "3.0.0")

	m.Register(mock1)
	m.Register(mock2)
	m.Register(mock3)

	tests := []struct {
		name             string
		installedVersion string
		targetVersion    string
		expectCount      int
	}{
		{
			name:             "all migrations apply",
			installedVersion: "0.5.0",
			targetVersion:    "3.0.0",
			expectCount:      3,
		},
		{
			name:             "two migrations apply",
			installedVersion: "0.5.0",
			targetVersion:    "2.5.0",
			expectCount:      2,
		},
		{
			name:             "one migration applies",
			installedVersion: "0.5.0",
			targetVersion:    "1.5.0",
			expectCount:      1,
		},
		{
			name:             "no migrations apply",
			installedVersion: "3.0.0",
			targetVersion:    "4.0.0",
			expectCount:      0,
		},
		{
			name:             "fresh install - no migrations",
			installedVersion: "",
			targetVersion:    "3.0.0",
			expectCount:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{
				InstalledVersion: tt.installedVersion,
				TargetVersion:    tt.targetVersion,
				Logger:           logger,
			}

			applicable, err := m.GetApplicable(ctx)
			require.NoError(t, err)
			assert.Len(t, applicable, tt.expectCount)
		})
	}
}

func TestManager_RequiresMigration(t *testing.T) {
	logger := testLogger()
	m := NewManager(WithLogger(logger), WithComponent("test"))

	mock1 := NewMockMigration("migration-1", "1.0.0")
	m.Register(mock1)

	t.Run("migration required", func(t *testing.T) {
		ctx := &Context{
			InstalledVersion: "0.5.0",
			TargetVersion:    "1.0.0",
			Logger:           logger,
		}

		required, summary, err := m.RequiresMigration(ctx)
		require.NoError(t, err)
		assert.True(t, required)
		assert.Contains(t, summary, "migration-1")
		assert.Contains(t, summary, "test")
	})

	t.Run("no migration required", func(t *testing.T) {
		ctx := &Context{
			InstalledVersion: "1.0.0",
			TargetVersion:    "1.1.0",
			Logger:           logger,
		}

		required, summary, err := m.RequiresMigration(ctx)
		require.NoError(t, err)
		assert.False(t, required)
		assert.Empty(t, summary)
	})
}

func TestManager_Execute_Success(t *testing.T) {
	logger := testLogger()
	m := NewManager(WithLogger(logger), WithComponent("test"))

	mock1 := NewMockMigration("migration-1", "1.0.0")
	mock2 := NewMockMigration("migration-2", "2.0.0")

	m.Register(mock1)
	m.Register(mock2)

	ctx := &Context{
		InstalledVersion: "0.5.0",
		TargetVersion:    "3.0.0",
		Logger:           logger,
	}

	err := m.Execute(context.Background(), ctx)
	require.NoError(t, err)

	assert.Equal(t, 1, mock1.executeCalls)
	assert.Equal(t, 1, mock2.executeCalls)
	assert.Equal(t, 0, mock1.rollbackCalls)
	assert.Equal(t, 0, mock2.rollbackCalls)
}

func TestManager_Execute_NoMigrations(t *testing.T) {
	logger := testLogger()
	m := NewManager(WithLogger(logger), WithComponent("test"))

	mock1 := NewMockMigration("migration-1", "1.0.0")
	m.Register(mock1)

	ctx := &Context{
		InstalledVersion: "2.0.0", // Already past the migration
		TargetVersion:    "3.0.0",
		Logger:           logger,
	}

	err := m.Execute(context.Background(), ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, mock1.executeCalls)
}

func TestManager_Execute_FailureWithRollback(t *testing.T) {
	logger := testLogger()
	m := NewManager(WithLogger(logger), WithComponent("test"))

	mock1 := NewMockMigration("migration-1", "1.0.0")
	mock2 := NewMockMigration("migration-2", "2.0.0")
	mock2.executeFunc = func(ctx context.Context, mctx *Context) error {
		return errors.New("migration-2 failed")
	}

	m.Register(mock1)
	m.Register(mock2)

	ctx := &Context{
		InstalledVersion: "0.5.0",
		TargetVersion:    "3.0.0",
		Logger:           logger,
	}

	err := m.Execute(context.Background(), ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration-2")
	assert.Contains(t, err.Error(), "rollback succeeded")

	// First migration executed and rolled back
	assert.Equal(t, 1, mock1.executeCalls)
	assert.Equal(t, 1, mock1.rollbackCalls)

	// Second migration failed, no rollback for itself
	assert.Equal(t, 1, mock2.executeCalls)
	assert.Equal(t, 0, mock2.rollbackCalls)
}

func TestManager_Execute_FailureWithRollbackFailure(t *testing.T) {
	logger := testLogger()
	m := NewManager(WithLogger(logger), WithComponent("test"))

	mock1 := NewMockMigration("migration-1", "1.0.0")
	mock1.rollbackFunc = func(ctx context.Context, mctx *Context) error {
		return errors.New("rollback-1 failed")
	}

	mock2 := NewMockMigration("migration-2", "2.0.0")
	mock2.executeFunc = func(ctx context.Context, mctx *Context) error {
		return errors.New("migration-2 failed")
	}

	m.Register(mock1)
	m.Register(mock2)

	ctx := &Context{
		InstalledVersion: "0.5.0",
		TargetVersion:    "3.0.0",
		Logger:           logger,
	}

	err := m.Execute(context.Background(), ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration-2")
	assert.Contains(t, err.Error(), "rollback also failed")
	assert.Contains(t, err.Error(), "Manual intervention")
}

func TestManager_Execute_RollbackOrder(t *testing.T) {
	logger := testLogger()
	m := NewManager(WithLogger(logger), WithComponent("test"))

	var rollbackOrder []string

	mock1 := NewMockMigration("migration-1", "1.0.0")
	mock1.rollbackFunc = func(ctx context.Context, mctx *Context) error {
		rollbackOrder = append(rollbackOrder, "migration-1")
		return nil
	}

	mock2 := NewMockMigration("migration-2", "2.0.0")
	mock2.rollbackFunc = func(ctx context.Context, mctx *Context) error {
		rollbackOrder = append(rollbackOrder, "migration-2")
		return nil
	}

	mock3 := NewMockMigration("migration-3", "3.0.0")
	mock3.executeFunc = func(ctx context.Context, mctx *Context) error {
		return errors.New("migration-3 failed")
	}

	m.Register(mock1)
	m.Register(mock2)
	m.Register(mock3)

	ctx := &Context{
		InstalledVersion: "0.5.0",
		TargetVersion:    "4.0.0",
		Logger:           logger,
	}

	err := m.Execute(context.Background(), ctx)
	require.Error(t, err)

	// Rollback should happen in reverse order
	require.Len(t, rollbackOrder, 2)
	assert.Equal(t, "migration-2", rollbackOrder[0])
	assert.Equal(t, "migration-1", rollbackOrder[1])
}

func TestManager_Execute_WithContextData(t *testing.T) {
	logger := testLogger()
	m := NewManager(WithLogger(logger), WithComponent("test"))

	var capturedProfile string
	mock1 := NewMockMigration("migration-1", "1.0.0")
	mock1.executeFunc = func(ctx context.Context, mctx *Context) error {
		capturedProfile = mctx.GetString("profile")
		return nil
	}

	m.Register(mock1)

	ctx := &Context{
		InstalledVersion: "0.5.0",
		TargetVersion:    "1.0.0",
		Logger:           logger,
	}
	ctx.Set("profile", "production")

	err := m.Execute(context.Background(), ctx)
	require.NoError(t, err)
	assert.Equal(t, "production", capturedProfile)
}

// ============================================================================
// Multiple Component Tests
// ============================================================================

func TestMultipleComponentManagers(t *testing.T) {
	logger := testLogger()

	// Create managers for different components
	blockNodeManager := NewManager(WithLogger(logger), WithComponent("block-node"))
	ciliumManager := NewManager(WithLogger(logger), WithComponent("cilium"))

	// Register component-specific migrations
	blockNodeManager.Register(NewMockMigration("bn-migration-1", "0.26.2"))
	ciliumManager.Register(NewMockMigration("cilium-migration-1", "1.14.0"))

	// Check block-node migrations
	bnCtx := &Context{
		InstalledVersion: "0.26.0",
		TargetVersion:    "0.26.2",
		Logger:           logger,
	}
	bnApplicable, err := blockNodeManager.GetApplicable(bnCtx)
	require.NoError(t, err)
	assert.Len(t, bnApplicable, 1)
	assert.Equal(t, "bn-migration-1", bnApplicable[0].ID())

	// Check cilium migrations
	ciliumCtx := &Context{
		InstalledVersion: "1.13.0",
		TargetVersion:    "1.14.0",
		Logger:           logger,
	}
	ciliumApplicable, err := ciliumManager.GetApplicable(ciliumCtx)
	require.NoError(t, err)
	assert.Len(t, ciliumApplicable, 1)
	assert.Equal(t, "cilium-migration-1", ciliumApplicable[0].ID())
}
