// SPDX-License-Identifier: Apache-2.0

// Package migration provides a generic framework for managing software migrations.
//
// This package implements a migration pattern similar to database migration tools like
// Flyway and Rails ActiveRecord migrations. It allows defining versioned migrations
// that can be executed in sequence with rollback support.
//
// # Usage
//
// 1. Define a migration by implementing the Migration interface:
//
//	type MyMigration struct{}
//
//	func (m *MyMigration) ID() string { return "my-feature-v1.0.0" }
//	func (m *MyMigration) Description() string { return "Add my feature" }
//	func (m *MyMigration) MinVersion() string { return "1.0.0" }
//	func (m *MyMigration) Applies(mctx *Context) (bool, error) { ... }
//	func (m *MyMigration) Execute(ctx context.Context, mctx *Context) error { ... }
//	func (m *MyMigration) Rollback(ctx context.Context, mctx *Context) error { ... }
//
// 2. Create a Manager and register migrations:
//
//	manager := migration.NewManager(
//	    migration.WithLogger(logger),
//	    migration.WithComponent("block-node"),
//	)
//	manager.Register(&MyMigration{})
//
// 3. Execute migrations:
//
//	ctx := &migration.Context{
//	    InstalledVersion: "0.9.0",
//	    TargetVersion:    "1.0.0",
//	}
//	err := manager.Execute(context.Background(), ctx)
//
// # Design Principles
//
// - Generic: Works with any component type (helm charts, apt packages, binaries, configs)
// - Versioned: Each migration has a clear version boundary
// - Ordered: Migrations execute in registration order
// - Rollback: Failed migrations trigger reverse-order rollback
// - Extensible: Easy to add new migration types
package migration

import (
	"context"
	"fmt"

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

	// MinVersion returns the minimum version that requires this migration.
	// Upgrades from versions < MinVersion to versions >= MinVersion will trigger this migration.
	MinVersion() string

	// Applies checks if this migration applies to the given context.
	// The default implementation checks version boundaries, but implementations
	// can override for custom logic (e.g., feature flags, environment checks).
	Applies(ctx *Context) (bool, error)

	// Execute performs the migration.
	Execute(ctx context.Context, mctx *Context) error

	// Rollback attempts to undo the migration. This is best-effort and may not fully
	// restore the previous state. Implementations should document what can and cannot
	// be rolled back. Return nil if rollback is not supported or not needed.
	Rollback(ctx context.Context, mctx *Context) error
}

// Context provides context and dependencies for migration execution.
// It is designed to be generic and extensible via the Data map.
type Context struct {
	// Component identifies what is being migrated (e.g., "block-node", "cilium", "crio")
	Component string

	// InstalledVersion is the currently installed version (empty if not installed)
	InstalledVersion string

	// TargetVersion is the version being upgraded to
	TargetVersion string

	// Logger for migration logging
	Logger *zerolog.Logger

	// Data holds component-specific data needed by migrations.
	// This allows passing arbitrary dependencies without changing the interface.
	// Examples:
	//   - "helm.manager": helm.Manager for helm-based migrations
	//   - "kube.client": *kube.Client for kubernetes operations
	//   - "fs.manager": fsx.Manager for filesystem operations
	//   - "profile": string for environment profile
	//   - "values.file": string path to values file
	Data map[string]interface{}
}

// Get retrieves a value from the Data map with type assertion.
// Returns the zero value and false if the key doesn't exist or type doesn't match.
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

// Manager manages and executes migrations for a specific component.
type Manager struct {
	component  string
	migrations []Migration
	logger     *zerolog.Logger
}

// Option configures a Manager.
type Option func(*Manager)

// WithLogger sets the logger for the migration manager.
func WithLogger(logger *zerolog.Logger) Option {
	return func(m *Manager) {
		m.logger = logger
	}
}

// WithComponent sets the component name for the migration manager.
func WithComponent(component string) Option {
	return func(m *Manager) {
		m.component = component
	}
}

// NewManager creates a new migration manager with the given options.
func NewManager(opts ...Option) *Manager {
	nop := zerolog.Nop()
	m := &Manager{
		component:  "unknown",
		migrations: make([]Migration, 0),
		logger:     &nop,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// Register adds a migration to the manager.
// Migrations should be registered in chronological order.
func (m *Manager) Register(migration Migration) {
	m.migrations = append(m.migrations, migration)
	m.logger.Debug().
		Str("component", m.component).
		Str("migrationID", migration.ID()).
		Str("minVersion", migration.MinVersion()).
		Msg("Registered migration")
}

// GetApplicable returns all migrations that apply for the given context,
// in the order they should be executed.
func (m *Manager) GetApplicable(mctx *Context) ([]Migration, error) {
	var applicable []Migration

	for _, migration := range m.migrations {
		applies, err := migration.Applies(mctx)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err,
				"failed to check if migration %q applies", migration.ID())
		}
		if applies {
			applicable = append(applicable, migration)
		}
	}

	return applicable, nil
}

