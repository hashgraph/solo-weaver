# Migration Framework Redesign

**Centralized Registration with CLI-Version-Ordered Execution**

> **Status:** Architecture proposal — supersedes `migration-framework.md` once accepted.  
> **Date:** 2026-03-19

---

## Table of Contents

1. [Current State: What Is Broken and Why](#current-state-what-is-broken-and-why)
2. [Proposed Architecture Goals](#proposed-architecture-goals)
3. [Concept 1 — Two Execution Scopes](#concept-1--two-execution-scopes)
4. [Concept 2 — Single `RegisterMigrations()`, Scope as the Tag](#concept-2--single-registermigrations-scope-as-the-tag)
5. [Concept 3 — CLI Version as the Migration Gate](#concept-3--cli-version-as-the-migration-gate)
6. [Concept 4 — Enriched `migration.Context` with CLI Versions](#concept-4--enriched-migrationcontext-with-cli-versions)
7. [Concept 5 — Unified Startup Migration Runner](#concept-5--unified-startup-migration-runner)
8. [Concept 6 — `blocknode.BuildMigrationWorkflow()` Keeps Its Special Logic](#concept-6--blocknodebuildmigrationworkflow-keeps-its-special-logic)
9. [Idempotency vs Migration History Tracking](#idempotency-vs-migration-history-tracking)
10. [What Changes — Summary](#what-changes--summary)
11. [New File Structure](#new-file-structure)
12. [Adding a New Migration — Developer Guide](#adding-a-new-migration--developer-guide)
13. [Further Considerations](#further-considerations)

---

## Current State: What Is Broken and Why

### Problem 1 — Split-brain registration

There are two parallel registration paths that are never connected:

| Path | Where | What it registers today                              |
|---|---|------------------------------------------------------|
| `root.go RegisterMigrations()` | cmd layer | `UnifiedState`, 2× blocknode storage, `LegacyBinary` |
| `state.InitMigrations()` | state package | `UnifiedState` + **`HelmReleaseSchemaV2`** (duplicate of root.go)          |
| `blocknode.InitMigrations()` | blocknode package | same 2× storage migrations (duplicate of root.go)    |
| `workflows.InitMigrations()` | workflows package | `LegacyBinary` (duplicate of root.go)                |

**Result:** Any migration added to a component's
`InitMigrations()` but forgotten in `root.go` suffers the same fate of missed execution.

### Problem 2 — `workflows` migrations have no execution driver

`RunGlobalChecks` calls `RunStateMigrations()` → `state.BuildMigrationWorkflow()`.
It never calls `workflows.BuildMigrationWorkflow()`. `LegacyBinaryMigration` is registered but
never executed at startup.

### Problem 3 — No version-ordering guarantee across components

Migrations from the `"state"` and `"solo-provisioner"` components are driven in separate passes.
There is no mechanism to enforce that state migration A runs before workflows migration B across
component boundaries. Registration order only applies within a single
`GetApplicableMigrations(component, …)` call.

### Problem 4 — `ProvisionerState.Version` is not used as a migration gate

`state.ProvisionerState.Version` records the CLI version that last wrote the state file. This is
the ideal *"installed CLI version"* for version-gated startup migrations. However, `Refresh()`
never stamps the current CLI version back in, so the field drifts and cannot be trusted for
version comparisons without an explicit fix.

---

## Proposed Architecture Goals

> A single `RegisterMigrations()` in `root.go` is the **only** place any migration is ever
> registered. The CLI version drives which migrations run and in what order.
> No `InitMigrations()` exists anywhere.

---

## Concept 1 — Two Execution Scopes

All migrations fall into exactly one of two execution scopes:

| Scope | Tag | Runs when | Context versioning |
|---|---|---|---|
| **Startup** | `"startup"` | Every CLI invocation, before any command, in `RunGlobalChecks` | CLI version: `version.Number()` vs last stored `ProvisionerState.Version` |
| **Block-node upgrade** | `"block-node"` | Explicitly during the block node upgrade workflow | Chart version: installed chart version vs target chart version |

**Startup scope** covers: `UnifiedStateMigration`, `HelmReleaseSchemaV2Migration`,
`LegacyBinaryMigration` — all migrations that fix on-disk or on-machine state regardless of
which component originally created it.

**Block-node scope** covers: `VerificationStorageMigration`, `PluginsStorageMigration`, and any
future Kubernetes StatefulSet / PV/PVC migrations. These remain component-scoped because they
require a live `blocknode.Manager`, a Helm client, and a Kube client at `Execute()` time, and
they have a custom two-phase workflow (PV/PVC creation → single Helm upgrade) that cannot be
expressed as a generic `Migration.Execute()`.

---

## Concept 2 — Single `RegisterMigrations()`, Scope as the Tag

`migration.Register()` already accepts a component string that doubles as the scope tag. All
migrations are registered in one place, in chronological order:

```go
// cmd/weaver/commands/root.go — the ONE authoritative list
func RegisterMigrations() {
    // ── Startup migrations (run on every CLI invocation) ──────────────────────
    // Ordered chronologically: earlier breaking changes first.
    migration.Register("startup", state.NewUnifiedStateMigration())          // pre-v0.13
    migration.Register("startup", state.NewHelmReleaseSchemaV2Migration())   // v0.14.0
    migration.Register("startup", workflows.NewLegacyBinaryMigration())      // v0.13.x

    // ── Block-node upgrade migrations (run during block node upgrade) ─────────
    migration.Register("block-node", blocknode.NewVerificationStorageMigration()) // v0.26.2
    migration.Register("block-node", blocknode.NewPluginsStorageMigration())      // v0.28.x
}
```

Registration order within `"startup"` **is** the canonical execution order.
Future migrations are appended chronologically. This replaces all three `InitMigrations()`
functions — they are deleted entirely.

---

## Concept 3 — CLI Version as the Migration Gate

`state.ProvisionerState.Version` (e.g. `"0.13.0"`) is the *"last CLI version that successfully
ran"*. It is already written to disk by `NewState()` on fresh installs, but `Refresh()` currently
does not update it when a newer CLI version runs. This must be fixed.

**New behaviour in `Refresh()`:**

```go
// internal/state/state_manager.go — Refresh()
// After loading state from disk:
if newState.ProvisionerState.Version != version.Number() {
    newState.ProvisionerState.Version = version.Number()
    // Reaches disk on the next FlushState() call
}
```

This establishes a clear two-value invariant at migration time:

| What | Value | Where it comes from |
|---|---|---|
| `lastCLIVersion` | e.g. `"0.13.0"` | Raw state file on disk **before** `Refresh()` |
| `currentCLIVersion` | e.g. `"0.14.0"` | `version.Number()` embedded in the binary |

The migration runner reads `lastCLIVersion` from the **raw state file** before `Refresh()` ever
runs, exactly as `isV1StateFile()` already does today. After a successful run, `Refresh()` (which
fires in `initializeDependencies()`) stamps `currentCLIVersion` into memory, and the first
`FlushState()` call writes it to disk — ready to gate the next invocation.

---

## Concept 4 — Enriched `migration.Context` with CLI Versions

Two new well-known context keys are added alongside the existing chart-version keys:

```go
// internal/migration/migration.go
const (
    // Startup migration keys — populated by RunStartupMigrations()
    CtxKeyInstalledCLIVersion = "installedCLIVersion" // state.provisioner.version on disk
    CtxKeyCurrentCLIVersion   = "currentCLIVersion"   // version.Number()

    // Block-node upgrade keys — populated by blocknode.BuildMigrationWorkflow()
    CtxKeyInstalledVersion = "installedVersion" // existing
    CtxKeyTargetVersion    = "targetVersion"    // existing
)
```

A new `CLIVersionMigration` base type (analogous to the existing `VersionMigration`) gives
developers an `Applies()` implementation for free:

```go
// internal/migration/cli_version_migration.go
type CLIVersionMigration struct {
    id, description, minCLIVersion string
}

// Applies returns true when the CLI is upgrading across the minCLIVersion boundary:
//   lastCLIVersion < minCLIVersion ≤ currentCLIVersion
// Returns false on fresh installs (empty lastCLIVersion).
func (c *CLIVersionMigration) Applies(mctx *Context) (bool, error) { … }
```

State-based migrations (`UnifiedState`, `HelmReleaseSchemaV2`, `LegacyBinary`) keep their own
`Applies()` implementations that inspect filesystem/on-disk state. They ignore CLI version keys
entirely — they are already idempotent by design.

---

## Concept 5 — Unified Startup Migration Runner

`RunStateMigrations()` in `run.go` is replaced by `RunStartupMigrations()`, which drives a single
ordered pass over all `"startup"`-scoped migrations:

```go
// cmd/weaver/commands/common/run.go
func RunStartupMigrations(ctx context.Context) error {
    // 1. Read last CLI version from raw state file (before Refresh overwrites in memory)
    lastCLIVersion, err := state.ReadProvisionerVersionFromDisk()
    if err != nil {
        return err
    }

    // 2. Build a single context for all startup migrations
    mctx := &migration.Context{
        Component: migration.ScopeStartup, // = "startup"
        Data:      &automa.SyncStateBag{},
    }
    mctx.Data.Set(migration.CtxKeyInstalledCLIVersion, lastCLIVersion)
    mctx.Data.Set(migration.CtxKeyCurrentCLIVersion,   version.Number())

    // 3. Get all applicable "startup" migrations in registration order
    migrations, err := migration.GetApplicableMigrations(migration.ScopeStartup, mctx)
    if err != nil {
        return err
    }
    if len(migrations) == 0 {
        logx.As().Debug().Msg("No startup migrations needed")
        return nil
    }

    // 4. Build and execute the workflow (RollbackOnError mode)
    wf, err := migration.MigrationsToWorkflow(migrations, mctx).Build()
    if err != nil {
        return err
    }
    logx.As().Info().Int("count", len(migrations)).Msg("Running startup migrations...")
    report := wf.Execute(ctx)
    if report.Error != nil {
        return report.Error
    }
    logx.As().Info().Msg("Startup migrations completed successfully")
    return nil
}
```

`state.BuildMigrationWorkflow()` and `workflows.BuildMigrationWorkflow()` are **deleted**.
`blocknode.BuildMigrationWorkflow()` is **kept** (see Concept 6).

A new exported helper `state.ReadProvisionerVersionFromDisk()` reads the raw YAML and extracts
`state.provisioner.version` using the `yaml.Node` helpers already in `yaml_helpers.go`. It
returns `""` on a fresh install (no state file), which causes all version-gated startup migrations
to return `false` from `Applies()` — the correct behaviour for a fresh install.

---

## Concept 6 — `blocknode.BuildMigrationWorkflow()` Keeps Its Special Logic

The block-node upgrade workflow has a two-phase structure that cannot be expressed as a
generic `Migration.Execute()`:

1. **Phase 1 (per migration):** Create storage directory + PV/PVC.
2. **Phase 2 (single upgrade step):** Delete StatefulSet (orphan cascade) + single Helm upgrade
   to the target version.

This constraint is imposed by Kubernetes: `volumeClaimTemplates` in a StatefulSet cannot be
updated in-place, so the StatefulSet must be deleted before the Helm upgrade.

`blocknode.BuildMigrationWorkflow(manager, profile, valuesFile)` therefore stays unchanged.
What **does** change:

- `blocknode.InitMigrations()` is **deleted**
- Both storage migrations are registered in `root.go RegisterMigrations()` under `"block-node"`
- `blocknode.BuildMigrationWorkflow()` continues to call
  `migration.GetApplicableMigrations("block-node", mctx)` with chart-version context — no change
  to its callers

---

## Idempotency vs Migration History Tracking

### The question

Two approaches exist for ensuring a migration does not run twice:

| Approach | Mechanism | Complexity |
|---|---|---|
| **A — Persistent ledger** | A `migrations.yaml` file records completed migration IDs; the runner skips any ID already in the ledger | High |
| **B — Idempotent `Applies()`** | Each migration's `Applies()` returns `false` once the migration has been applied; `Execute()` is safe to call twice | Low |

### Recommendation: idempotent `Applies()` — no ledger

All current startup migrations already follow the idempotent `Applies()` pattern:

| Migration | How `Applies()` returns `false` after execution |
|---|---|
| `UnifiedStateMigration` | Legacy `*.installed` / `*.configured` files no longer exist |
| `HelmReleaseSchemaV2Migration` | `state.version` is now `"v2"`, not `"v1"` |
| `LegacyBinaryMigration` | `weaver` binary has been removed |
| `StorageMigration` (blocknode) | `installedChartVersion ≥ minVersion` — version boundary crossed |

A persistent ledger would add complexity without improving correctness:

- **It can become wrong.** If the state file is deleted and recreated (e.g., disaster recovery),
  the ledger says "already run" but the migration is genuinely needed again. `Applies()` reflects
  the actual ground truth; a ledger does not.
- **Rollback breaks it.** After a `Rollback()`, the migration must be eligible to re-run.
  A ledger would need manual intervention; an idempotent `Applies()` handles this automatically
  because `Rollback()` restores the pre-migration state that `Applies()` detects.
- **`ProvisionerState.Version` is already a lightweight ledger.** For CLI-version-gated
  migrations, the on-disk `state.provisioner.version` advances after each successful run. This
  is exactly what a ledger entry would track, but it is derived from existing domain data rather
  than a separate file.

### The idempotency contract

Every migration author **must** uphold this contract:

#### `Applies()` contract

- **Post-condition:** Returns `false` after a successful `Execute()`, without exception.
- **Fresh-install:** Returns `false` when no prior state exists.
- **Ground-truth:** Reflects actual on-disk / in-cluster state, not in-memory assumptions.

#### `Execute()` contract

- **Check-then-act:** Verify the precondition before acting (e.g., check if a key exists before
  renaming it, check if a file exists before removing it).
- **Safe to call twice:** Calling `Execute()` when the migration is already applied must not
  corrupt state. `Applies()` prevents this under normal conditions; defensive `Execute()` handles
  edge cases.
- **Reversible by `Rollback()`:** Every mutation in `Execute()` must have a corresponding
  undo in `Rollback()`.

#### `Rollback()` contract

- **Restores pre-migration state:** After `Rollback()`, `Applies()` must return `true` again —
  i.e., the migration is eligible to re-run.
- **Best-effort, never panics:** Log and continue on partial failure rather than aborting.

### Example: idempotent `Execute()`

```go
// Idempotent key rename — safe to call even if already renamed
func renameMappingKey(node *yaml.Node, oldKey, newKey string) {
    // If oldKey is already gone (was renamed in a prior run), this is a no-op.
    // If newKey already exists with the right value, this is also a no-op.
    for i := 0; i+1 < len(node.Content); i += 2 {
        if node.Content[i].Value == oldKey {
            node.Content[i].Value = newKey
            return
        }
    }
    // oldKey not found — already renamed or never existed. Safe to ignore.
}
```

### Future option: lightweight audit trail (not recommended now)

If an audit trail of completed migrations becomes necessary in future, the least-invasive approach
is to append completed migration IDs to `StateRecord`:

```go
type StateRecord struct {
    // ... existing fields ...
    CompletedMigrations []string `yaml:"completedMigrations,omitempty" json:"completedMigrations,omitempty"`
}
```

The runner appends a migration's ID to this list after `Execute()` succeeds. The runner can check
this list as a fast-path skip **before** calling `Applies()`. `Applies()` remains the authoritative
guard — the list is advisory only. This hybrid keeps correctness in `Applies()` while adding
an auditable record for operators.

This is deferred until there is a demonstrated need.

---

## What Changes — Summary

| File | Change |
|---|---|
| `internal/migration/migration.go` | Add `CtxKeyInstalledCLIVersion`, `CtxKeyCurrentCLIVersion`; add `ScopeStartup` constant |
| `internal/migration/cli_version_migration.go` | **New file** — `CLIVersionMigration` type using CLI version keys |
| `internal/migration/version_migration.go` | Unchanged (still used by blocknode chart-version migrations) |
| `internal/state/migrations.go` | **Delete** `InitMigrations()` and `BuildMigrationWorkflow()`; add `ReadProvisionerVersionFromDisk()` |
| `internal/workflows/migrations.go` | **Delete entire file** (`InitMigrations()` and `BuildMigrationWorkflow()` gone) |
| `internal/blocknode/migrations.go` | **Delete** `InitMigrations()` only; keep `BuildMigrationWorkflow()` |
| `internal/state/state_manager.go` | `Refresh()` stamps `ProvisionerState.Version = version.Number()` when it changes |
| `cmd/weaver/commands/root.go` | `RegisterMigrations()` becomes the single authoritative flat list |
| `cmd/weaver/commands/common/run.go` | `RunStateMigrations()` → `RunStartupMigrations()` with CLI-version context and unified `"startup"` pool |
| `docs/dev/migration-framework.md` | Full rewrite to reflect new model |

---

## New File Structure

```
internal/migration/
├── migration.go                ← add ScopeStartup, CtxKeyInstalledCLIVersion, CtxKeyCurrentCLIVersion
├── version_migration.go        ← unchanged (chart-version gates for blocknode)
├── cli_version_migration.go    ← NEW: CLIVersionMigration (CLI-version gates for startup)
└── migration_test.go

internal/state/
├── yaml_helpers.go                      ← unchanged
├── migration_unified_state.go           ← unchanged
├── migration_unified_state_test.go      ← unchanged
├── migration_helm_release_schema_v2.go  ← unchanged
├── migration_helm_release_schema_v2_test.go ← unchanged
└── migrations.go                        ← SHRINKS: InitMigrations() deleted,
                                            BuildMigrationWorkflow() deleted,
                                            ReadProvisionerVersionFromDisk() added

internal/blocknode/
├── migrations.go               ← InitMigrations() deleted; BuildMigrationWorkflow() kept
└── migration_storage.go        ← unchanged

internal/workflows/
└── migrations.go               ← DELETED (or emptied to just the package comment)

cmd/weaver/commands/
└── root.go                     ← RegisterMigrations() is the single authoritative list

cmd/weaver/commands/common/
└── run.go                      ← RunStateMigrations() → RunStartupMigrations()
```

---

## Adding a New Migration — Developer Guide

The new flow is **3 steps** instead of the previous 4:

### Step 1: Create the migration file

Create `internal/<package>/migration_<feature>.go` implementing `migration.Migration`.

Choose the right base type:

```go
// Option A: State-based — write your own Applies() that checks ground truth
type MyStateMigration struct{}

func (m *MyStateMigration) Applies(_ *migration.Context) (bool, error) {
    // Check actual filesystem/state. Must return false after Execute() succeeds.
    _, err := os.Stat(legacyFilePath)
    return err == nil, nil
}

// Option B: CLI-version-gated — embed CLIVersionMigration for Applies() for free
type MyVersionedMigration struct {
    migration.CLIVersionMigration
}

func NewMyVersionedMigration() *MyVersionedMigration {
    return &MyVersionedMigration{
        CLIVersionMigration: migration.NewCLIVersionMigration(
            "my-feature-v0.15.0",           // ID
            "Describe what this fixes",      // Description
            "0.15.0",                        // minCLIVersion
        ),
    }
}
```

Uphold the idempotency contract: `Applies()` returns `false` after `Execute()`, and `Execute()`
is safe to call twice (check-then-act pattern).

### Step 2: Register in `root.go`

Append to `RegisterMigrations()` at the chronologically correct position:

```go
// cmd/weaver/commands/root.go
func RegisterMigrations() {
    migration.Register("startup", state.NewUnifiedStateMigration())
    migration.Register("startup", state.NewHelmReleaseSchemaV2Migration())
    migration.Register("startup", workflows.NewLegacyBinaryMigration())
    migration.Register("startup", mypackage.NewMyVersionedMigration()) // ← append here

    migration.Register("block-node", blocknode.NewVerificationStorageMigration())
    migration.Register("block-node", blocknode.NewPluginsStorageMigration())
}
```

### Step 3: Done

No `InitMigrations()` to update. No `BuildMigrationWorkflow()` to touch.
The migration will run automatically on the next CLI invocation once `Applies()` returns `true`.

---

## Further Considerations

### `ReadProvisionerVersionFromDisk()` implementation

Should live in `internal/state/migrations.go` (or `yaml_helpers.go`) and use the existing
`yaml.Node` helpers to extract `state.provisioner.version` from the raw YAML file. It returns
`""` when the file does not exist (fresh install), which causes all `CLIVersionMigration`-based
`Applies()` calls to return `false` — the correct behaviour for a node that has never run the
CLI before.

```go
// internal/state/migrations.go
func ReadProvisionerVersionFromDisk() (string, error) {
    b, err := os.ReadFile(filepath.Join(models.Paths().StateDir, StateFileName))
    if errors.Is(err, os.ErrNotExist) {
        return "", nil // fresh install
    }
    if err != nil {
        return "", err
    }
    var doc yaml.Node
    if err := yaml.Unmarshal(b, &doc); err != nil {
        return "", err
    }
    stateNode := mappingValue(rootMappingNode(&doc), "state")
    provNode  := mappingValue(stateNode, "provisioner")
    return mappingScalar(provNode, "version"), nil
}
```

### `ScopeStartup` constant

Introduce `migration.ScopeStartup = "startup"` and `migration.ScopeBlockNode = "block-node"` as
typed constants to eliminate bare strings and prevent typos:

```go
const (
    ScopeStartup   = "startup"
    ScopeBlockNode = "block-node"
)
```

### Backward compatibility of `blocknode.BuildMigrationWorkflow()`

The block-node upgrade path reads from `migration.GetApplicableMigrations("block-node", mctx)`
with no change to its callers or context setup. As long as `RegisterMigrations()` continues to
tag blocknode migrations with `"block-node"`, this path is completely unaffected.

### Test coverage

Each migration should have a test that asserts the idempotency contract directly:

```go
func TestMyMigration_AppliesReturnsFalseAfterExecute(t *testing.T) {
    m := migrationWithTempState(t, legacyYAML)
    require.NoError(t, m.Execute(context.Background(), nil))
    applies, err := m.Applies(nil)
    require.NoError(t, err)
    assert.False(t, applies, "Applies() must return false after Execute()")
}
```
