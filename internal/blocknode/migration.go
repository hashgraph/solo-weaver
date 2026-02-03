// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
)

// Context keys for BlockNode-specific migration data
const (
	CtxKeyManager        = "blocknode.manager"
	CtxKeyProfile        = "blocknode.profile"
	CtxKeyValuesFile     = "blocknode.valuesFile"
	CtxKeyReuseValues    = "blocknode.reuseValues"
	CtxKeyCapturedValues = "blocknode.capturedValues"
)

// RequiresMigration checks if upgrading from the currently installed version to the target version
// requires migrations due to breaking changes.
func RequiresMigration(manager *Manager) (bool, string, error) {
	logger := manager.logger

	installedVersion, err := manager.GetInstalledVersion()
	if err != nil {
		return false, "", err
	}

	// If not installed, no migration needed
	if installedVersion == "" {
		logger.Info().Msg("Block Node not installed, no migration needed")
		return false, "", nil
	}

	targetVersion := manager.blockConfig.Version

	logger.Info().
		Str("installedVersion", installedVersion).
		Str("targetVersion", targetVersion).
		Msg("Checking if migration is required for upgrade")

	// Use MigrationManager to check for applicable migrations
	mm := NewBlockNodeMigrationManager(logger)
	mctx := &migration.Context{
		InstalledVersion: installedVersion,
		TargetVersion:    targetVersion,
	}
	return mm.RequiresMigration(mctx)
}

// ExecuteMigration handles the migration flow for breaking chart changes.
// It delegates to the MigrationManager to execute all applicable migrations in order.
//
// When reuseValues is true and valuesFile is empty, the function captures the currently
// installed release's user-supplied values before uninstall and uses them for reinstall.
// This preserves user customizations that would otherwise be lost during the migration.
//
// IMPORTANT: This operation is NOT atomic. See migration.Manager.Execute for details
// on the migration process and rollback behavior.
func ExecuteMigration(ctx context.Context, manager *Manager, profile string, valuesFile string, reuseValues bool) error {
	logger := manager.logger
	logger.Info().Msg("Executing migrations for breaking chart changes")

	// Get the currently installed version
	installedVersion, err := manager.GetInstalledVersion()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get installed version for migration")
	}
	if installedVersion == "" {
		return errorx.IllegalState.New("cannot perform migration: no Block Node release is currently installed")
	}

	logger.Info().
		Str("installedVersion", installedVersion).
		Str("targetVersion", manager.blockConfig.Version).
		Bool("reuseValues", reuseValues).
		Msg("Starting migration from installed version to target version")

	// Capture current release values before migration if reuseValues is true
	// and no custom values file is provided
	var capturedValues map[string]interface{}
	if reuseValues && valuesFile == "" {
		logger.Info().Msg("Capturing current release values for reuse during migration")
		var err error
		capturedValues, err = manager.GetReleaseValues()
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to capture release values for migration")
		}
		if capturedValues != nil {
			logger.Info().Int("numKeys", len(capturedValues)).Msg("Captured release values")
		}
	}

	// Create migration context with all required data
	mctx := &migration.Context{
		Component:        "block-node",
		InstalledVersion: installedVersion,
		TargetVersion:    manager.blockConfig.Version,
		Logger:           logger,
		Data:             make(map[string]interface{}),
	}
	mctx.Set(CtxKeyManager, manager)
	mctx.Set(CtxKeyProfile, profile)
	mctx.Set(CtxKeyValuesFile, valuesFile)
	mctx.Set(CtxKeyReuseValues, reuseValues)

	// Store captured values in context for use by migrations
	if capturedValues != nil {
		mctx.Set(CtxKeyCapturedValues, capturedValues)
	}

	// Execute migrations using the MigrationManager
	mm := NewBlockNodeMigrationManager(logger)
	return mm.Execute(ctx, mctx)
}

// NewBlockNodeMigrationManager creates a migration manager for Block Node upgrades.
// It registers all known Block Node migrations in chronological order.
func NewBlockNodeMigrationManager(logger *zerolog.Logger) *migration.Manager {
	m := migration.NewManager(
		migration.WithLogger(logger),
		migration.WithComponent("block-node"),
	)

	// Register all known migrations in chronological order
	m.Register(NewVerificationStorageMigration())

	// Future migrations would be registered here:
	// m.Register(NewSomeOtherMigration())

	return m
}

// GetManager retrieves the Block Node manager from migration context.
func GetManager(ctx *migration.Context) (*Manager, error) {
	v, ok := ctx.Get(CtxKeyManager)
	if !ok {
		return nil, errorx.IllegalState.New("block node manager not found in migration context")
	}
	manager, ok := v.(*Manager)
	if !ok {
		return nil, errorx.IllegalState.New("invalid block node manager type in migration context")
	}
	return manager, nil
}

// GetReuseValues retrieves the reuseValues flag from migration context.
func GetReuseValues(ctx *migration.Context) bool {
	v, ok := ctx.Get(CtxKeyReuseValues)
	if !ok {
		return false
	}
	reuseValues, ok := v.(bool)
	if !ok {
		return false
	}
	return reuseValues
}

// GetCapturedValues retrieves the captured release values from migration context.
func GetCapturedValues(ctx *migration.Context) map[string]interface{} {
	v, ok := ctx.Get(CtxKeyCapturedValues)
	if !ok {
		return nil
	}
	values, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	return values
}
