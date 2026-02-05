// SPDX-License-Identifier: Apache-2.0

// migrations.go provides the component-level orchestration for block node migrations.
//
// This file contains:
//   - InitMigrations(): Registers all block node migrations at startup (called from root.go)
//   - BuildMigrationWorkflow(): Builds an automa workflow for applicable migrations during upgrades
//
// Individual migration implementations are in separate migration_*.go files:
//   - migration_verification_storage.go: Handles v0.26.2 verification storage PV/PVC addition
//
// To add a new migration:
//  1. Create a new migration_<name>.go file implementing the migration.Migration interface
//  2. Register it in InitMigrations() below
//
// See docs/dev/migration-framework.md for the full guide.

package blocknode

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
)

// ComponentBlockNode is the component name for block node migrations.
const ComponentBlockNode = "block-node"

// InitMigrations registers all block node migrations.
// Called once at startup from root.go.
func InitMigrations() {
	migration.Register(ComponentBlockNode, NewVerificationStorageMigration())
}

// BuildMigrationWorkflow returns an automa workflow for executing applicable migrations.
// Returns nil if no migrations are needed (installed version is empty or no applicable migrations).
func BuildMigrationWorkflow(manager *Manager, profile, valuesFile string) (*automa.WorkflowBuilder, error) {
	installedVersion, err := manager.GetInstalledVersion()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to get installed version")
	}

	if installedVersion == "" {
		return nil, nil
	}

	// Build context
	mctx := &migration.Context{
		Component: ComponentBlockNode,
		Logger:    manager.logger,
		Data:      &automa.SyncStateBag{},
	}
	mctx.Data.Set(migration.CtxKeyInstalledVersion, installedVersion)
	mctx.Data.Set(migration.CtxKeyTargetVersion, manager.blockConfig.Version)

	migrations, err := migration.GetApplicableMigrations(ComponentBlockNode, mctx)
	if err != nil {
		return nil, err
	}

	if len(migrations) == 0 {
		return nil, nil
	}

	// Capture release values if needed
	// Add context data
	mctx.Data.Set(ctxKeyManager, manager)
	mctx.Data.Set(ctxKeyProfile, profile)
	mctx.Data.Set(ctxKeyValuesFile, valuesFile)

	return migration.MigrationsToWorkflow(migrations, mctx), nil
}
