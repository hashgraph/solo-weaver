// SPDX-License-Identifier: Apache-2.0

// Package migration provides a framework for managing software migrations.
//
// Migrations handle breaking changes between versions that require special
// handling beyond a simple upgrade (e.g., uninstall + reinstall, data migration).
//
// # Usage
//
// 1. Define a migration by implementing the Migration interface:
//
//	type MyMigration struct{}
//
//	func (m *MyMigration) ID() string { return "my-feature-v1.0.0" }
//	func (m *MyMigration) Description() string { return "Add my feature" }
//	func (m *MyMigration) Applies(mctx *Context) (bool, error) { ... }
//	func (m *MyMigration) Execute(ctx context.Context, mctx *Context) error { ... }
//	func (m *MyMigration) Rollback(ctx context.Context, mctx *Context) error { ... }
//
// 2. Register migrations at startup (e.g., in root.go init):
//
//	migration.Register("block-node", &MyMigration{})
//
// 3. Get applicable migrations and execute:
//
//	mctx := &migration.Context{Component: "block-node", Data: make(map[string]interface{})}
//	migrations, _ := migration.GetApplicableMigrations("block-node", mctx)
//	workflow := migration.MigrationsToWorkflow(migrations, mctx)
package migration

import (
	"context"
	"fmt"
	"sync"

	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
)

// Migration represents a single migration that handles a breaking change between versions.
// Implementations should be stateless - all state should be passed via Context.
type Migration interface {
	// ID returns a unique identifier for this migration.
	// Convention: "<feature>-v<version>" (e.g., "verification-storage-v0.26.2")
	ID() string

	// Description returns a human-readable description of what this migration does.
	Description() string

	// Applies checks if this migration applies to the given context.
	Applies(mctx *Context) (bool, error)

	// Execute performs the migration.
	Execute(ctx context.Context, mctx *Context) error

	// Rollback attempts to undo the migration. Best-effort, may not fully restore.
	// Return nil if rollback is not supported or not needed.
	Rollback(ctx context.Context, mctx *Context) error
}

// Context provides context for migration execution.
type Context struct {
	// Component identifies what is being migrated (e.g., "block-node")
	Component string

	// Logger for migration logging
	Logger *zerolog.Logger

	// Data holds migration-specific data (versions, managers, etc.)
	Data map[string]interface{}
}

// Get retrieves a value from the Data map.
func (c *Context) Get(key string) (interface{}, bool) {
	if c.Data == nil {
		return nil, false
	}
	v, ok := c.Data[key]
	return v, ok
}

// GetString retrieves a string value from the Data map.
func (c *Context) GetString(key string) string {
	if v, ok := c.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Set stores a value in the Data map.
func (c *Context) Set(key string, value interface{}) {
	if c.Data == nil {
		c.Data = make(map[string]interface{})
	}
	c.Data[key] = value
}

// Global registry of migrations per component
var (
	registry = make(map[string][]Migration)
	mu       sync.RWMutex
)

// Register adds a migration to the global registry for a component.
// Migrations should be registered in chronological order at startup.
func Register(component string, m Migration) {
	mu.Lock()
	defer mu.Unlock()
	registry[component] = append(registry[component], m)
}

// GetApplicableMigrations returns all migrations that apply for the given context.
func GetApplicableMigrations(component string, mctx *Context) ([]Migration, error) {
	mu.RLock()
	migrations := registry[component]
	mu.RUnlock()

	if mctx == nil {
		mctx = &Context{Component: component}
	}

	var applicable []Migration
	for _, m := range migrations {
		applies, err := m.Applies(mctx)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to check if migration %q applies", m.ID())
		}
		if applies {
			applicable = append(applicable, m)
		}
	}
	return applicable, nil
}

// MigrationsToWorkflow converts a list of migrations into an automa workflow.
// Each migration becomes a step with execute and rollback handlers.
// The workflow executes migrations in order with rollback on failure.
func MigrationsToWorkflow(migrations []Migration, mctx *Context) *automa.WorkflowBuilder {
	if len(migrations) == 0 {
		return automa.NewWorkflowBuilder().WithId("no-migrations")
	}

	var steps []automa.Builder
	for _, m := range migrations {
		migration := m // capture for closure
		step := automa.NewStepBuilder().
			WithId(fmt.Sprintf("migration-%s", migration.ID())).
			WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
				if mctx.Logger != nil {
					mctx.Logger.Info().
						Str("migrationID", migration.ID()).
						Str("description", migration.Description()).
						Msg("Executing migration")
				}

				if err := migration.Execute(ctx, mctx); err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
				return automa.StepSuccessReport(stp.Id())
			}).
			WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
				if mctx.Logger != nil {
					mctx.Logger.Warn().Str("migrationID", migration.ID()).Msg("Rolling back migration")
				}
				if err := migration.Rollback(ctx, mctx); err != nil {
					if mctx.Logger != nil {
						mctx.Logger.Error().Err(err).Str("migrationID", migration.ID()).Msg("Rollback failed")
					}
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
				return automa.StepSuccessReport(stp.Id())
			})
		steps = append(steps, step)
	}

	return automa.NewWorkflowBuilder().
		WithId(fmt.Sprintf("%s-migrations", mctx.Component)).
		WithExecutionMode(automa.RollbackOnError).
		Steps(steps...)
}

// ClearRegistry clears all registered migrations. Useful for testing.
func ClearRegistry() {
	mu.Lock()
	defer mu.Unlock()
	registry = make(map[string][]Migration)
}
