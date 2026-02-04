# Migration Framework

This document describes the migration framework used in Weaver to handle breaking changes during software upgrades.

## Overview

The migration framework provides a simple, extensible way to handle breaking changes:

- **Global Registry**: Migrations are registered at startup in `root.go`
- **Two Migration Types**: Version-based (crosses version boundary) and State-based (checks actual system state)
- **Automa Integration**: Migrations convert to automa workflows for execution with automatic rollback
- **Component-Scoped**: Each component (blocknode, state, etc.) owns its migrations

## Architecture

```
cmd/weaver/commands/
└── root.go                     # Registers all migrations at startup

internal/migration/
├── migration.go                # Interface, Context, Registry, ToWorkflow
└── version_migration.go        # VersionMigration base implementation

internal/blocknode/
├── migrations.go               # RegisterMigrations(), GetMigrationWorkflow()
└── migration_verification_storage.go  # Specific migration

internal/state/
├── migrations.go               # RegisterMigrations(), GetMigrationWorkflow()
└── migration_unified_state.go  # Specific migration (state-based)
```

## Core Components

### Migration Interface

```go
type Migration interface {
    // ID returns a unique identifier for this migration
    ID() string

    // Description returns a human-readable description
    Description() string

    // Applies checks if this migration applies to the given context
    Applies(mctx *Context) (bool, error)

    // Execute performs the migration
    Execute(ctx context.Context, mctx *Context) error

    // Rollback attempts to undo the migration (best-effort)
    Rollback(ctx context.Context, mctx *Context) error
}
```

### Migration Context

```go
type Context struct {
    Component string                 // e.g., "block-node", "state"
    Logger    *zerolog.Logger
    Data      map[string]interface{} // All migration data (versions, managers, etc.)
}

// Well-known context keys
const (
    CtxKeyInstalledVersion = "installedVersion"  // Used by version-based migrations
    CtxKeyTargetVersion    = "targetVersion"     // Used by version-based migrations
)
```

The `Data` map holds all migration-specific data. Version-based migrations use the well-known keys `CtxKeyInstalledVersion` and `CtxKeyTargetVersion`.

### Global Registry

Migrations are registered at startup via package-level functions:

```go
// Register a migration for a component
migration.Register("block-node", myMigration)

// Get applicable migrations (caller prepares context with any needed data)
mctx := &migration.Context{Component: "block-node", Data: make(map[string]interface{})}
mctx.Set(migration.CtxKeyInstalledVersion, installedVersion)  // For version-based migrations
mctx.Set(migration.CtxKeyTargetVersion, targetVersion)
migrations, err := migration.GetApplicableMigrations("block-node", mctx)

// Check if migration is needed
required, summary, err := migration.RequiresMigration("block-node", mctx)

// Convert to automa workflow
workflow := migration.ToWorkflow(migrations, mctx)
```

## Migration Types

### 1. Version-Based Migrations

Use `VersionMigration` when a specific version introduces breaking changes:

```go
type MyMigration struct {
    migration.VersionMigration
}

func NewMyMigration() *MyMigration {
    return &MyMigration{
        VersionMigration: migration.NewVersionMigration(
            "my-feature-v1.0.0",           // ID
            "Description of the migration", // Description  
            "1.0.0",                        // MinVersion - applies when crossing this boundary
        ),
    }
}

// Must implement Execute and Rollback
func (m *MyMigration) Execute(ctx context.Context, mctx *migration.Context) error {
    // Migration logic
    return nil
}

func (m *MyMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
    // Rollback logic (optional)
    return nil
}
```

The `VersionMigration` automatically provides `Applies()` logic:
- Returns `true` if: `installedVersion < minVersion AND targetVersion >= minVersion`
- Returns `false` for fresh installs (empty installedVersion)

### 2. State-Based Migrations

Use custom `Applies()` when migration depends on actual system state:

