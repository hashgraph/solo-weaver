// SPDX-License-Identifier: Apache-2.0

// migration_legacy_binary.go implements the legacy "weaver" binary removal migration.
//
// This migration detects and removes the legacy "weaver" binary that was used before
// the CLI was renamed to "solo-provisioner". It:
//   - Checks if the legacy "weaver" binary exists in the bin directory
//   - Removes the binary and its symlink in /usr/local/bin
//
// This file is registered in migrations.go via InitMigrations().

package workflows

import (
	"context"
	"os"
	"path/filepath"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
)

const legacyBinaryName = "weaver"

// LegacyBinaryMigration removes the old "weaver" binary after rename to "solo-provisioner".
// This migration applies based on file state, not version boundaries.
type LegacyBinaryMigration struct {
	id          string
	description string
}

// NewLegacyBinaryMigration creates a new legacy binary removal migration.
func NewLegacyBinaryMigration() *LegacyBinaryMigration {
	return &LegacyBinaryMigration{
		id:          "remove-legacy-weaver-binary",
		description: "Remove legacy 'weaver' binary after rename to 'solo-provisioner'",
	}
}

func (m *LegacyBinaryMigration) ID() string          { return m.id }
func (m *LegacyBinaryMigration) Description() string { return m.description }

// Applies returns true if the legacy "weaver" binary exists and needs to be removed.
func (m *LegacyBinaryMigration) Applies(mctx *migration.Context) (bool, error) {
	binDir := core.Paths().BinDir
	legacyPath := filepath.Join(binDir, legacyBinaryName)

	// Check if legacy binary exists
	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		return false, nil // No legacy binary, nothing to migrate
	} else if err != nil {
		return false, errorx.InternalError.Wrap(err, "failed to check for legacy weaver binary")
	}

	return true, nil
}

// Execute removes the legacy "weaver" binary and its symlink.
func (m *LegacyBinaryMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	binDir := core.Paths().BinDir
	legacyPath := filepath.Join(binDir, legacyBinaryName)
	legacySymlink := filepath.Join("/usr/local/bin", legacyBinaryName)

	logx.As().Info().
		Str("legacy_binary_path", legacyPath).
		Msg("Removing legacy 'weaver' binary")

	// Remove the legacy binary
	if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
		return errorx.InternalError.Wrap(err, "failed to remove legacy weaver binary at %s", legacyPath)
	}

	// Remove the legacy symlink (best effort, ignore errors)
	if err := os.Remove(legacySymlink); err != nil && !os.IsNotExist(err) {
		logx.As().Warn().
			Str("symlink_path", legacySymlink).
			Err(err).
			Msg("Failed to remove legacy weaver symlink (non-fatal)")
	} else if err == nil {
		logx.As().Info().
			Str("symlink_path", legacySymlink).
			Msg("Removed legacy 'weaver' symlink")
	}

	logx.As().Info().Msg("Legacy 'weaver' binary removed successfully")

	return nil
}

// Rollback is a no-op for this migration since we can't restore the old binary.
// The user would need to reinstall the old version if they want to go back.
func (m *LegacyBinaryMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	logx.As().Warn().Msg("Rollback for legacy binary removal is not supported - the old binary cannot be restored automatically")
	return nil
}
