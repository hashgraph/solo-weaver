// SPDX-License-Identifier: Apache-2.0

// migration_per_component_state.go splits the single state.yaml into the
// per-component files internal/bll.BaseHandler.FlushScoped writes (#1231):
// machine.yaml, cluster.yaml, blocknode.yaml, consensus.yaml, teleport.yaml,
// each in the same state directory. It must run after every other
// ScopeStartup migration in this package (migration_unified_state.go,
// migration_helm_release_schema_v2.go, migration_mgmt_ports_v1.go), which
// still assume and rewrite the single-file legacy shape — see
// cmd/cli/commands/root.go RegisterMigrations.
//
// Execute never deletes state.yaml: once every component file is written, the
// legacy file is renamed to state.yaml.pre-v1231, a safety net Rollback reads
// back from and an operator can delete once satisfied the split is correct.

package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// legacyStateBackupSuffix names the renamed legacy file Execute leaves behind.
const legacyStateBackupSuffix = ".pre-v1231"

// PerComponentStateMigration splits a legacy single state.yaml into one file
// per component.
type PerComponentStateMigration struct {
	// legacyStateFileOverride, when non-empty, is used as the legacy state file
	// path instead of the production path derived from models.Paths().StateDir.
	// Intended for unit tests only.
	legacyStateFileOverride string
}

// NewPerComponentStateMigration returns a new PerComponentStateMigration.
func NewPerComponentStateMigration() *PerComponentStateMigration {
	return &PerComponentStateMigration{}
}

func (m *PerComponentStateMigration) ID() string { return "per-component-state-v1231" }
func (m *PerComponentStateMigration) Description() string {
	return "Split the single state.yaml into one file per component (machine, cluster, blocknode, consensus, teleport)"
}

func (m *PerComponentStateMigration) legacyStateFilePath() string {
	if m.legacyStateFileOverride != "" {
		return m.legacyStateFileOverride
	}
	return filepath.Join(models.Paths().StateDir, StateFileName)
}

// Applies returns true when a legacy state.yaml exists and at least one
// component file has not yet been split out of it. A host already fully split
// (or a fresh install, which writes component files directly and never
// creates state.yaml) does not re-run this.
func (m *PerComponentStateMigration) Applies(_ *migration.Context) (bool, error) {
	legacyPath := m.legacyStateFilePath()
	if _, err := os.Stat(legacyPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errorx.ExternalError.Wrap(err, "failed to stat legacy state file")
	}

	dir := filepath.Dir(legacyPath)
	for _, id := range AllComponentIDs {
		if _, err := os.Stat(componentFilePath(dir, id)); os.IsNotExist(err) {
			return true, nil
		}
	}
	return false, nil
}

// Execute reads the legacy state.yaml, writes every component's section to
// its own file (establishing a fresh per-file hash baseline for each, since
// none existed before), and only then renames the legacy file out of the way.
// If writing the component files fails partway through, state.yaml is left
// untouched, so a re-run is idempotent: Applies() is still true and Execute
// starts over from the unmodified legacy content.
func (m *PerComponentStateMigration) Execute(_ context.Context, mctx *migration.Context) error {
	legacyPath := m.legacyStateFilePath()

	b, err := os.ReadFile(legacyPath)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to read legacy state file %s", legacyPath)
	}

	var legacy State
	if err := yaml.Unmarshal(b, &legacy); err != nil {
		return errorx.IllegalFormat.Wrap(err, "legacy state file %s is not parseable YAML", legacyPath)
	}
	legacy.StateFile = legacyPath

	// Applies() only reports true while the legacy file still exists, so any
	// component file found at this point is a partial leftover from a prior
	// Execute that crashed before the rename below — not something flushing
	// needs a baseline to protect, since nothing else writes these files while
	// state.yaml is still present. Clear it so the flush below is a plain
	// first write for every component, matching a fresh install.
	dir := filepath.Dir(legacyPath)
	for _, id := range AllComponentIDs {
		if err := os.Remove(componentFilePath(dir, id)); err != nil && !os.IsNotExist(err) {
			return errorx.ExternalError.Wrap(err, "failed to remove partial %s state file before migration", id)
		}
	}

	sm, err := NewStateManager(WithState(legacy))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager for per-component migration")
	}
	if err := sm.FlushState(); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write per-component state files")
	}

	backupPath := legacyPath + legacyStateBackupSuffix
	if err := os.Rename(legacyPath, backupPath); err != nil {
		// The split already succeeded and is what every command now reads; a
		// failed rename only leaves a redundant legacy file behind, so this is
		// logged, not fatal.
		if mctx.Logger != nil {
			mctx.Logger.Warn().Err(err).Str("legacyStateFile", legacyPath).
				Msg("per-component migration wrote the new files but could not rename the legacy state.yaml out of the way; remove it manually once verified")
		}
	}
	return nil
}

// Rollback recomposes a single state.yaml from the per-component files and
// removes them, undoing Execute. Best-effort: any change to a component file
// made after Execute (by a command or by a later migration) is included in
// the recomposed legacy file, same as the "may not fully restore" contract
// every Migration.Rollback has.
func (m *PerComponentStateMigration) Rollback(_ context.Context, _ *migration.Context) error {
	legacyPath := m.legacyStateFilePath()
	dir := filepath.Dir(legacyPath)

	sm, err := NewStateManager(WithStateFile(legacyPath))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager for per-component rollback")
	}
	if err := sm.Refresh(); err != nil && !errorx.IsOfType(err, NotFoundError) {
		return errorx.IllegalState.Wrap(err, "failed to read per-component state files for rollback")
	}

	composed := sm.State()
	canonical, err := canonicalJSON(composed.Hashable())
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to canonicalize state for rollback")
	}
	sum := sha256.Sum256(canonical)
	composed.Hash = hex.EncodeToString(sum[:])
	composed.HashAlgo = "sha256"

	b, err := yaml.Marshal(composed)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal recomposed state.yaml for rollback")
	}
	if err := atomicWriteFile(legacyPath, b); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write recomposed state.yaml for rollback")
	}

	for _, id := range AllComponentIDs {
		if err := os.Remove(componentFilePath(dir, id)); err != nil && !os.IsNotExist(err) {
			return errorx.ExternalError.Wrap(err, "failed to remove %s state file during rollback", id)
		}
	}
	return nil
}
