// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
)

// VerificationStorageMinVersion is the minimum Block Node version that requires verification storage.
// Block Node v0.26.2 introduced a new PersistentVolume for verification data.
// Upgrading from versions < 0.26.2 to >= 0.26.2 requires uninstall + reinstall.
const VerificationStorageMinVersion = "0.26.2"

// VerificationStorageMigration handles the breaking change introduced in Block Node v0.26.2
// where a new verification storage PV/PVC was added to the StatefulSet.
//
// This migration:
// 1. Creates the verification storage directory on the host
// 2. Uninstalls the current Block Node release
// 3. Recreates PVs/PVCs including the new verification storage
// 4. Reinstalls Block Node with the new chart version
//
// Rollback attempts to reinstall the previous version if the migration fails.
type VerificationStorageMigration struct {
	migration.BaseMigration
}

// NewVerificationStorageMigration creates a new verification storage migration.
func NewVerificationStorageMigration() *VerificationStorageMigration {
	return &VerificationStorageMigration{
		BaseMigration: migration.NewBaseMigration(
			"verification-storage-v0.26.2",
			"Add verification storage PV/PVC required by Block Node v0.26.2+",
			VerificationStorageMinVersion,
		),
	}
}

func (m *VerificationStorageMigration) ID() string {
	return "verification-storage-v0.26.2"
}

func (m *VerificationStorageMigration) Description() string {
	return "Add verification storage PV/PVC required by Block Node v0.26.2+"
}

func (m *VerificationStorageMigration) MinVersion() string {
	return VerificationStorageMinVersion // "0.26.2"
}

func (m *VerificationStorageMigration) Applies(mctx *migration.Context) (bool, error) {
	return m.BaseMigration.Applies(mctx)
}

func (m *VerificationStorageMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	manager, err := GetManager(mctx)
	if err != nil {
		return err
	}

	profile := mctx.GetString(CtxKeyProfile)
	valuesFile := mctx.GetString(CtxKeyValuesFile)
	logger := mctx.Logger

	logger.Info().Msg("Executing verification storage migration")

	// Step 1: Create the verification storage directory
	_, _, _, verificationPath, err := manager.GetStoragePaths()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get storage paths")
	}

	if verificationPath != "" {
		logger.Info().Str("path", verificationPath).Msg("Creating verification storage directory")
		if err := manager.fsManager.CreateDirectory(verificationPath, true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to create verification storage directory")
		}
		if err := manager.fsManager.WritePermissions(verificationPath, 0755, true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to set permissions on verification storage")
		}
	}

	// Step 2: Uninstall the current release
	logger.Info().Msg("Uninstalling current Block Node release")
	if err := manager.UninstallChart(ctx); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to uninstall chart")
	}

	// Step 3: Recreate PVs/PVCs with verification storage
	logger.Info().Msg("Creating PersistentVolumes with verification storage")
	if err := manager.CreatePersistentVolumes(ctx, manager.getTempDir()); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create PVs")
	}

	// Step 4: Reinstall with new chart version
	logger.Info().Msg("Reinstalling Block Node with new chart version")
	valuesFilePath, err := manager.ComputeValuesFile(profile, valuesFile)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to compute values file")
	}

	_, err = manager.InstallChart(ctx, valuesFilePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to install chart")
	}

	logger.Info().Msg("Verification storage migration completed")
	return nil
}

func (m *VerificationStorageMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	manager, err := GetManager(mctx)
	if err != nil {
		return err
	}

	profile := mctx.GetString(CtxKeyProfile)
	valuesFile := mctx.GetString(CtxKeyValuesFile)
	logger := mctx.Logger

	logger.Warn().Msg("Attempting rollback of verification storage migration")

	// Temporarily set version back to installed version
	originalVersion := manager.blockConfig.Version
	manager.blockConfig.Version = mctx.InstalledVersion
	defer func() {
		manager.blockConfig.Version = originalVersion
	}()

	// Try to reinstall the previous version
	valuesFilePath, err := manager.ComputeValuesFile(profile, valuesFile)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "rollback failed: could not compute values file")
	}

	_, err = manager.InstallChart(ctx, valuesFilePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "rollback failed: could not reinstall previous version")
	}

	logger.Info().Str("version", mctx.InstalledVersion).Msg("Rollback successful")
	return nil
}
