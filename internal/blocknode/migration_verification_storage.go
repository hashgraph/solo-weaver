// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"os"
	"path"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/semver"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// VerificationStorageMinVersion is the minimum Block Node version that requires verification storage.
const VerificationStorageMinVersion = "0.26.2"

// Context keys for migration data
const (
	ctxKeyManager        = "blocknode.manager"
	ctxKeyProfile        = "blocknode.profile"
	ctxKeyValuesFile     = "blocknode.valuesFile"
	ctxKeyReuseValues    = "blocknode.reuseValues"
	ctxKeyCapturedValues = "blocknode.capturedValues"
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
		if err := manager.fsManager.WritePermissions(verificationPath, core.DefaultDirOrExecPerm, true); err != nil {
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
	if err := manager.CreatePersistentVolumes(ctx, core.Paths().TempDir); err != nil {
		logger.Error().Err(err).Msg("Failed to create PersistentVolumes")
		return errorx.IllegalState.Wrap(err, "failed to create PVs")
	}

	// Step 5: Determine values file for reinstall
	// IMPORTANT: We must use the profile defaults (nano-values-v0.26.2.yaml or full-values-v0.26.2.yaml)
	// because they have the correct verification persistence settings (create: false, existingClaim).
	// Captured values from older versions won't have these settings, causing the chart to create
	// a duplicate PVC via StatefulSet volumeClaimTemplates.
	var valuesFilePath string

	if valuesFile != "" {
		// User provided a values file, use it (they're responsible for correct settings)
		valuesFilePath, err = manager.ComputeValuesFile(profile, valuesFile)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to compute values file")
		}
		logger.Info().Str("valuesFile", valuesFilePath).Msg("Using user-provided values file")
	} else {
		// Use profile defaults which have correct verification storage settings
		valuesFilePath, err = manager.ComputeValuesFile(profile, "")
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to compute values file")
		}
		logger.Info().Str("valuesFile", valuesFilePath).Msg("Using profile default values file")
	}

	// Step 6: Reinstall with new chart version
	logger.Info().Msg("Reinstalling Block Node with new chart version")
	_, err = manager.InstallChart(ctx, valuesFilePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to install chart")
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
	reuseValues := getReuseValues(mctx)
	capturedValues := getCapturedValues(mctx)
	logger := mctx.Logger
	installedVersion := mctx.GetString(migration.CtxKeyInstalledVersion)

	logger.Warn().Msg("Attempting rollback of verification storage migration")

	// Temporarily set version back to installed version
	originalVersion := manager.blockConfig.Version
	manager.blockConfig.Version = installedVersion
	defer func() {
		manager.blockConfig.Version = originalVersion
	}()

	// Determine values file for rollback reinstall
	var valuesFilePath string
	var tempValuesFile string

	if valuesFile != "" {
		valuesFilePath, err = manager.ComputeValuesFile(profile, valuesFile)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "rollback failed: could not compute values file")
		}
	} else if reuseValues && capturedValues != nil && len(capturedValues) > 0 {
		// Use captured values for rollback
		logger.Info().Int("numKeys", len(capturedValues)).Msg("Using captured release values for rollback")

		tempValuesFile = path.Join(core.Paths().TempDir, "rollback-values.yaml")
		yamlData, err := yaml.Marshal(capturedValues)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "rollback failed: could not marshal captured values")
		}
		if err := os.WriteFile(tempValuesFile, yamlData, core.DefaultFilePerm); err != nil {
			return errorx.IllegalState.Wrap(err, "rollback failed: could not write captured values to temp file")
		}
		valuesFilePath = tempValuesFile
	} else {
		valuesFilePath, err = manager.ComputeValuesFile(profile, "")
		if err != nil {
			return errorx.IllegalState.Wrap(err, "rollback failed: could not compute values file")
		}
	}

	// Try to reinstall the previous version
	_, err = manager.InstallChart(ctx, valuesFilePath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "rollback failed: could not reinstall previous version")
	}

	// Clean up temp values file if created
	if tempValuesFile != "" {
		_ = os.Remove(tempValuesFile)
	}

	logger.Info().Str("version", installedVersion).Msg("Rollback successful")
	return nil
}

// Helper functions for extracting typed data from migration context

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
func getReuseValues(mctx *migration.Context) bool {
	v, ok := mctx.Get(ctxKeyReuseValues)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func getCapturedValues(mctx *migration.Context) map[string]interface{} {
	v, ok := mctx.Get(ctxKeyCapturedValues)
	if !ok {
		return nil
	}
	m, _ := v.(map[string]interface{})
	return m
}
