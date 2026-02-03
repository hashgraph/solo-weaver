# Migration Framework

This document describes the migration framework used in Weaver to handle breaking changes during software upgrades. The framework is designed to be generic and reusable across different component types.

## Overview

The migration framework follows patterns from well-known database migration tools like Flyway and Rails ActiveRecord:

- **Versioned**: Each migration has a version boundary that determines when it applies
- **Ordered**: Migrations execute in registration order
- **Rollback Support**: Failed migrations trigger reverse-order rollback (best-effort)
- **Extensible**: Easy to add new migrations for any component type

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    internal/migration                            │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐  │
│  │   Migration     │  │    Manager      │  │    Context      │  │
│  │   (interface)   │  │                 │  │                 │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘  │
│  ┌─────────────────┐                                            │
│  │  BaseMigration  │                                            │
│  │  (default impl) │                                            │
│  └─────────────────┘                                            │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ uses
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                 Component-Specific Migrations                    │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │              internal/blocknode                          │    │
│  │  ┌───────────────────┐  ┌─────────────────────────────┐ │    │
│  │  │    migration.go   │  │ migration_verification_     │ │    │
│  │  │  (helpers)        │  │ storage.go                  │ │    │
│  │  └───────────────────┘  └─────────────────────────────┘ │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │              (future) internal/cilium                    │    │
│  │  ┌───────────────────┐  ┌─────────────────────────────┐ │    │
│  │  │    migration.go   │  │ migration_cni_v2.go         │ │    │
│  │  └───────────────────┘  └─────────────────────────────┘ │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
```

## Core Components

### Migration Interface (`internal/migration/migration.go`)

```go
type Migration interface {
    // ID returns a unique identifier for this migration
    ID() string

    // Description returns a human-readable description
    Description() string

    // MinVersion returns the minimum version that requires this migration
    MinVersion() string

    // Applies checks if this migration applies to the given context
    Applies(ctx *Context) (bool, error)

    // Execute performs the migration
    Execute(ctx context.Context, mctx *Context) error

    // Rollback attempts to undo the migration (best-effort)
    Rollback(ctx context.Context, mctx *Context) error
}
```

### Migration Context

The `Context` struct provides dependencies and data to migrations:

```go
type Context struct {
    Component        string                 // e.g., "block-node", "cilium"
    InstalledVersion string                 // Currently installed version
    TargetVersion    string                 // Version being upgraded to
    Logger           *zerolog.Logger
    Data             map[string]interface{} // Component-specific data
}
```

The `Data` map allows passing arbitrary dependencies without changing the interface. Common keys include:
- `blocknode.manager` - Block Node manager instance
- `blocknode.profile` - Environment profile (local, full)
- `blocknode.valuesFile` - Custom values file path

### Migration Manager

The `Manager` handles migration registration and execution:

```go
manager := migration.NewManager(
    migration.WithLogger(logger),
    migration.WithComponent("block-node"),
)

// Register migrations in chronological order
manager.Register(NewVerificationStorageMigration())

// Check if migrations are needed
required, summary, err := manager.RequiresMigration(ctx)

// Execute all applicable migrations
err := manager.Execute(context.Background(), ctx)
```

### BaseMigration

Provides default version-boundary checking logic:

```go
type BaseMigration struct {
    id          string
    description string
    minVersion  string
}

// Applies returns true if:
// installedVersion < minVersion AND targetVersion >= minVersion
func (b *BaseMigration) Applies(mctx *Context) (bool, error) { ... }
```

## Current Implementation: Block Node

### Verification Storage Migration (v0.26.2)

Block Node v0.26.2 introduced a breaking change: a new PersistentVolume for verification data was added to the StatefulSet. This requires a full reinstall when upgrading from versions < 0.26.2.

**Files:**
- `internal/blocknode/migration.go` - Block Node migration helpers
- `internal/blocknode/migration_verification_storage.go` - The specific migration

**Migration Steps:**
1. Create verification storage directory on host
2. Uninstall current Block Node release
3. Recreate PVs/PVCs including new verification storage
4. Reinstall with new chart version

**Rollback:**
- Attempts to reinstall the previous version if migration fails

### Version-Aware Values Files

The Block Node manager automatically selects appropriate Helm values based on the target version:

| Version | Profile | Values File |
|---------|---------|-------------|
| < 0.26.2 | local | `nano-values.yaml` |
| < 0.26.2 | full | `full-values.yaml` |
| >= 0.26.2 | local | `nano-values-v0.26.2.yaml` |
| >= 0.26.2 | full | `full-values-v0.26.2.yaml` |

### Usage in Workflow

The upgrade workflow checks for and executes migrations automatically:

```go
// In internal/workflows/steps/step_block_node.go
requiresMigration, reason, err := blocknode.RequiresMigration(manager)
if requiresMigration {
    err := blocknode.ExecuteMigration(ctx, manager, profile, valuesFile)
    // ...
}
```

## Adding New Block Node Migrations

### Step 1: Create Migration File

Create a new file `internal/blocknode/migration_<feature>.go`:

```go
package blocknode

