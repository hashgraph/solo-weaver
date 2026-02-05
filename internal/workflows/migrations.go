// SPDX-License-Identifier: Apache-2.0

// migrations.go provides the component-level orchestration for weaver/solo-provisioner migrations.
//
// This file contains:
//   - InitMigrations(): Registers all weaver migrations at startup (called from root.go)
//   - BuildMigrationWorkflow(): Builds an automa workflow for applicable migrations
//
// Individual migration implementations are in separate migration_*.go files:
//   - migration_legacy_binary.go: Handles removal of legacy "weaver" binary
//
// See docs/dev/migration-framework.md for the full guide.

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
)

// MigrationComponent is the component name for weaver/solo-provisioner migrations.
const MigrationComponent = "solo-provisioner"

// InitMigrations registers all weaver/solo-provisioner migrations.
// Called once at startup from root.go.
func InitMigrations() {
	migration.Register(MigrationComponent, NewLegacyBinaryMigration())
}

// BuildMigrationWorkflow returns an automa workflow for executing applicable migrations.
// Returns nil if no migrations are needed.
func BuildMigrationWorkflow() (*automa.WorkflowBuilder, error) {
	mctx := &migration.Context{
		Component: MigrationComponent,
		Data:      &automa.SyncStateBag{},
	}

	migrations, err := migration.GetApplicableMigrations(MigrationComponent, mctx)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to get applicable solo-provisioner migrations")
	}

	if len(migrations) == 0 {
		return nil, nil
	}

	return migration.MigrationsToWorkflow(migrations, mctx), nil
}
