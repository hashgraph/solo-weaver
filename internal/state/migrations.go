// SPDX-License-Identifier: Apache-2.0

// migrations.go provides the component-level orchestration for state migrations.
//
// This file contains:
//   - InitMigrations(): Registers all state migrations at startup (called from root.go)
//   - BuildMigrationWorkflow(): Builds an automa workflow for applicable migrations
//
// Individual migration implementations are in separate migration_*.go files:
//   - migration_unified_state.go: Handles unified state file migration
//
// To add a new migration:
//  1. Create a new migration_<name>.go file implementing the migration.Migration interface
//  2. Register it in InitMigrations() below
//
// See docs/dev/migration-framework.md for the full guide.

package state

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
)

// MigrationComponent is the component name for state migrations.
const MigrationComponent = "state"

// InitMigrations registers all state migrations.
// Called once at startup from root.go.
func InitMigrations() {
	migration.Register(MigrationComponent, NewUnifiedStateMigration())
}

// BuildMigrationWorkflow returns an automa workflow for executing applicable state migrations.
// Returns nil if no migrations are needed.
func BuildMigrationWorkflow() (*automa.WorkflowBuilder, error) {
	mctx := &migration.Context{
		Component: MigrationComponent,
		Data:      make(map[string]interface{}),
	}

	migrations, err := migration.GetApplicableMigrations(MigrationComponent, mctx)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to get applicable state migrations")
	}

	if len(migrations) == 0 {
		return nil, nil
	}

	return migration.MigrationsToWorkflow(migrations, mctx), nil
}
