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
		if err := manager.fsManager.WritePermissions(storagePath, models.DefaultDirOrExecPerm, true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to set permissions on %s storage", m.storage.Name)
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
