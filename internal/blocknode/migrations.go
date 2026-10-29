// SPDX-License-Identifier: Apache-2.0

// migrations.go provides the component-level orchestration for block node storage
// migrations.
//
// This file contains:
//   - BuildStorageMigrationWorkflow(): builds an automa workflow of the storage-prep
//     steps that must run before a breaking-boundary chart upgrade.
//
// Each applicable StorageMigration creates its storage dir + PV/PVC for an optional
// storage that a newer chart version requires (verification @0.26.2, plugins
// @0.28.1, application-state @0.37.0). Under the operator model (#1319) the chart
// move itself is a HelmCapsule patch driven by the upgrade step
// (steps.UpgradeBlockNode), so this workflow is storage-prep only — it no longer
// performs the Helm upgrade. The volumes must exist before the operator renders the
// chart against them (weaver owns PVs via the existingClaim model).
//
// All block node migrations are registered centrally in cmd/cli/commands/root.go RegisterMigrations().
//
// To add a new storage migration:
//  1. Add an OptionalStorage entry to optionalStorages in optional_storage.go
//  2. Add the corresponding config fields to config.BlockNodeStorage
//  3. Add the conditional section to the Go-templated values YAML files
//  4. Register the migration in root.go RegisterMigrations()
//
// See docs/dev/migration-framework.md for the full guide.

package blocknode

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
)

// BuildStorageMigrationWorkflow returns an automa workflow of the storage-prep steps
// (PV/PVC + directory creation) required before a breaking-boundary chart upgrade.
// Returns nil when nothing applies (installed version empty or no applicable
// migration). The caller runs this before patching the HelmCapsule to the target
// version, so the new optional-storage volumes exist when the operator renders the
// chart against them.
func BuildStorageMigrationWorkflow(manager *Manager, profile, valuesFile string) (*automa.WorkflowBuilder, error) {
	installedVersion, err := manager.GetInstalledVersion()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to get installed version")
	}

	if installedVersion == "" {
		return nil, nil
	}

	// Build context
	mctx := &migration.Context{
		Component: migration.ScopeBlockNode,
		Logger:    manager.logger,
		Data:      &automa.SyncStateBag{},
	}
	mctx.Data.Set(migration.CtxKeyInstalledVersion, installedVersion)
	mctx.Data.Set(migration.CtxKeyTargetVersion, manager.blockNodeInputs.ChartVersion)
	// #913: expose the plugins-baked signal to StorageMigration.Applies. It must be
	// set before GetApplicableMigrations (which evaluates Applies); the Manager
	// itself is only added to the context afterwards, for Execute/Rollback.
	mctx.Data.Set(ctxKeyManagePlugins, manager.managesPluginsStorage())

	migrations, err := migration.GetApplicableMigrations(migration.ScopeBlockNode, mctx)
	if err != nil {
		return nil, err
	}

	if len(migrations) == 0 {
		return nil, nil
	}

	// Add context data for migration Execute/Rollback
	mctx.Data.Set(ctxKeyManager, manager)
	mctx.Data.Set(ctxKeyProfile, profile)
	mctx.Data.Set(ctxKeyValuesFile, valuesFile)

	// Storage-prep steps only (PV/PVC creation); the chart move is the operator-driven
	// HelmCapsule patch in steps.UpgradeBlockNode.
	return migration.MigrationsToWorkflow(migrations, mctx), nil
}