import (
    "context"
    "github.com/hashgraph/solo-weaver/internal/migration"
)

const SomeFeatureMinVersion = "0.28.0"

type SomeFeatureMigration struct {
    migration.BaseMigration
}

func NewSomeFeatureMigration() *SomeFeatureMigration {
    return &SomeFeatureMigration{
        BaseMigration: migration.NewBaseMigration(
            "some-feature-v0.28.0",
            "Description of what this migration does",
            SomeFeatureMinVersion,
        ),
    }
}

func (m *SomeFeatureMigration) Execute(ctx context.Context, mctx *migration.Context) error {
    manager, err := GetManager(mctx)
    if err != nil {
        return err
    }
    
    // Implement migration logic here
    return nil
}

func (m *SomeFeatureMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
    // Implement rollback logic (optional, return nil if not supported)
    return nil
}
```

### Step 2: Register Migration

Add the migration to `NewBlockNodeMigrationManager()` in `internal/blocknode/migration.go`:

```go
func NewBlockNodeMigrationManager(logger *zerolog.Logger) *migration.Manager {
    m := migration.NewManager(
        migration.WithLogger(logger),
        migration.WithComponent("block-node"),
    )

    // Register migrations in chronological order
    m.Register(NewVerificationStorageMigration())
    m.Register(NewSomeFeatureMigration())  // Add new migration here

    return m
}
```

### Step 3: Add Tests

Create tests in `internal/blocknode/migration_<feature>_test.go` or add to existing migration tests.

## Adding Migrations for Other Components

### Example: Cilium CNI Migration

```go
// internal/cilium/migration.go
package cilium

import (
    "github.com/hashgraph/solo-weaver/internal/migration"
    "github.com/rs/zerolog"
)

const (
    CtxKeyManager = "cilium.manager"
)

func NewCiliumMigrationManager(logger *zerolog.Logger) *migration.Manager {
    m := migration.NewManager(
        migration.WithLogger(logger),
        migration.WithComponent("cilium"),
    )

    m.Register(NewCNIv2Migration())
    return m
}

func NewCiliumMigrationContext(manager *Manager, installed, target string, logger *zerolog.Logger) *migration.Context {
    ctx := &migration.Context{
        Component:        "cilium",
        InstalledVersion: installed,
        TargetVersion:    target,
        Logger:           logger,
        Data:             make(map[string]interface{}),
    }
    ctx.Set(CtxKeyManager, manager)
    return ctx
}
```

```go
// internal/cilium/migration_cni_v2.go
package cilium

type CNIv2Migration struct {
    migration.BaseMigration
}

func NewCNIv2Migration() *CNIv2Migration {
    return &CNIv2Migration{
        BaseMigration: migration.NewBaseMigration(
            "cni-v2-v1.14.0",
            "Migrate to CNI v2 configuration format",
            "1.14.0",
        ),
    }
}

func (m *CNIv2Migration) Execute(ctx context.Context, mctx *migration.Context) error {
    // Migration logic for CNI v2
    return nil
}
```

### Example: APT Package Migration

For system packages managed via apt, the migration framework can handle configuration file changes or service restarts:

```go
// pkg/software/migration.go
package software

import (
    "github.com/hashgraph/solo-weaver/internal/migration"
)

const (
    CtxKeyPackageManager = "software.packageManager"
    CtxKeyServiceManager = "software.serviceManager"
)

