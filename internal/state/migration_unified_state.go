// SPDX-License-Identifier: Apache-2.0

// migration_unified_state.go implements the unified state file migration.
//
// This migration consolidates multiple legacy state files (*.installed, *.configured)
// into the unified State model by writing SoftwareState entries into
// MachineState.Software: into state.yaml when it exists, otherwise into
// machine.yaml. The marker files are removed once the merge is persisted.

package state

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
	htime "helm.sh/helm/v3/pkg/time"
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

// Execute merges every marker file into the persisted state and removes the
// marker files on success.
func (m *UnifiedStateMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	files, err := findLegacyStateFiles(models.Paths().StateDir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}

	markers, err := readLegacyMarkers(files)
	if err != nil {
		return err
	}

	legacyPath := filepath.Join(models.Paths().StateDir, StateFileName)
	b, readErr := os.ReadFile(legacyPath)
	switch {
	case readErr == nil:
		// The startup migrations after this one rewrite state.yaml and the
		// per-component split runs last, so merge into state.yaml itself, and
		// at the node level: decoding into today's State would silently drop
		// any older-schema field a later migration still has to convert.
		out, err := mergeMarkersIntoStateYAML(b, markers)
		if err != nil {
			return errorx.IllegalFormat.Wrap(err, "failed to merge marker files into %s", legacyPath)
		}
		if err := atomicWriteFile(legacyPath, out); err != nil {
			return errorx.ExternalError.Wrap(err, "failed to write migrated state file %s", legacyPath)
		}

	case os.IsNotExist(readErr):
		// Already split, or old enough to have only marker files: the software
		// entries belong to machine.yaml, so merge into that alone.
		sm, err := NewStateManager()
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to create state manager for unified-state migration")
		}
		if err := sm.Refresh(); err != nil && !errorx.IsOfType(err, NotFoundError) {
			return errorx.IllegalState.Wrap(err, "failed to read state for unified-state migration")
		}
		current := sm.State()
		for _, mk := range markers {
			current = SetSoftwareState(current, mk.component, mk.applyTo(GetSoftwareState(current, mk.component)))
		}
		if err := sm.Set(current).FlushScoped(ComponentMachine); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to flush migrated state")
		}

	default:
		return errorx.IllegalState.Wrap(readErr, "failed to read existing state file %s", legacyPath)
	}

	// Remove legacy files now that the state has been persisted.
	for _, fp := range files {
		if removeErr := os.Remove(fp); removeErr != nil && mctx != nil && mctx.Logger != nil {
			mctx.Logger.Warn().Err(removeErr).Str("file", fp).Msg("Failed to remove legacy state file after migration")
		}
	}

	return nil
}

// legacyMarker is one parsed *.installed / *.configured marker file.
type legacyMarker struct {
	component string
	stateType string
	version   string
}

func readLegacyMarkers(files []string) ([]legacyMarker, error) {
	var markers []legacyMarker
	for _, fp := range files {
		base := filepath.Base(fp)
		parts := strings.SplitN(base, ".", 2)
		if len(parts) != 2 {
			continue
		}
		content, err := os.ReadFile(fp)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to read legacy state file %s", base)
		}
		markers = append(markers, legacyMarker{
			component: parts[0],
			stateType: parts[1],
			version:   parseVersionFromContent(string(content)),
		})
	}
	return markers, nil
}

func (mk legacyMarker) applyTo(sw SoftwareState) SoftwareState {
	sw.Version = mk.version
	switch mk.stateType {
	case "installed":
		sw.Installed = true
	case "configured":
		sw.Configured = true
	}
	return sw
}

// mergeMarkersIntoStateYAML records each marker's software entry under
// state.machineState.software in b, leaving every other node untouched.
func mergeMarkersIntoStateYAML(b []byte, markers []legacyMarker) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	root := rootMappingNode(&doc)
	if root == nil {
		return nil, errorx.IllegalFormat.New("state file has no top-level mapping")
	}
	software := ensureMapping(ensureMapping(ensureMapping(root, "state"), "machineState"), "software")

	for _, mk := range markers {
		sw := SoftwareState{}
		if existing := mappingValue(software, mk.component); existing != nil {
			if err := existing.Decode(&sw); err != nil {
				return nil, err
			}
		}
		sw = mk.applyTo(sw)
		sw.Name = mk.component
		sw.LastSync = htime.Now()

		var n yaml.Node
		if err := n.Encode(sw); err != nil {
			return nil, err
		}
		setMappingValue(software, mk.component, &n)
	}
	return yaml.Marshal(&doc)
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
