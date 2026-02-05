// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyBinaryMigration_Applies(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "weaver-migration-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	m := NewLegacyBinaryMigration()

	tests := []struct {
		name           string
		setupLegacy    bool
		expectedResult bool
	}{
		{
			name:           "no legacy binary",
			setupLegacy:    false,
			expectedResult: false,
		},
		{
			name:           "legacy binary exists",
			setupLegacy:    true,
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: This test is limited because it uses the actual core.Paths().BinDir
			// A more complete test would mock the paths
			mctx := &migration.Context{
				Component: MigrationComponent,
				Data:      &automa.SyncStateBag{},
			}

			// The actual Applies check uses core.Paths().BinDir which we can't easily mock here
			// So we just verify the function returns without error
			_, err := m.Applies(mctx)
			assert.NoError(t, err)
		})
	}
}

func TestLegacyBinaryMigration_Execute(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "weaver-migration-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a fake legacy binary
	legacyPath := filepath.Join(tmpDir, "weaver")
	err = os.WriteFile(legacyPath, []byte("fake binary"), 0755)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(legacyPath)
	require.NoError(t, err)

	// Note: We can't fully test Execute without mocking core.Paths()
	// This is a placeholder for when proper mocking is implemented
	m := NewLegacyBinaryMigration()
	assert.Equal(t, "remove-legacy-weaver-binary", m.ID())
	assert.Equal(t, "Remove legacy 'weaver' binary after rename to 'solo-provisioner'", m.Description())
}

func TestLegacyBinaryMigration_Rollback(t *testing.T) {
	m := NewLegacyBinaryMigration()
	mctx := &migration.Context{
		Component: MigrationComponent,
		Data:      &automa.SyncStateBag{},
	}

	// Rollback should be a no-op and not return an error
	err := m.Rollback(context.Background(), mctx)
	assert.NoError(t, err)
}

func TestInitMigrations(t *testing.T) {
	migration.ClearRegistry()
	InitMigrations()
	defer migration.ClearRegistry()

	mctx := &migration.Context{
		Component: MigrationComponent,
		Data:      &automa.SyncStateBag{},
	}

	// Should be able to get migrations for our component without error
	// The migration will only apply if legacy binary exists, which it doesn't in test
	// So we expect an empty slice (or nil), not an error
	migrations, err := migration.GetApplicableMigrations(MigrationComponent, mctx)
	require.NoError(t, err)
	// No legacy binary exists in test environment, so no migrations should apply
	assert.Empty(t, migrations)
}
