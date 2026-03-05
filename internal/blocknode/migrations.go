// SPDX-License-Identifier: Apache-2.0

// migrations.go provides the component-level orchestration for block node migrations.
//
// This file contains:
//   - InitMigrations(): Registers all block node migrations at startup (called from root.go)
//   - BuildMigrationWorkflow(): Builds an automa workflow for applicable migrations during upgrades
//
// The workflow uses a two-phase approach:
//   - Phase 1: Each applicable StorageMigration creates its storage dir + PV/PVC.
//   - Phase 2: A single "upgrade" step deletes the StatefulSet (orphan cascade)
//     and performs one Helm upgrade to the final target version.
//
// This avoids intermediate chart upgrades, version-swapping, and repeated
// StatefulSet deletions when multiple storage migrations apply at once.
//
// To add a new storage migration:
//  1. Add an OptionalStorage entry to optionalStorages in optional_storage.go
//  2. Add the corresponding config fields to config.BlockNodeStorage
//  3. Add the conditional section to the Go-templated values YAML files
//  4. Register the migration in InitMigrations() below
//
// See docs/dev/migration-framework.md for the full guide.

package blocknode

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
)

// ComponentBlockNode is the component name for block node migrations.
const ComponentBlockNode = "block-node"

// InitMigrations registers all block node migrations.
// Called once at startup from root.go.
func InitMigrations() {
	migration.Register(ComponentBlockNode, NewVerificationStorageMigration())
	migration.Register(ComponentBlockNode, NewPluginsStorageMigration())
}

// BuildMigrationWorkflow returns an automa workflow for executing applicable migrations.
// Returns nil if no migrations are needed (installed version is empty or no applicable migrations).
//
// The workflow structure is:
//
//	[migration-start] → [migration-<id>] → ... → [migration-upgrade-chart]
//
// Each migration step creates its PV/PVC. The final upgrade step performs a single
// StatefulSet delete + Helm upgrade to the target version with all new storages included.
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

	// Add context data for migration Execute/Rollback
	mctx.Data.Set(ctxKeyManager, manager)
	mctx.Data.Set(ctxKeyProfile, profile)
	mctx.Data.Set(ctxKeyValuesFile, valuesFile)

	// Build the workflow: migration steps (PV/PVC creation) + final upgrade step
	wf := migration.MigrationsToWorkflow(migrations, mctx)

	// Append the single upgrade step that brings the chart to the target version
	// after all PV/PVCs have been created.
	targetVersion := manager.blockConfig.Version
	upgradeStep := buildMigrationUpgradeStep(manager, mctx, installedVersion, targetVersion, profile, valuesFile, migrations)
	wf.Steps(upgradeStep)

	return wf, nil
}

// buildMigrationUpgradeStep creates the final workflow step that performs a single
// StatefulSet delete + Helm upgrade after all storage migrations have created their PV/PVCs.
//
// On rollback, it downgrades to the installed version and cleans up all PV/PVCs
// created by the migration steps.
func buildMigrationUpgradeStep(
	manager *Manager,
	mctx *migration.Context,
	installedVersion, targetVersion, profile, valuesFile string,
	migrations []migration.Migration,
) automa.Builder {
	return automa.NewStepBuilder().
		WithId("migration-upgrade-chart").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			l.Info().
				Str("target", targetVersion).
				Int("migrationsApplied", len(migrations)).
				Msg("All storage PV/PVCs created, upgrading chart to target version")

			// Compute values file for the final target version (all storages now exist)
			valuesFilePath, err := manager.ComputeValuesFile(profile, valuesFile)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.IllegalState.Wrap(err, "failed to compute values file for target version")))
			}

			// Delete StatefulSet with orphan cascade — Kubernetes forbids
			// in-place updates to volumeClaimTemplates.
			if err := manager.DeleteStatefulSetForUpgrade(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Single Helm upgrade to the final target version
			if err := manager.UpgradeChart(ctx, valuesFilePath, false); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.IllegalState.Wrap(err, "failed to upgrade chart to target version")))
			}

			l.Info().Str("version", targetVersion).Msg("Chart upgraded successfully after migrations")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			l.Warn().
				Str("target", installedVersion).
				Msg("Rolling back migration upgrade, downgrading chart")

			// Temporarily set version to installed version for ComputeValuesFile + UpgradeChart
			originalVersion := manager.blockConfig.Version
			manager.blockConfig.Version = installedVersion
			defer func() { manager.blockConfig.Version = originalVersion }()

			valuesFilePath, err := manager.ComputeValuesFile(profile, valuesFile)
			if err != nil {
				l.Error().Err(err).Msg("Rollback failed: could not compute values file")
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Delete StatefulSet before downgrade (volumeClaimTemplates may differ)
			if err := manager.DeleteStatefulSetForUpgrade(ctx); err != nil {
				l.Warn().Err(err).Msg("Failed to delete StatefulSet during rollback, continuing")
			}

			if err := manager.UpgradeChart(ctx, valuesFilePath, false); err != nil {
				l.Error().Err(err).Msg("Rollback failed: could not downgrade chart")
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Clean up all PV/PVCs created by migrations (best-effort)
			for _, m := range migrations {
				sm, ok := m.(*StorageMigration)
				if !ok {
					continue
				}
				l.Info().Str("storage", sm.storage.Name).Msg("Cleaning up PV/PVC after rollback")
				if delErr := manager.kubeClient.DeletePVC(ctx, manager.blockConfig.Namespace, sm.storage.PVCName); delErr != nil {
					l.Warn().Err(delErr).Str("pvc", sm.storage.PVCName).Msg("Could not delete PVC")
				}
				if delErr := manager.kubeClient.DeletePV(ctx, sm.storage.PVName); delErr != nil {
					l.Warn().Err(delErr).Str("pv", sm.storage.PVName).Msg("Could not delete PV")
				}
			}

			l.Info().Str("version", installedVersion).Msg("Migration rollback completed")
			return automa.StepSuccessReport(stp.Id())
		})
}
