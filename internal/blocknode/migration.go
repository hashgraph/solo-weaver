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
	CtxKeyManager    = "blocknode.manager"
	CtxKeyProfile    = "blocknode.profile"
	CtxKeyValuesFile = "blocknode.valuesFile"
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
	mctx := NewBlockNodeMigrationContext(manager, installedVersion, targetVersion, "", "", logger)
	return mm.RequiresMigration(mctx)
}

// ExecuteMigration handles the migration flow for breaking chart changes.
// It delegates to the MigrationManager to execute all applicable migrations in order.
//
// IMPORTANT: This operation is NOT atomic. See migration.Manager.Execute for details
// on the migration process and rollback behavior.
func ExecuteMigration(ctx context.Context, manager *Manager, profile string, valuesFile string) error {
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
		Msg("Starting migration from installed version to target version")

	// Create migration context with all required data
	mctx := NewBlockNodeMigrationContext(
		manager,
		installedVersion,
		manager.blockConfig.Version,
		profile,
		valuesFile,
		logger,
	)

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

// NewBlockNodeMigrationContext creates a migration context for Block Node operations.
func NewBlockNodeMigrationContext(
	manager *Manager,
	installedVersion string,
	targetVersion string,
	profile string,
	valuesFile string,
	logger *zerolog.Logger,
) *migration.Context {
	ctx := &migration.Context{
		Component:        "block-node",
		InstalledVersion: installedVersion,
		TargetVersion:    targetVersion,
		Logger:           logger,
		Data:             make(map[string]interface{}),
	}

	ctx.Set(CtxKeyManager, manager)
	ctx.Set(CtxKeyProfile, profile)
	ctx.Set(CtxKeyValuesFile, valuesFile)

	return ctx
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
