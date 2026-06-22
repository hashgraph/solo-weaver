// SPDX-License-Identifier: Apache-2.0

// migration_storage.go implements a generic storage migration for Block Node.
//
// This handles the breaking change pattern where a new PersistentVolume is added
// to the Block Node StatefulSet across a version boundary.
//
// The migration uses a two-phase approach orchestrated by BuildMigrationWorkflow:
//
//   - Phase 1 (Execute per migration): Creates storage directory + PV/PVC only.
//   - Phase 2 (single upgrade step):   Deletes StatefulSet (orphan cascade) and
//     performs one Helm upgrade to the final target version.
//
// This avoids intermediate Helm upgrades, version-swapping tricks, and repeated
// StatefulSet deletions when multiple storage migrations apply at once
// (e.g., upgrading from 0.26.0 → 0.28.1 triggers both verification and plugins).
//
// Concrete instances are registered in migrations.go via InitMigrations().

package blocknode

import (
	"context"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// Context keys for migration data
const (
	ctxKeyManager    = "blocknode.manager"
	ctxKeyProfile    = "blocknode.profile"
	ctxKeyValuesFile = "blocknode.valuesFile"
)

// StorageMigration handles the breaking change pattern where a new storage PV/PVC
// is added to the Block Node StatefulSet. It is parameterized by the OptionalStorage
// descriptor, allowing a single implementation to serve all storage migrations.
//
// Execute only creates the storage directory and PV/PVC — no Helm upgrade is performed.
// The single upgrade is appended as a final workflow step by BuildMigrationWorkflow.
type StorageMigration struct {
	migration.VersionMigration
	storage OptionalStorage
}

// NewStorageMigration creates a new storage migration for the given optional storage entry.
func NewStorageMigration(optStorage OptionalStorage) *StorageMigration {
	return &StorageMigration{
		VersionMigration: migration.NewVersionMigration(
			optStorage.Name+"-storage-v"+optStorage.MinVersion,
			"Add "+optStorage.Name+" storage PV/PVC required by Block Node v"+optStorage.MinVersion+"+",
			optStorage.MinVersion,
		),
		storage: optStorage,
	}
}

// Applies extends VersionMigration.Applies with a MaxVersion guard: a storage
// that has been retired in newer chart versions must not be re-created when an
// operator skips across the retirement boundary. Concretely, if a cluster
// installed at 0.25.0 upgrades directly to >=0.36.0, the generic VersionMigration
// check (installed < 0.26.2 && target >= 0.26.2) reports the verification
// migration as applicable, but at target=0.36.0 verification has been retired
// (MaxVersion=0.36.0) and creating its PV/PVC would leave an orphan resource.
// The OptionalStorage's RequiredByVersion captures the [MinVersion, MaxVersion)
// range; AND-ing it in keeps the migration registry version-correct without
// duplicating the bound on every individual migration.
func (m *StorageMigration) Applies(mctx *migration.Context) (bool, error) {
	applies, err := m.VersionMigration.Applies(mctx)
	if err != nil || !applies {
		return applies, err
	}

	targetVersion, _ := mctx.Data.String(migration.CtxKeyTargetVersion)
	return m.storage.RequiredByVersion(targetVersion), nil
}

// Execute creates the storage directory on the host and the PV/PVC in Kubernetes.
// It does NOT perform a Helm upgrade — that is handled once after all migrations
// complete, by the upgrade step appended in BuildMigrationWorkflow.
func (m *StorageMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	manager, err := getManager(mctx)
	if err != nil {
		return err
	}

	logger := mctx.Logger
	logger.Info().Str("storage", m.storage.Name).Msg("Creating storage directory and PV/PVC")

	// Create the storage directory on host
	storagePath, _, err := manager.resolveOptionalStoragePathAndSize(m.storage)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to resolve %s storage path", m.storage.Name)
	}

	if storagePath != "" {
		logger.Info().Str("path", storagePath).Str("storage", m.storage.Name).Msg("Creating storage directory")
		if err := manager.fsManager.CreateDirectory(storagePath, true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to create %s storage directory", m.storage.Name)
		}
		if err := manager.fsManager.WritePermissions(storagePath, models.DefaultStorageDirPerm, true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to set permissions on %s storage", m.storage.Name)
		}

		if err := manager.fsManager.WriteOwnerByName(storagePath, config.HederaUserName(), config.HederaGroupName(), true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to set ownership on %s storage", m.storage.Name)
		}
	}

	// Create the PV/PVC
	logger.Info().Str("storage", m.storage.Name).Msg("Creating storage PV/PVC")
	if err := manager.CreateOptionalStorage(ctx, models.Paths().TempDir, m.storage); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create %s storage PV/PVC", m.storage.Name)
	}

	logger.Info().Str("storage", m.storage.Name).Msg("Storage PV/PVC created successfully")
	return nil
}

// Rollback cleans up the PV/PVC created by this migration. Best-effort.
// No Helm downgrade is needed because the chart was never upgraded during Execute.
func (m *StorageMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	manager, err := getManager(mctx)
	if err != nil {
		return err
	}

	logger := mctx.Logger
	logger.Warn().Str("storage", m.storage.Name).Msg("Rolling back storage migration")

	if err := manager.kubeClient.DeletePVC(ctx, manager.blockNodeInputs.Namespace, m.storage.PVCName); err != nil {
		logger.Warn().Err(err).Str("pvc", m.storage.PVCName).Msg("Could not delete PVC during rollback")
	}
	if err := manager.kubeClient.DeletePV(ctx, m.storage.PVName); err != nil {
		logger.Warn().Err(err).Str("pv", m.storage.PVName).Msg("Could not delete PV during rollback")
	}

	logger.Info().Str("storage", m.storage.Name).Msg("Storage rollback completed")
	return nil
}

// getManager extracts the Manager from the migration context.
func getManager(mctx *migration.Context) (*Manager, error) {
	v, ok := mctx.Data.Get(ctxKeyManager)
	if !ok {
		return nil, errorx.IllegalState.New("manager not found in migration context")
	}
	m, ok := v.(*Manager)
	if !ok {
		return nil, errorx.IllegalState.New("invalid manager type in migration context")
	}
	return m, nil
}

// NewVerificationStorageMigration creates the verification storage migration.
func NewVerificationStorageMigration() *StorageMigration {
	for _, optStor := range optionalStorages {
		if optStor.Name == "verification" {
			return NewStorageMigration(optStor)
		}
	}
	panic("verification storage not found in optional storage registry")
}

// NewPluginsStorageMigration creates the plugins storage migration.
func NewPluginsStorageMigration() *StorageMigration {
	for _, optStor := range optionalStorages {
		if optStor.Name == "plugins" {
			return NewStorageMigration(optStor)
		}
	}
	panic("plugins storage not found in optional storage registry")
}

// NewApplicationStateMigration creates the application-state storage migration.
//
// At BlockNodeApplicationStateRequiredVersion (0.37.0) the Helm chart starts
// mounting the applicationStateFacility volume (hiero-ledger/hiero-block-node#3025)
// and stops mounting the verification volume in lockstep. The migration creates
// the new PV/PVC; existing verification PV/PVC objects are left in place — the
// chart no longer references them so they become orphan state, and cleanup is
// a manual operator step.
func NewApplicationStateMigration() *StorageMigration {
	for _, optStor := range optionalStorages {
		if optStor.Name == "application-state" {
			return NewStorageMigration(optStor)
		}
	}
	panic("application-state storage not found in optional storage registry")
}