```go
type UnifiedStateMigration struct {
    id          string
    description string
}

func (m *UnifiedStateMigration) ID() string          { return m.id }
func (m *UnifiedStateMigration) Description() string { return m.description }

// Custom Applies - checks file system state instead of versions
func (m *UnifiedStateMigration) Applies(mctx *migration.Context) (bool, error) {
    // Check for legacy state files
    legacyFiles, err := findLegacyStateFiles(stateDir)
    if err != nil {
        return false, err
    }
    return len(legacyFiles) > 0, nil
}

func (m *UnifiedStateMigration) Execute(ctx context.Context, mctx *migration.Context) error {
    // Migration logic
    return nil
}

func (m *UnifiedStateMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
    // Rollback logic
    return nil
}
```

## Adding New Migrations

### Step 1: Create Migration File

Create `internal/<component>/migration_<feature>.go`:

```go
package mycomponent

import (
    "context"
    "github.com/hashgraph/solo-weaver/internal/migration"
)

type MyFeatureMigration struct {
    migration.VersionMigration
}

func NewMyFeatureMigration() *MyFeatureMigration {
    return &MyFeatureMigration{
        VersionMigration: migration.NewVersionMigration(
            "my-feature-v1.0.0",
            "Add support for my feature",
            "1.0.0",
        ),
    }
}

func (m *MyFeatureMigration) Execute(ctx context.Context, mctx *migration.Context) error {
    // Get component-specific data from context
    manager := mctx.Data["mycomponent.manager"].(*Manager)
    
    // Implement migration logic
    return nil
}

func (m *MyFeatureMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
    // Implement rollback logic
    return nil
}
```

### Step 2: Create/Update migrations.go

Create `internal/<component>/migrations.go`:

```go
package mycomponent

import (
    "github.com/automa-saga/automa"
    "github.com/hashgraph/solo-weaver/internal/migration"
)

const MigrationComponent = "my-component"

// RegisterMigrations registers all migrations for this component.
// Called once at startup from root.go.
func RegisterMigrations() {
    migration.Register(MigrationComponent, NewMyFeatureMigration())
    // Add future migrations here in chronological order
}

// GetMigrationWorkflow returns an automa workflow for applicable migrations.
func GetMigrationWorkflow(manager *Manager, ...) (*automa.WorkflowBuilder, error) {
    installedVersion, err := manager.GetInstalledVersion()
    if err != nil {
        return nil, err
    }

    if installedVersion == "" {
        return nil, nil
    }

    // Build context first
    mctx := &migration.Context{
        Component: MigrationComponent,
        Logger:    manager.logger,
        Data:      make(map[string]interface{}),
    }
    mctx.Set(migration.CtxKeyInstalledVersion, installedVersion)
    mctx.Set(migration.CtxKeyTargetVersion, targetVersion)

    migrations, err := migration.GetApplicableMigrations(MigrationComponent, mctx)
    if err != nil {
        return nil, err
    }

    if len(migrations) == 0 {
        return nil, nil
    }

    // Add component-specific data
    mctx.Set("mycomponent.manager", manager)

    return migration.ToWorkflow(migrations, mctx), nil
}
```

### Step 3: Register in root.go

Add the registration call in `cmd/weaver/commands/root.go`:

```go
func init() {
    // Register all migrations at startup
    blocknode.RegisterMigrations()
    state.RegisterMigrations()
    mycomponent.RegisterMigrations()  // Add new component
    
    // ...
}
```

### Step 4: Integrate with Workflow

In the upgrade workflow step:

```go
func upgradeMyComponent(...) automa.Builder {
    return automa.NewStepBuilder().WithId("upgrade-mycomponent").
        WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
            // Check for migrations
            workflow, err := mycomponent.GetMigrationWorkflow(manager, ...)
            if err != nil {
                return automa.StepFailureReport(stp.Id(), automa.WithError(err))
            }

            if workflow != nil {
                wf, _ := workflow.Build()
                report := wf.Execute(ctx)
                if report.Error != nil {
                    return automa.StepFailureReport(stp.Id(), automa.WithError(report.Error))
                }
                return automa.StepSuccessReport(stp.Id())
            }

            // Normal upgrade path (no migration needed)
            // ...
        })
}
```

