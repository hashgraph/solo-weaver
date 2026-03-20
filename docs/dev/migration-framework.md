# Migration Framework

This document describes the migration framework used in Solo Provisioner to handle breaking changes during software upgrades.

## Overview

The migration framework provides a simple, extensible way to handle breaking changes:

- **Centralised registration**: all migrations are registered in a single place — `root.go RegisterMigrations()`. No `InitMigrations()` function exists anywhere else.
- **Two execution scopes**: `startup` (every CLI invocation) and `block-node` (during block node upgrades only).
- **Two migration base types**: `VersionMigration` (chart version boundary) and `CLIVersionMigration` (CLI binary version boundary). State-based migrations implement `Applies()` directly.
- **Automa integration**: migrations convert to automa workflows with automatic rollback on failure.
- **Idempotent `Applies()`**: ground truth (on-disk state or version comparison) is used instead of a persistent ledger.

## Architecture

```
cmd/weaver/commands/
└── root.go                         # Single authoritative RegisterMigrations()

cmd/weaver/commands/common/
└── run.go                          # RunStartupMigrations() — drives startup scope

internal/migration/
├── migration.go                    # Interface, Context, Registry, MigrationsToWorkflow, scope constants
├── version_migration.go            # VersionMigration base (chart version boundary)
├── cli_version_migration.go        # CLIVersionMigration base (CLI binary version boundary)
└── migration_test.go

internal/state/
├── migrations.go                   # ReadProvisionerVersionFromDisk()
├── migration_unified_state.go      # State-based: legacy *.installed/*.configured → state.yaml
└── migration_helm_release_schema_v2.go

internal/blocknode/
├── migrations.go                   # BuildMigrationWorkflow() — block-node scope only
├── migration_storage.go            # StorageMigration base
├── migration_verification_storage.go
└── migration_plugins_storage.go

internal/workflows/
└── migration_legacy_binary.go      # Startup: remove legacy "weaver" binary
```

## Scopes

| Scope | Constant | When it runs |
|---|---|---|
| `"startup"` | `migration.ScopeStartup` | Before every CLI command, in `RunGlobalChecks` |
| `"block-node"` | `blocknode.ComponentBlockNode` | Explicitly during block node upgrade workflow |

## Core Components

### Migration Interface

```go
type Migration interface {
    ID() string
    Description() string
    Applies(mctx *Context) (bool, error)
    Execute(ctx context.Context, mctx *Context) error
    Rollback(ctx context.Context, mctx *Context) error
}
```

### Migration Context

```go
type Context struct {
    Component string          // scope — e.g. migration.ScopeStartup
    Logger    *zerolog.Logger
    Data      automa.StateBag // key/value bag for migration data
}

// Chart version keys (block-node scope)
const (
    CtxKeyInstalledVersion = "installedVersion"
    CtxKeyTargetVersion    = "targetVersion"
)

// CLI version keys (startup scope)
const (
    CtxKeyInstalledCLIVersion = "installedCLIVersion"
    CtxKeyCurrentCLIVersion   = "currentCLIVersion"
)
```

### Global Registry

```go
// Register a migration under a scope
migration.Register(migration.ScopeStartup, myMigration)

// Retrieve applicable migrations
mctx := &migration.Context{
    Component: migration.ScopeStartup,
    Data:      &automa.SyncStateBag{},
}
mctx.Data.Set(migration.CtxKeyInstalledCLIVersion, installedVersion)
mctx.Data.Set(migration.CtxKeyCurrentCLIVersion, version.Number())

migrations, err := migration.GetApplicableMigrations(migration.ScopeStartup, mctx)
```

## Migration Types

### 1. CLI-Version-Based (startup scope)

Use `CLIVersionMigration` when a CLI binary version introduces a breaking change that must be fixed before any command runs:

```go
type MyStartupMigration struct {
    migration.CLIVersionMigration
}

func NewMyStartupMigration() *MyStartupMigration {
    return &MyStartupMigration{
        CLIVersionMigration: migration.NewCLIVersionMigration(
            "my-change-v1.2.0",              // ID
            "Description of the migration",  // Description
            "1.2.0",                         // minVersion boundary
        ),
    }
}

func (m *MyStartupMigration) Execute(ctx context.Context, mctx *migration.Context) error { ... }
func (m *MyStartupMigration) Rollback(ctx context.Context, mctx *migration.Context) error { ... }
```

`CLIVersionMigration.Applies()` returns `true` when:
`installedCLIVersion < minVersion AND currentCLIVersion >= minVersion`

### 2. Chart-Version-Based (block-node scope)

Use `VersionMigration` when a Helm chart version introduces breaking storage changes:

```go
type MyStorageMigration struct {
    migration.VersionMigration
}

func NewMyStorageMigration() *MyStorageMigration {
    return &MyStorageMigration{
        VersionMigration: migration.NewVersionMigration(
            "my-storage-v1.2.0",
            "Add new PV/PVC for v1.2.0",
            "1.2.0",
        ),
    }
}

func (m *MyStorageMigration) Execute(ctx context.Context, mctx *migration.Context) error { ... }
func (m *MyStorageMigration) Rollback(ctx context.Context, mctx *migration.Context) error { ... }
```

