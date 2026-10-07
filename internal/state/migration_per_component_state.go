// SPDX-License-Identifier: Apache-2.0

// migration_per_component_state.go splits the single state.yaml into the
// per-component files internal/bll.BaseHandler.FlushScoped writes:
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

// Applies returns true whenever a legacy state.yaml exists, regardless of
// whether the component files are already present. A host already fully
// split never has a state.yaml (Execute always renames or removes it on
// success), and a fresh install never creates one — so "legacy file exists"
// alone is both necessary and sufficient to mean "not done yet".
//
// This also covers a crash strictly between Execute's last component-file
// write and its rename/remove of the legacy file: at that point every
// component file is already correct, but state.yaml is still present, so
// Applies() must stay true to retry the rename on the very next invocation —
// checking "any component missing" instead would miss this window, since
// nothing is missing. Execute is cheap to retry in that case: it clears and
// rewrites identical content, then the rename succeeds (or fails again,
// handled the same way as any other rename failure).
func (m *PerComponentStateMigration) Applies(_ *migration.Context) (bool, error) {
	legacyPath := m.legacyStateFilePath()
	if _, err := os.Stat(legacyPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errorx.ExternalError.Wrap(err, "failed to stat legacy state file")
	}
	return true, nil
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
		// The split already succeeded and legacyPath is what Applies() checks
		// for next time — leaving it in place would make Applies() true again
		// the moment any component file is later lost for an unrelated reason
		// (a bug, a bad actor, a disk issue), and Execute would then delete
		// every current component file and regenerate all of them from this
		// now-stale legacy content, discarding everything written since. Fall
		// back to removing it outright: its content is already safely
		// duplicated in the component files just written, so losing the
		// backup copy is a far smaller cost than that failure mode.
		if mctx != nil && mctx.Logger != nil {
			mctx.Logger.Warn().Err(err).Str("legacyStateFile", legacyPath).
				Msg("could not rename the legacy state file to its backup name; removing it instead")
		}
		if removeErr := os.Remove(legacyPath); removeErr != nil {
			return errorx.ExternalError.Wrap(removeErr,
				"wrote the new per-component files but could not remove or rename the now-stale legacy state file %s; remove it manually before running any further commands",
				legacyPath)
		}
	}
	return nil
}

// Rollback restores a single state.yaml and removes the per-component files,
// undoing Execute. When Execute's backup survived (the normal case), this
// restores that exact pre-migration snapshot verbatim — not a recomposition
// of the current component files, which would stamp the running binary's
// version into provisioner.version rather than whatever version was actually
// recorded before the split. If the backup is missing (Execute's rename
// failed and it fell back to removing the legacy file, or there was never a
// legacy file to begin with), this falls back to a best-effort recomposition
// from the current component files, which may not fully restore — the same
// contract every Migration.Rollback has.
func (m *PerComponentStateMigration) Rollback(_ context.Context, _ *migration.Context) error {
	legacyPath := m.legacyStateFilePath()
	dir := filepath.Dir(legacyPath)
	backupPath := legacyPath + legacyStateBackupSuffix

	if b, err := os.ReadFile(backupPath); err == nil {
		if err := atomicWriteFile(legacyPath, b); err != nil {
			return errorx.ExternalError.Wrap(err, "failed to restore legacy state file from backup %s", backupPath)
		}
	} else if os.IsNotExist(err) {
		if err := recomposeLegacyStateFromComponents(legacyPath); err != nil {
			return err
		}
	} else {
		return errorx.ExternalError.Wrap(err, "failed to check for a per-component migration backup at %s", backupPath)
	}

	for _, id := range AllComponentIDs {
		if err := os.Remove(componentFilePath(dir, id)); err != nil && !os.IsNotExist(err) {
			return errorx.ExternalError.Wrap(err, "failed to remove %s state file during rollback", id)
		}
	}
	// Best-effort: the backup has now been restored (or recomposed in its
	// absence), so it no longer serves a purpose.
	_ = os.Remove(backupPath)
	return nil
}

// recomposeLegacyStateFromComponents rebuilds a legacy state file from the
// current per-component files when no Execute-created backup is available to
// restore verbatim.
func recomposeLegacyStateFromComponents(legacyPath string) error {
	sm, err := NewStateManager(WithStateFile(legacyPath))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager for per-component rollback")
	}
	if err := sm.Refresh(); err != nil && !errorx.IsOfType(err, NotFoundError) {
		return errorx.IllegalState.Wrap(err, "failed to read per-component state files for rollback")
	}

	composed := sm.State()
	hash, err := stateContentHash(composed)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to canonicalize state for rollback")
	}
	composed.Hash = hash
	composed.HashAlgo = "sha256"

	b, err := yaml.Marshal(composed)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal recomposed state.yaml for rollback")
	}
	if err := atomicWriteFile(legacyPath, b); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write recomposed state.yaml for rollback")
	}
	return nil
}
