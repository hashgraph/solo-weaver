// SPDX-License-Identifier: Apache-2.0

// migration_verification_storage.go implements the verification storage migration for Block Node v0.26.2+.
//
// This migration handles the breaking change where a new verification PersistentVolume was added
// to the Block Node StatefulSet. It:
//   - Creates the verification storage directory on the host
//   - Creates the verification PV/PVC (existing storage PVs are preserved)
//   - Ensures the Helm values file has correct verification persistence settings
//   - Performs an in-place Helm upgrade
//
// This file is registered in migrations.go via InitMigrations().

package blocknode

import (
	"context"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/semver"
	"github.com/joomcode/errorx"
)

// VerificationStorageMinVersion is the minimum Block Node version that requires verification storage.
const VerificationStorageMinVersion = "0.26.2"

// Context keys for migration data
const (
	ctxKeyManager    = "blocknode.manager"
	ctxKeyProfile    = "blocknode.profile"
	ctxKeyValuesFile = "blocknode.valuesFile"
)

// requiresVerificationStorage checks if the target version requires verification storage.
// Returns true if the target version is >= 0.26.2, false otherwise.
func (m *Manager) requiresVerificationStorage() bool {
	targetVersion := m.blockConfig.Version

	target, err := semver.NewSemver(targetVersion)
	if err != nil {
		// If we can't parse the version, assume it doesn't need verification storage
		// to maintain backward compatibility
		m.logger.Warn().
			Err(err).
			Str("version", targetVersion).
			Msg("Could not parse target version, assuming no verification storage needed")
		return false
	}

	minVersion, err := semver.NewSemver(VerificationStorageMinVersion)
	if err != nil {
		m.logger.Panic().
			Err(err).
			Str("version", VerificationStorageMinVersion).
			Msg("Invalid VerificationStorageMinVersion constant; this is a programming error")
		return false
	}

	// Requires verification storage if target >= 0.26.2
	return !target.LessThan(minVersion)
}

// VerificationStorageMigration handles the breaking change introduced in Block Node v0.26.2
// where a new verification storage PV/PVC was added to the StatefulSet.
type VerificationStorageMigration struct {
	migration.VersionMigration
}

// NewVerificationStorageMigration creates a new verification storage migration.
func NewVerificationStorageMigration() *VerificationStorageMigration {
	return &VerificationStorageMigration{
		VersionMigration: migration.NewVersionMigration(
			"verification-storage-v0.26.2",
			"Add verification storage PV/PVC required by Block Node v0.26.2+",
			VerificationStorageMinVersion,
		),
	}
}

func (m *VerificationStorageMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	manager, err := getManager(mctx)
	if err != nil {
		return err
	}

	profile := mctx.GetString(ctxKeyProfile)
	valuesFile := mctx.GetString(ctxKeyValuesFile)
	logger := mctx.Logger

	logger.Info().Msg("Executing verification storage migration")

	// Step 1: Create the verification storage directory on host
	_, _, _, verificationPath, err := manager.GetStoragePaths()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get storage paths")
	}

	if verificationPath != "" {
		logger.Info().Str("path", verificationPath).Msg("Creating verification storage directory")
		if err := manager.fsManager.CreateDirectory(verificationPath, true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to create verification storage directory")
		}
		if err := manager.fsManager.WritePermissions(verificationPath, core.DefaultDirOrExecPerm, true); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to set permissions on verification storage")
		}
	}

	// Step 2: Create only the verification PV/PVC (existing PVs are kept)
	logger.Info().Msg("Creating verification storage PV/PVC")
	if err := manager.CreateVerificationStorage(ctx, core.Paths().TempDir); err != nil {
		logger.Error().Err(err).Msg("Failed to create verification storage")
		return errorx.IllegalState.Wrap(err, "failed to create verification storage")
	}

	// Step 3: Determine values file for upgrade
	// IMPORTANT: We must use the profile defaults (nano-values-v0.26.2.yaml or full-values-v0.26.2.yaml)
	// because they have the correct verification persistence settings (create: false, existingClaim).
	var valuesFilePath string

	if valuesFile != "" {
		valuesFilePath, err = manager.ComputeValuesFile(profile, valuesFile)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to compute values file")
		}
		logger.Info().Str("valuesFile", valuesFilePath).Msg("Using user-provided values file")
	} else {
		valuesFilePath, err = manager.ComputeValuesFile(profile, "")
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to compute values file")
		}
		logger.Info().Str("valuesFile", valuesFilePath).Msg("Using profile default values file")
	}

	// Step 4: Upgrade the Helm chart (not uninstall/reinstall)
	logger.Info().Msg("Upgrading Block Node chart to new version")
	if err := manager.UpgradeChart(ctx, valuesFilePath, false); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to upgrade chart")
	}

	logger.Info().Msg("Verification storage migration completed")
	return nil
}

func (m *VerificationStorageMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	manager, err := getManager(mctx)
	if err != nil {
		return err
	}

	profile := mctx.GetString(ctxKeyProfile)
	valuesFile := mctx.GetString(ctxKeyValuesFile)
	logger := mctx.Logger
	installedVersion := mctx.GetString(migration.CtxKeyInstalledVersion)

	logger.Warn().Msg("Attempting rollback of verification storage migration")

	// Temporarily set version back to installed version for ComputeValuesFile
	originalVersion := manager.blockConfig.Version
	manager.blockConfig.Version = installedVersion
	defer func() {
		manager.blockConfig.Version = originalVersion
	}()

	// Determine values file for rollback
	var valuesFilePath string

	if valuesFile != "" {
		valuesFilePath, err = manager.ComputeValuesFile(profile, valuesFile)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "rollback failed: could not compute values file")
		}
	} else {
		valuesFilePath, err = manager.ComputeValuesFile(profile, "")
		if err != nil {
			return errorx.IllegalState.Wrap(err, "rollback failed: could not compute values file")
		}
	}

	// Downgrade to the previous version
	logger.Info().Str("version", installedVersion).Msg("Downgrading to previous version")
	if err := manager.UpgradeChart(ctx, valuesFilePath, false); err != nil {
		return errorx.IllegalState.Wrap(err, "rollback failed: could not downgrade chart")
	}

	// Clean up verification storage PV/PVC (optional, best-effort)
	logger.Info().Msg("Cleaning up verification storage PV/PVC")
	if err := manager.kubeClient.DeletePVC(ctx, manager.blockConfig.Namespace, "verification-storage-pvc"); err != nil {
		logger.Warn().Err(err).Msg("Could not delete verification PVC")
	}
	if err := manager.kubeClient.DeletePV(ctx, "verification-storage-pv"); err != nil {
		logger.Warn().Err(err).Msg("Could not delete verification PV")
	}

	logger.Info().Str("version", installedVersion).Msg("Rollback successful")
	return nil
}

// Helper function for extracting manager from migration context

func getManager(mctx *migration.Context) (*Manager, error) {
	v, ok := mctx.Get(ctxKeyManager)
	if !ok {
		return nil, errorx.IllegalState.New("manager not found in context")
	}
	m, ok := v.(*Manager)
	if !ok {
		return nil, errorx.IllegalState.New("invalid manager type")
	}
	return m, nil
}
