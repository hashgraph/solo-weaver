// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// CLIVersionMigration Tests
// ============================================================================

func TestCLIVersionMigration_Metadata(t *testing.T) {
	m := NewCLIVersionMigration("my-change-v1.2.0", "My CLI change", "1.2.0")

	assert.Equal(t, "my-change-v1.2.0", m.ID())
	assert.Equal(t, "My CLI change", m.Description())
}

func TestCLIVersionMigration_Applies(t *testing.T) {
	m := &CLIVersionMigration{
		id:          "test-cli-v1.0.0",
		description: "Test",
		minVersion:  "1.0.0",
	}

	tests := []struct {
		name             string
		installedVersion string
		currentVersion   string
		expectApplies    bool
		expectError      bool
	}{
		// ── applies ──────────────────────────────────────────────────────────
		{
			name:             "upgrade across boundary should apply",
			installedVersion: "0.9.0",
			currentVersion:   "1.0.0",
			expectApplies:    true,
		},
		{
			name:             "upgrade from well below to above boundary should apply",
			installedVersion: "0.5.0",
			currentVersion:   "2.0.0",
			expectApplies:    true,
		},
		// ── does not apply ───────────────────────────────────────────────────
		{
			name:             "upgrade within old versions should NOT apply",
			installedVersion: "0.8.0",
			currentVersion:   "0.9.0",
			expectApplies:    false,
		},
		{
			name:             "upgrade within new versions (already past boundary) should NOT apply",
			installedVersion: "1.0.0",
			currentVersion:   "1.1.0",
			expectApplies:    false,
		},
		{
			name:             "same installed and current version should NOT apply",
			installedVersion: "1.0.0",
			currentVersion:   "1.0.0",
			expectApplies:    false,
		},
		{
			name:             "downgrade should NOT apply",
			installedVersion: "1.0.0",
			currentVersion:   "0.9.0",
			expectApplies:    false,
		},
		{
			name:             "fresh install (empty installedCLIVersion) should NOT apply",
			installedVersion: "",
			currentVersion:   "1.0.0",
			expectApplies:    false,
		},
		// ── error paths ──────────────────────────────────────────────────────
		{
			name:             "missing currentCLIVersion key should return error",
			installedVersion: "0.9.0",
			currentVersion:   "", // not set in context
			expectError:      true,
		},
		{
			name:             "invalid installedCLIVersion should return error",
			installedVersion: "not-a-version",
			currentVersion:   "1.0.0",
			expectError:      true,
		},
		{
			name:             "invalid currentCLIVersion should return error",
			installedVersion: "0.9.0",
			currentVersion:   "not-a-version",
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mctx := &Context{
				Data: &automa.SyncStateBag{},
			}
			mctx.Data.Set(CtxKeyInstalledCLIVersion, tt.installedVersion)
			// Omitting the key entirely when currentVersion is empty simulates
			// a context that was not populated by RunStartupMigrations.
			if tt.currentVersion != "" {
				mctx.Data.Set(CtxKeyCurrentCLIVersion, tt.currentVersion)
			}

			applies, err := m.Applies(mctx)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectApplies, applies)
			}
		})
	}
}

func TestCLIVersionMigration_Applies_InvalidMinVersion(t *testing.T) {
	// A migration constructed with a bad minVersion should surface an error
	// in Applies() rather than silently misrouting.
	m := &CLIVersionMigration{
		id:         "bad-min",
		minVersion: "not-a-version",
	}

	mctx := &Context{Data: &automa.SyncStateBag{}}
	mctx.Data.Set(CtxKeyInstalledCLIVersion, "0.9.0")
	mctx.Data.Set(CtxKeyCurrentCLIVersion, "1.0.0")

	_, err := m.Applies(mctx)
	require.Error(t, err)
}