func NewSoftwareMigrationManager(logger *zerolog.Logger) *migration.Manager {
    m := migration.NewManager(
        migration.WithLogger(logger),
        migration.WithComponent("system-packages"),
    )

    m.Register(NewKubeletConfigMigration())
    return m
}
```

```go
// pkg/software/migration_kubelet_config.go
package software

type KubeletConfigMigration struct {
    migration.BaseMigration
}

func NewKubeletConfigMigration() *KubeletConfigMigration {
    return &KubeletConfigMigration{
        BaseMigration: migration.NewBaseMigration(
            "kubelet-config-v1.28.0",
            "Update kubelet configuration for Kubernetes 1.28",
            "1.28.0",
        ),
    }
}

func (m *KubeletConfigMigration) Execute(ctx context.Context, mctx *migration.Context) error {
    // 1. Backup existing config
    // 2. Update configuration file
    // 3. Restart kubelet service
    return nil
}

func (m *KubeletConfigMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
    // 1. Restore config from backup
    // 2. Restart kubelet service
    return nil
}
```

## Custom Applies Logic

For migrations that need more complex logic than version comparison, override the `Applies` method:

```go
func (m *FeatureFlagMigration) Applies(mctx *migration.Context) (bool, error) {
    // Check feature flag in addition to version
    if !isFeatureEnabled("new-storage-backend") {
        return false, nil
    }
    
    // Fall back to version check
    return m.BaseMigration.Applies(mctx)
}
```

## Error Handling and Rollback

### Execution Flow

1. Get applicable migrations for the upgrade path
2. Execute each migration in order
3. Track successfully executed migrations
4. On failure:
   - Attempt rollback of all executed migrations in **reverse order**
   - Report which rollbacks succeeded/failed
   - Return comprehensive error message

### Rollback Behavior

- Rollback is **best-effort** - individual failures don't stop other rollbacks
- All rollback errors are collected and reported
- If rollback fails, manual intervention guidance is provided

### Error Messages

The framework provides detailed error messages:

```
Migration "verification-storage-v0.26.2" failed and rollback also failed (migration-1: error).
Manual intervention may be required.
```

```
Migration "some-feature-v0.28.0" failed but rollback succeeded.
The system has been restored. Please investigate before retrying.
```

## Best Practices

1. **One concern per migration**: Keep migrations focused on a single change
2. **Idempotent operations**: Where possible, make migration steps idempotent
3. **Test rollback**: Always test that rollback works correctly
4. **Document breaking changes**: Add clear descriptions of what changed and why
5. **Version constants**: Define version constants in the migration file
6. **Chronological registration**: Register migrations in the order they should execute
7. **Fail-closed for versions**: Return errors for unparseable versions rather than assuming no migration needed

## Testing

### Unit Tests

Test the `Applies` logic with various version combinations:

```go
func TestMyMigration_Applies(t *testing.T) {
    m := NewMyMigration()
    
    tests := []struct {
        installed string
        target    string
        expected  bool
    }{
        {"0.26.0", "0.28.0", true},   // Crosses boundary
        {"0.28.0", "0.29.0", false},  // Already past
        {"", "0.28.0", false},        // Fresh install
    }
    
    for _, tt := range tests {
        ctx := &migration.Context{
            InstalledVersion: tt.installed,
            TargetVersion:    tt.target,
        }
        applies, err := m.Applies(ctx)
        require.NoError(t, err)
        assert.Equal(t, tt.expected, applies)
    }
}
```

### Integration Tests

Test the full migration flow with mocked dependencies:

```go
func TestMigrationManager_Execute_WithRollback(t *testing.T) {
    // Create manager with mock migrations
    // Trigger failure in second migration
    // Verify first migration was rolled back
}
```

## File Structure

```
internal/
├── migration/
│   ├── migration.go       # Core framework
│   └── migration_test.go  # Framework tests
│
├── blocknode/
│   ├── migration.go                        # Block Node helpers
│   ├── migration_test.go                   # Migration tests  
│   └── migration_verification_storage.go   # v0.26.2 migration
│
├── cilium/                 # (future)
│   ├── migration.go
│   └── migration_cni_v2.go
│
pkg/
└── software/               # (future)
    ├── migration.go
    └── migration_kubelet_config.go
```

