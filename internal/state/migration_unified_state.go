// SPDX-License-Identifier: Apache-2.0

// migration_unified_state.go implements the unified state file migration.
//
// This migration consolidates multiple legacy state files (network state, block node state, etc.)
// into a single unified state file at ~/.weaver/state.yaml. It:
//   - Detects legacy state files from previous weaver versions
//   - Migrates their contents into the new unified format
//   - Preserves the old files as backups during rollback
//
// This file is registered in migrations.go via InitMigrations().

package state

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// UnifiedState represents the consolidated state file structure.
type UnifiedState struct {
	Version    string                     `yaml:"version"`
	Components map[string]*ComponentState `yaml:"components"`
}

// ComponentState represents the state of a single component.
type ComponentState struct {
	Installed  *StateEntry `yaml:"installed,omitempty"`
	Configured *StateEntry `yaml:"configured,omitempty"`
}

// StateEntry represents a single state entry with version info.
type StateEntry struct {
	Version string `yaml:"version"`
}

// UnifiedStateMigration consolidates individual state files into a single state.yaml.
// This migration applies based on file state, not version boundaries.
type UnifiedStateMigration struct {
	id          string
	description string
}

// NewUnifiedStateMigration creates a new unified state migration.
func NewUnifiedStateMigration() *UnifiedStateMigration {
	return &UnifiedStateMigration{
		id:          "unified-state",
		description: "Consolidate individual state files into single state.yaml",
	}
}

func (m *UnifiedStateMigration) ID() string          { return m.id }
func (m *UnifiedStateMigration) Description() string { return m.description }

// Applies returns true if there are individual state files that need to be migrated.
// This checks for the existence of legacy state files (*.installed, *.configured)
// that haven't been consolidated into state.yaml yet.
func (m *UnifiedStateMigration) Applies(mctx *migration.Context) (bool, error) {
	stateDir := core.Paths().StateDir

	// Check if state directory exists
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return false, nil // No state directory, nothing to migrate
	}

	// Check for legacy individual state files
	legacyFiles, err := findLegacyStateFiles(stateDir)
	if err != nil {
		return false, err
	}

	// If no legacy files exist, no migration needed
	return len(legacyFiles) > 0, nil
}

// findLegacyStateFiles returns a list of legacy state files (*.installed, *.configured)
func findLegacyStateFiles(stateDir string) ([]string, error) {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to read state directory")
	}

	var legacyFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".installed") || strings.HasSuffix(name, ".configured") {
			legacyFiles = append(legacyFiles, filepath.Join(stateDir, name))
		}
	}
	return legacyFiles, nil
}

func (m *UnifiedStateMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	stateDir := core.Paths().StateDir

	// Check if state directory exists
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return nil // No state to migrate
	}

	// Read all existing state files
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to read state directory")
	}

	unified := &UnifiedState{
		Version:    "1.0.0",
		Components: make(map[string]*ComponentState),
	}

	var filesToRemove []string

	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "state.yaml" {
			continue
		}

		name := entry.Name()

		// Parse filename: "cilium.installed" -> component="cilium", type="installed"
		parts := strings.SplitN(name, ".", 2)
		if len(parts) != 2 {
			continue // Skip files that don't match pattern
		}

		componentName := parts[0]
		stateType := parts[1]

		if stateType != "installed" && stateType != "configured" {
			continue // Skip unknown state types
		}

		// Read file content to extract version
		filePath := filepath.Join(stateDir, name)
		content, err := os.ReadFile(filePath)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to read state file %s", name)
		}

		// Parse content: "installed at version 1.16.0"
		version := parseVersionFromContent(string(content))

		// Add to unified state
		if unified.Components[componentName] == nil {
			unified.Components[componentName] = &ComponentState{}
		}

		entry := &StateEntry{Version: version}
		if stateType == "installed" {
			unified.Components[componentName].Installed = entry
		} else {
			unified.Components[componentName].Configured = entry
		}

		filesToRemove = append(filesToRemove, filePath)
	}

	// Only write if we have components to migrate
	if len(unified.Components) == 0 {
		return nil
	}

	// Write unified state.yaml
	unifiedPath := filepath.Join(stateDir, "state.yaml")
	data, err := yaml.Marshal(unified)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to marshal unified state")
	}

	if err := os.WriteFile(unifiedPath, data, core.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write unified state file")
	}

	// Remove old state files
	for _, filePath := range filesToRemove {
		if err := os.Remove(filePath); err != nil {
			// Log but don't fail - the migration succeeded
			if mctx.Logger != nil {
				mctx.Logger.Warn().Err(err).Str("file", filePath).Msg("Failed to remove old state file")
			}
		}
	}

	return nil
}

func (m *UnifiedStateMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	stateDir := core.Paths().StateDir
	unifiedPath := filepath.Join(stateDir, "state.yaml")

	// Read unified state
	data, err := os.ReadFile(unifiedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Nothing to rollback
		}
		return errorx.IllegalState.Wrap(err, "failed to read unified state for rollback")
	}

	var unified UnifiedState
	if err := yaml.Unmarshal(data, &unified); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to parse unified state for rollback")
	}

	// Restore individual files
	for componentName, state := range unified.Components {
		if state.Installed != nil {
			content := formatStateContent("installed", state.Installed.Version)
			filePath := filepath.Join(stateDir, componentName+".installed")
			if err := os.WriteFile(filePath, []byte(content), core.DefaultFilePerm); err != nil {
				return errorx.IllegalState.Wrap(err, "failed to restore %s.installed", componentName)
			}
		}
		if state.Configured != nil {
			content := formatStateContent("configured", state.Configured.Version)
			filePath := filepath.Join(stateDir, componentName+".configured")
			if err := os.WriteFile(filePath, []byte(content), core.DefaultFilePerm); err != nil {
				return errorx.IllegalState.Wrap(err, "failed to restore %s.configured", componentName)
			}
		}
	}

	// Remove unified state file
	if err := os.Remove(unifiedPath); err != nil {
		if mctx.Logger != nil {
			mctx.Logger.Warn().Err(err).Msg("Failed to remove unified state file during rollback")
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
