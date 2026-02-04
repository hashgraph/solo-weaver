// SPDX-License-Identifier: Apache-2.0

package state

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/joomcode/errorx"
)

// MigrationComponent is the component name for state migrations.
const MigrationComponent = "state"

// RegisterMigrations registers all state migrations.
// Called once at startup from root.go.
func RegisterMigrations() {
	migration.Register(MigrationComponent, NewUnifiedStateMigration())
}

// GetMigrationWorkflow returns an automa workflow for executing applicable state migrations.
// Returns nil if no migrations are needed.
func GetMigrationWorkflow() (*automa.WorkflowBuilder, error) {
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

	return migration.ToWorkflow(migrations, mctx), nil
}