// RequiresMigration checks if any migrations are needed for the given context.
// Returns true if at least one migration applies, along with a summary.
func (m *Manager) RequiresMigration(mctx *Context) (bool, string, error) {
	applicable, err := m.GetApplicable(mctx)
	if err != nil {
		return false, "", err
	}

	if len(applicable) == 0 {
		return false, "", nil
	}

	// Build summary
	var summary string
	if len(applicable) == 1 {
		summary = fmt.Sprintf("1 migration required for %s: %s - %s",
			m.component, applicable[0].ID(), applicable[0].Description())
	} else {
		summary = fmt.Sprintf("%d migrations required for %s:\n", len(applicable), m.component)
		for i, migration := range applicable {
			summary += fmt.Sprintf("  %d. %s - %s\n", i+1, migration.ID(), migration.Description())
		}
	}

	return true, summary, nil
}

// Execute runs all applicable migrations in order.
// If any migration fails, it attempts to rollback previously executed migrations
// in reverse order (best-effort).
func (m *Manager) Execute(ctx context.Context, mctx *Context) error {
	applicable, err := m.GetApplicable(mctx)
	if err != nil {
		return err
	}

	if len(applicable) == 0 {
		m.logger.Info().
			Str("component", m.component).
			Msg("No migrations required")
		return nil
	}

	m.logger.Info().
		Str("component", m.component).
		Int("count", len(applicable)).
		Str("installedVersion", mctx.InstalledVersion).
		Str("targetVersion", mctx.TargetVersion).
		Msg("Executing migrations")

	// Track executed migrations for potential rollback
	var executed []Migration

	for _, migration := range applicable {
		m.logger.Info().
			Str("component", m.component).
			Str("migrationID", migration.ID()).
			Str("description", migration.Description()).
			Msg("Executing migration")

		if err := migration.Execute(ctx, mctx); err != nil {
			m.logger.Error().
				Err(err).
				Str("component", m.component).
				Str("migrationID", migration.ID()).
				Msg("Migration failed, attempting rollback")

			// Attempt rollback of executed migrations
			rollbackErr := m.rollback(ctx, mctx, executed)

			if rollbackErr != nil {
				return errorx.IllegalState.Wrap(err,
					"migration %q failed and rollback also failed (%v). "+
						"Manual intervention may be required.",
					migration.ID(), rollbackErr)
			}

			return errorx.IllegalState.Wrap(err,
				"migration %q failed but rollback succeeded. "+
					"The system has been restored. Please investigate before retrying.",
				migration.ID())
		}

		executed = append(executed, migration)
		m.logger.Info().
			Str("component", m.component).
			Str("migrationID", migration.ID()).
			Msg("Migration completed successfully")
	}

	m.logger.Info().
		Str("component", m.component).
		Int("count", len(executed)).
		Msg("All migrations completed successfully")

	return nil
}

// rollback attempts to rollback migrations in reverse order.
func (m *Manager) rollback(ctx context.Context, mctx *Context, migrations []Migration) error {
	if len(migrations) == 0 {
		return nil
	}

	var rollbackErrors []error

	// Rollback in reverse order
	for i := len(migrations) - 1; i >= 0; i-- {
		migration := migrations[i]
		m.logger.Warn().
			Str("component", m.component).
			Str("migrationID", migration.ID()).
			Msg("Attempting rollback")

		if err := migration.Rollback(ctx, mctx); err != nil {
			m.logger.Error().
				Err(err).
				Str("component", m.component).
				Str("migrationID", migration.ID()).
				Msg("Rollback failed")
			rollbackErrors = append(rollbackErrors, fmt.Errorf("%s: %w", migration.ID(), err))
		} else {
			m.logger.Info().
				Str("component", m.component).
				Str("migrationID", migration.ID()).
				Msg("Rollback succeeded")
		}
	}

	if len(rollbackErrors) > 0 {
		return fmt.Errorf("rollback failed for %d migration(s): %v", len(rollbackErrors), rollbackErrors)
	}

	return nil
}