## Current Implementations

### Block Node: Verification Storage Migration

**Purpose**: Block Node v0.26.2 added a new PersistentVolume for verification data. Upgrading from < 0.26.2 requires uninstall + reinstall.

**Type**: Version-based (uses `VersionMigration`)

**Files**:
- `internal/blocknode/migrations.go`
- `internal/blocknode/migration_verification_storage.go`

**Steps**:
1. Create verification storage directory
2. Uninstall current release
3. Recreate PVs/PVCs with verification storage
4. Reinstall with new chart

### State: Unified State Migration

**Purpose**: Consolidate individual state files (`*.installed`, `*.configured`) into a single `state.yaml`.

**Type**: State-based (custom `Applies()` checks for legacy files)

**Files**:
- `internal/state/migrations.go`
- `internal/state/migration_unified_state.go`

**Steps**:
1. Find all legacy state files
2. Parse component name and version from each
3. Write unified `state.yaml`
4. Remove old files

## Rollback Behavior

The framework uses automa's `RollbackOnError` execution mode:

1. Migrations execute in registration order
2. If a migration fails, automa automatically rolls back completed migrations in reverse order
3. Each migration's `Rollback()` is called
4. Rollback errors are logged but don't stop other rollbacks

## Best Practices

1. **One concern per migration**: Keep migrations focused on a single change
2. **Implement Rollback**: Always provide rollback logic where possible
3. **Idempotent operations**: Make migration steps idempotent when possible
4. **Test thoroughly**: Test both Execute and Rollback paths
5. **Chronological registration**: Register migrations in the order they should execute
6. **Use context for data**: Pass component-specific data via `Context.Data`
7. **Private context keys**: Use unexported constants for context keys (e.g., `ctxKeyManager`)

## Testing

### Test Applies Logic

```go
func TestMyMigration_Applies(t *testing.T) {
    m := NewMyMigration()
    
    tests := []struct {
        name      string
        installed string
        target    string
        expected  bool
    }{
        {"crosses boundary", "0.9.0", "1.0.0", true},
        {"already past", "1.0.0", "1.1.0", false},
        {"fresh install", "", "1.0.0", false},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mctx := &migration.Context{
                Data: make(map[string]interface{}),
            }
            mctx.Set(migration.CtxKeyInstalledVersion, tt.installed)
            mctx.Set(migration.CtxKeyTargetVersion, tt.target)
            
            applies, err := m.Applies(mctx)
            require.NoError(t, err)
            assert.Equal(t, tt.expected, applies)
        })
    }
}
```

### Test Registration

```go
func TestRegisterMigrations(t *testing.T) {
    migration.ClearRegistry()
    RegisterMigrations()
    defer migration.ClearRegistry()

    mctx := &migration.Context{Data: make(map[string]interface{})}
    mctx.Set(migration.CtxKeyInstalledVersion, "0.9.0")
    mctx.Set(migration.CtxKeyTargetVersion, "1.0.0")

    migrations, err := migration.GetApplicableMigrations(MigrationComponent, mctx)
    require.NoError(t, err)
    assert.Len(t, migrations, 1)
    assert.Equal(t, "my-feature-v1.0.0", migrations[0].ID())
}
```

## File Structure Summary

```
cmd/weaver/commands/
└── root.go                              # Registers all migrations

internal/migration/
├── migration.go                         # Interface, Context, Registry, ToWorkflow
├── version_migration.go                 # VersionMigration base
└── migration_test.go

internal/blocknode/
├── migrations.go                        # RegisterMigrations, GetMigrationWorkflow
├── migration_verification_storage.go    # v0.26.2 migration
└── migration_test.go

internal/state/
├── migrations.go                        # RegisterMigrations, GetMigrationWorkflow
├── migration_unified_state.go           # Unified state migration
└── migration_unified_state_test.go
```