`VersionMigration.Applies()` returns `true` when:
`installedChartVersion < minVersion AND targetChartVersion >= minVersion`

### 3. State-Based (startup scope)

Use a custom `Applies()` when the migration should run based on actual on-disk state rather than version numbers:

```go
type MyStateMigration struct{ id, description string }

func (m *MyStateMigration) ID() string          { return m.id }
func (m *MyStateMigration) Description() string { return m.description }

func (m *MyStateMigration) Applies(mctx *migration.Context) (bool, error) {
    // Check actual on-disk state
    legacyFiles, err := findLegacyFiles(stateDir)
    return len(legacyFiles) > 0, err
}

func (m *MyStateMigration) Execute(ctx context.Context, mctx *migration.Context) error { ... }
func (m *MyStateMigration) Rollback(ctx context.Context, mctx *migration.Context) error { ... }
```

**Idempotency contract**: `Applies()` must return `false` after a successful `Execute()`. Ground truth (actual on-disk state) is more reliable than a separate ledger.

## Adding a New Migration

### Step 1: Create the migration file

Create `internal/<package>/migration_<name>.go` implementing `migration.Migration`. Choose the appropriate base type.

### Step 2: Register in `root.go`

Append to `RegisterMigrations()` at the chronologically correct position:

```go
func RegisterMigrations() {
    // Startup migrations
    migration.Register(migration.ScopeStartup, state.NewUnifiedStateMigration())
    migration.Register(migration.ScopeStartup, state.NewHelmReleaseSchemaV2Migration())
    migration.Register(migration.ScopeStartup, workflows.NewLegacyBinaryMigration())
    migration.Register(migration.ScopeStartup, mypkg.NewMyStartupMigration()) // ← add here

    // Block-node upgrade migrations
    migration.Register(blocknode.ComponentBlockNode, blocknode.NewVerificationStorageMigration())
    migration.Register(blocknode.ComponentBlockNode, blocknode.NewPluginsStorageMigration())
    // migration.Register(blocknode.ComponentBlockNode, blocknode.NewMyStorageMigration()) // ← or here
}
```

### Step 3: Done

No `InitMigrations()` to update. The migration runs automatically on the next CLI invocation.

## Startup Migration Runner

`RunStartupMigrations()` (called from `RunGlobalChecks` before every command) drives a single ordered pass over all `"startup"`-scoped migrations:

1. Reads the provisioner version last written to disk via `state.ReadProvisionerVersionFromDisk()` — this is the *installed* CLI version.
2. Gets the *current* CLI version from `version.Number()`.
3. Builds a `migration.Context` with both version keys populated.
4. Calls `migration.GetApplicableMigrations(migration.ScopeStartup, mctx)`.
5. Executes the workflow with rollback on error.

After the first command runs, `stateManager.Refresh()` stamps `ProvisionerState.Version = version.Number()` to disk, so subsequent invocations of the same binary version see no applicable CLI-version-gated migrations.

## Block-Node Upgrade Workflow

`blocknode.BuildMigrationWorkflow()` uses a special two-phase structure because Kubernetes forbids in-place `volumeClaimTemplates` updates:

1. **Phase 1** — each `StorageMigration.Execute()` creates its storage directory + PV/PVC.
2. **Phase 2** — a single final step deletes the StatefulSet (orphan cascade) and performs one Helm upgrade to the target version.

This avoids multiple StatefulSet deletions and intermediate chart upgrades when several storage migrations apply at once.

## Rollback Behaviour

The framework uses automa's `RollbackOnError` execution mode:

1. Migrations execute in registration order.
2. If a migration fails, automa rolls back completed migrations in reverse order.
3. Each migration's `Rollback()` is called — errors are logged but do not abort other rollbacks.
4. Because `Applies()` is ground-truth-based, a successful rollback automatically re-enables the migration on the next invocation.

## Testing

```go
// Test Applies() boundary logic
func TestMyMigration_Applies(t *testing.T) {
    m := NewMyMigration()
    mctx := &migration.Context{Data: &automa.SyncStateBag{}}
    mctx.Data.Set(migration.CtxKeyInstalledCLIVersion, "1.1.0")
    mctx.Data.Set(migration.CtxKeyCurrentCLIVersion, "1.2.0")
    applies, err := m.Applies(mctx)
    require.NoError(t, err)
    assert.True(t, applies)
}

// Test registration (register directly — no InitMigrations())
func TestMyMigration_Registration(t *testing.T) {
    migration.ClearRegistry()
    defer migration.ClearRegistry()
    migration.Register(migration.ScopeStartup, NewMyMigration())

    mctx := &migration.Context{
        Component: migration.ScopeStartup,
        Data:      &automa.SyncStateBag{},
    }
    migrations, err := migration.GetApplicableMigrations(migration.ScopeStartup, mctx)
    require.NoError(t, err)
    // assert expected count / IDs
}

// Idempotency contract
func TestMyMigration_Idempotency(t *testing.T) {
    m := NewMyMigration()
    // ... set up state so Applies() returns true
    // execute
    err := m.Execute(context.Background(), mctx)
    require.NoError(t, err)
    // Applies() must now return false
    applies, err := m.Applies(mctx)
    require.NoError(t, err)
    assert.False(t, applies, "Applies() must return false after Execute()")
}
```
