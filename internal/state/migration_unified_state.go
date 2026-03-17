// SPDX-License-Identifier: Apache-2.0

// migration_unified_state.go implements the unified state file migration.
//
// This migration consolidates multiple legacy state files (*.installed, *.configured)
// into the new unified State model by writing SoftwareState entries into
// MachineState.Software via the DefaultStateManager and SetSoftwareState helper.
// It preserves the legacy files until Rollback is explicitly called.

package state

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// UnifiedStateMigration consolidates individual legacy state files into the
// unified state.yaml managed by DefaultStateManager.
type UnifiedStateMigration struct {
	id          string
	description string
}

// NewUnifiedStateMigration creates a new unified state migration.
func NewUnifiedStateMigration() *UnifiedStateMigration {
	return &UnifiedStateMigration{
		id:          "unified-state",
		description: "Consolidate individual *.installed / *.configured files into state.yaml",
	}
}

func (m *UnifiedStateMigration) ID() string          { return m.id }
func (m *UnifiedStateMigration) Description() string { return m.description }

// Applies returns true when legacy *.installed or *.configured files are present.
func (m *UnifiedStateMigration) Applies(mctx *migration.Context) (bool, error) {
	stateDir := models.Paths().StateDir
	// If the directory doesn't exist there are no legacy files to migrate.
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return false, nil
	}
	files, err := findLegacyStateFiles(stateDir)
	if err != nil {
		return false, err
	}
	return len(files) > 0, nil
}

// Execute reads all legacy state files and merges them into the unified state via
// DefaultStateManager.  The legacy files are removed on success.
func (m *UnifiedStateMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	files, err := findLegacyStateFiles(models.Paths().StateDir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	sm, err := NewStateManager()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager for unified-state migration")
	}

	current := sm.State()

	for _, fp := range files {
		base := filepath.Base(fp)
		parts := strings.SplitN(base, ".", 2)
		if len(parts) != 2 {
			continue
		}
		component, stateType := parts[0], parts[1]

		content, readErr := os.ReadFile(fp)
		if readErr != nil {
			return errorx.IllegalState.Wrap(readErr, "failed to read legacy state file %s", base)
		}
		version := parseVersionFromContent(string(content))

		sw := GetSoftwareState(current, component)
		sw.Version = version
		switch stateType {
		case "installed":
			sw.Installed = true
		case "configured":
			sw.Configured = true
		}
		current = SetSoftwareState(current, component, sw)
	}

	if err = sm.Set(current).FlushState(); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to flush migrated state")
	}

	// Remove legacy files now that the state has been persisted.
	for _, fp := range files {
		if removeErr := os.Remove(fp); removeErr != nil && mctx.Logger != nil {
			mctx.Logger.Warn().Err(removeErr).Str("file", fp).Msg("Failed to remove legacy state file after migration")
		}
	}

	return nil
}

// Rollback restores the legacy state files from the unified state.
func (m *UnifiedStateMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	sm, err := NewStateManager()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager for unified-state rollback")
	}

	current := sm.State()
	for name, sw := range current.MachineState.Software {
		if sw.Installed {
			content := formatStateContent("installed", sw.Version)
			fp := filepath.Join(models.Paths().StateDir, name+".installed")
			if writeErr := os.WriteFile(fp, []byte(content), 0o600); writeErr != nil {
				return errorx.IllegalState.Wrap(writeErr, "failed to restore %s.installed", name)
			}
		}
		if sw.Configured {
			content := formatStateContent("configured", sw.Version)
			fp := filepath.Join(models.Paths().StateDir, name+".configured")
			if writeErr := os.WriteFile(fp, []byte(content), 0o600); writeErr != nil {
				return errorx.IllegalState.Wrap(writeErr, "failed to restore %s.configured", name)
			}
		}
	}
	return nil
}

// parseVersionFromContent extracts version from content like "installed at version 1.16.0"
func parseVersionFromContent(content string) string {
	content = strings.TrimSpace(content)
	parts := strings.Split(content, " at version ")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return "unknown"
}

// formatStateContent formats state content for individual files
func formatStateContent(stateType, version string) string {
	return stateType + " at version " + version + "\n"
}

// findLegacyStateFiles returns paths of all *.installed and *.configured files
// found in stateDir. These are the per-component state files written by the
// old file-based Manager that this migration consolidates.
func findLegacyStateFiles(stateDir string) ([]string, error) {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to read state directory %s", stateDir)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".installed") || strings.HasSuffix(name, ".configured") {
			files = append(files, filepath.Join(stateDir, name))
		}
	}
	return files, nil
}
