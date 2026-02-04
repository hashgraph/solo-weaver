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
// VersionMigration Tests
// ============================================================================

func TestVersionMigration_Metadata(t *testing.T) {
	m := NewVersionMigration("test-migration-v1.0.0", "Test migration", "1.0.0")

	assert.Equal(t, "test-migration-v1.0.0", m.ID())
	assert.Equal(t, "Test migration", m.Description())
}

func TestVersionMigration_Applies(t *testing.T) {
	m := &VersionMigration{
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
			name:             "fresh install should NOT apply",
			installedVersion: "",
			targetVersion:    "1.0.0",
			expectApplies:    false,
		},
		{
			name:             "invalid installed version",
			installedVersion: "invalid",
			targetVersion:    "1.0.0",
			expectError:      true,
		},
		{
			name:             "invalid target version",
			installedVersion: "0.9.0",
			targetVersion:    "invalid",
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{
				Data: make(map[string]interface{}),
			}
			ctx.Set(CtxKeyInstalledVersion, tt.installedVersion)
			ctx.Set(CtxKeyTargetVersion, tt.targetVersion)

			applies, err := m.Applies(ctx)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectApplies, applies)
			}
		})
	}
}

// ============================================================================
// Mock Migration for testing
// ============================================================================

type MockMigration struct {
	VersionMigration
	executeFunc   func(ctx context.Context, mctx *Context) error
	rollbackFunc  func(ctx context.Context, mctx *Context) error
	executeCalls  int
	rollbackCalls int
}

func NewMockMigration(id, minVersion string) *MockMigration {
	return &MockMigration{
		VersionMigration: NewVersionMigration(id, "Mock migration", minVersion),
	}
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
// Registry Tests
// ============================================================================

func TestRegister_And_GetApplicable(t *testing.T) {
	// Clear registry before test
	ClearRegistry()
	defer ClearRegistry()

	mock1 := NewMockMigration("migration-1", "1.0.0")
	mock2 := NewMockMigration("migration-2", "2.0.0")
	mock3 := NewMockMigration("migration-3", "3.0.0")

	Register("test-component", mock1)
	Register("test-component", mock2)
	Register("test-component", mock3)

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
			mctx := &Context{Data: make(map[string]interface{})}
			mctx.Set(CtxKeyInstalledVersion, tt.installedVersion)
			mctx.Set(CtxKeyTargetVersion, tt.targetVersion)

			applicable, err := GetApplicableMigrations("test-component", mctx)
			require.NoError(t, err)
			assert.Len(t, applicable, tt.expectCount)
		})
	}
}

func TestMigrationsToWorkflow(t *testing.T) {
	logger := testLogger()

	t.Run("empty migrations returns no-op workflow", func(t *testing.T) {
		workflow := MigrationsToWorkflow(nil, &Context{Logger: logger})
		assert.NotNil(t, workflow)
	})

	t.Run("migrations execute in order", func(t *testing.T) {
		var order []string
		mock1 := NewMockMigration("m1", "1.0.0")
		mock1.executeFunc = func(ctx context.Context, mctx *Context) error {
			order = append(order, "m1")
			return nil
		}
		mock2 := NewMockMigration("m2", "2.0.0")
		mock2.executeFunc = func(ctx context.Context, mctx *Context) error {
			order = append(order, "m2")
			return nil
		}

		mctx := &Context{
			Component: "test",
			Logger:    logger,
			Data:      make(map[string]interface{}),
		}
		mctx.Set(CtxKeyInstalledVersion, "0.5.0")
		mctx.Set(CtxKeyTargetVersion, "2.5.0")

		workflow := MigrationsToWorkflow([]Migration{mock1, mock2}, mctx)
		wf, err := workflow.Build()
		require.NoError(t, err)

		report := wf.Execute(context.Background())
		require.NoError(t, report.Error)
		assert.Equal(t, []string{"m1", "m2"}, order)
	})

	t.Run("rollback on failure", func(t *testing.T) {
		var rollbackOrder []string
		mock1 := NewMockMigration("m1", "1.0.0")
		mock1.rollbackFunc = func(ctx context.Context, mctx *Context) error {
			rollbackOrder = append(rollbackOrder, "m1")
			return nil
		}
		mock2 := NewMockMigration("m2", "2.0.0")
		mock2.executeFunc = func(ctx context.Context, mctx *Context) error {
			return errors.New("migration failed")
		}

		mctx := &Context{
			Component: "test",
			Logger:    logger,
			Data:      make(map[string]interface{}),
		}
		mctx.Set(CtxKeyInstalledVersion, "0.5.0")
		mctx.Set(CtxKeyTargetVersion, "2.5.0")

		workflow := MigrationsToWorkflow([]Migration{mock1, mock2}, mctx)
		wf, err := workflow.Build()
		require.NoError(t, err)

		report := wf.Execute(context.Background())
		require.Error(t, report.Error)
		// m1 should be rolled back since m2 failed
		assert.Contains(t, rollbackOrder, "m1")
	})
}

func TestClearRegistry(t *testing.T) {
	ClearRegistry()

	Register("test", NewMockMigration("m1", "1.0.0"))

	mctx := &Context{Data: make(map[string]interface{})}
	mctx.Set(CtxKeyInstalledVersion, "0.5.0")
	mctx.Set(CtxKeyTargetVersion, "1.5.0")

	applicable, _ := GetApplicableMigrations("test", mctx)
	assert.Len(t, applicable, 1)

	ClearRegistry()

	applicable, _ = GetApplicableMigrations("test", mctx)
	assert.Len(t, applicable, 0)
}
