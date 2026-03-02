# Effective Value Resolution — Architecture Guide

This document explains how Solo Weaver resolves the **effective value** for each
configurable field (namespace, version, chart, storage, …) when a CLI command
is executed.  It covers the roles of `config`, `state`, `reality`, `rsl`,
`resolver`, and `bll` and how they compose into a single, deterministic answer.

---

## Overview

Every field that appears in a Helm deployment has three possible sources of
truth.  Weaver must pick exactly one winner in a well-defined priority order:

```
Priority (highest → lowest)
─────────────────────────────────────────────────────────
StrategyCurrent    │ The resource is already deployed.
                   │ The live cluster owns the value.
───────────────────┤
StrategyUserInput  │ The operator explicitly passed a flag
                   │ (e.g. --version 0.23.0).
───────────────────┤
StrategyConfig     │ The default from config.yaml.
─────────────────────────────────────────────────────────
```

The winner is wrapped in an `automa.EffectiveValue[T]` that carries both the
value and the strategy that selected it, so every downstream layer knows *how*
the value was chosen.

---

## Package Responsibilities

### `pkg/models` — data shapes

Holds the plain data structs shared across every layer:

| Type | Role |
|---|---|
| `Config` / `BlockNodeConfig` | Configuration defaults loaded from `config.yaml` |
| `BlocknodeInputs` | Raw user-supplied flag values from the CLI |
| `UserInputs[T]` | Generic wrapper pairing `CommonInputs` with node-specific `T` |
| `BlockNodeStorage` | Storage path/size fields for a block-node PV set |

No logic lives here — only data and validation.

---

### `internal/config` — configuration loading

Loads `config.yaml` from disk once at startup and exposes it as
`config.Get() models.Config`.  All config defaults flow into `rsl` at
construction time via `NewRegistry(cfg, …)`.

---

### `internal/state` — persisted application state

`state.yaml` is Weaver's memory between runs.  It records:

- Whether the Kubernetes cluster has been created
- The last known `HelmReleaseInfo` for each deployed node type
  (name, namespace, version, chart, status, first/last deployed timestamps)
- Storage paths discovered from PersistentVolumes
- The last action taken (intent + inputs) for audit purposes

The state package exposes **three narrow interfaces** so each consumer only
receives the capability it actually needs:

| Interface | Methods | Typical consumer |
|---|---|---|
| `Reader` | `State() State`, `HasPersistedState()` | `rsl`, precondition checks, reality checker |
| `Writer` | `Set(State) Writer`, `AddActionHistory(…) Writer`, `Flush() error` | `bll.FlushNodeState` |
| `Persister` | `Refresh() error`, `FileManager()` | composition root (`init.go`) |

`DefaultStateManager` composes all three — the concrete `stateManager` satisfies
all of them.  At the DI boundary (`init.go`), the same `sm` is passed as both
`state.Reader` and `state.Writer`, keeping the fields on `NodeHandlerBase`
narrow.

Key design points:

- **`State()` returns a value copy** — callers cannot mutate the manager's
  internal state through the returned value.
- **`Set()` and `AddActionHistory()` return `Writer`** — not
  `DefaultStateManager`.  This keeps the chaining idiom (`Set(s).Flush()`)
  available on the narrow `Writer` interface without leaking the full manager
  type to callers who only hold a `Writer`.
- **All methods are mutex-protected** — `State()`, `Set()`,
  `AddActionHistory()`, `Flush()`, and `Refresh()` all acquire the internal
  mutex.  `Flush()` captures the snapshot and clears the pending action buffer
  under the lock, then releases it before the (slow) disk write so that
  `State()` and `Set()` are never blocked waiting for I/O.
- **`AddActionHistory` is in-memory only** — it does not call `Flush()`.
  The single explicit `Flush()` call in `FlushNodeState` writes both the state
  snapshot and all pending action entries atomically in one pass.

`state.yaml` is the **only** place Weaver records what it has done.  It is
intentionally never the source of truth for live cluster status — that role
belongs to `reality`.

---

### `internal/reality` — live cluster queries

`reality.Checker` answers "what is **actually** deployed right now?" by
making real network calls:

- **`BlockNodeState`** — lists all Helm releases, finds the one with a
  `StatefulSet` labelled `app.kubernetes.io/instance: block-node`, reads
  `PersistentVolumes` to reconstruct storage paths, returns
  `state.BlockNodeState`.
- **`ClusterState`** — calls the Kubernetes API to check whether a cluster
  exists and retrieves node info.
- **`MachineState`** — hardware/OS checks (partially implemented).

`reality` is **stateless** and **always fresh** — each call goes to the
cluster.  It is expensive by design and is only invoked deliberately by `rsl`.

```
reality.Checker
  ├── BlockNodeState(ctx) → talks to Helm + k8s API → state.BlockNodeState
  ├── ClusterState(ctx)   → talks to k8s API         → state.ClusterState
  └── MachineState(ctx)   → OS/hardware checks        → state.MachineState
```

---

### `internal/resolver` — pure value selection and validation

`resolver` is a **pure, side-effect-free** package.  Given the three sources,
it picks the winner.  No I/O, no locks, no network.

#### Core selection: `WithFunc`

```go
func WithFunc[T any](
    defaultVal automa.Value[T],   // from config
    userInput  automa.Value[T],   // from CLI flags
    currentVal T,                 // from live/persisted state
    isDeployed func() bool,
    equal      func(a, b T) bool, // nil → reflect.DeepEqual
    isEmpty    func(v T) bool,    // nil → reflect.Value.IsZero
) (*automa.EffectiveValue[T], error)
```

Decision tree:

```
isDeployed()           → StrategyCurrent   (currentVal wins)
userInput != nil
  && !isEmpty(user)    → StrategyUserInput  (user flag wins)
else                   → StrategyConfig    (config default wins)
```

#### Convenience wrapper: `ForStatus`

Derives `isDeployed` from a Helm `release.Status`:

```go
resolver.ForStatus(def, user, current, release.StatusDeployed, true)
// isDeployed = (status == StatusDeployed)
```

This is the standard path used by all simple scalar fields.

#### Post-selection validation: `Validator[T]` and `Field`

`Field` composes selection with zero or more domain validators:

```go
resolver.Field(
    selectionFn func() (*automa.EffectiveValue[T], error),
    validators  ...Validator[T],
)
```

Two built-in validators:

| Validator | When it fires | Use case |
|---|---|---|
| `ImmutableOnDeploy` | `StrategyUserInput` while deployed | Fields that can never change once set (e.g. release name in some deployments) |
| `RequiresExplicitOverride` | `StrategyCurrent won + user supplied input + --force` | Fields that CAN change (via upgrade) but must not be silently overridden during a plain install |

Validators are **pure functions** — testable without any infrastructure.

---

### `internal/rsl` — Runtime State Layer

`rsl` is the bridge between `reality` (expensive, fresh) and `resolver`
(cheap, pure).  It holds:

1. A **thread-safe cached snapshot** of live state (`Base[T].current`)
2. A set of **`automa.RuntimeValue[T]`** — one per field — each with a
   baked-in effective function that calls `resolver.ForStatus`

#### `Base[T]` — generic refresh/cache engine

```
Base[T]
  ├── current T           — cached live snapshot (protected by mu)
  ├── refreshInterval     — minimum time between reality calls (default 10 min)
  ├── fetch(ctx) (T,err)  — the reality.Checker method to call
  └── RefreshState(ctx, force bool)
        if force || stale  → calls fetch() → updates current
        else               → returns immediately (cache hit)
```

`RefreshState(ctx, force=false)` — used **before** a workflow runs.  Respects
the cache; avoids hitting the cluster if state is fresh.

`RefreshState(ctx, force=true)` — used **after** a workflow runs.  Always
calls reality, because the cluster state just changed.

#### `BlockNodeRuntimeState` — per-field resolution

Each string field is initialised by `initStringField` at construction time:

```go
automa.NewRuntime[string](
    defaultVal,
    automa.WithEffectiveFunc(func(ctx, def, user) (*EffectiveValue, bool, error) {
        current := br.current   // cached snapshot, protected by mu
        return resolver.ForStatus(def, user, currentFn(current),
                                  current.ReleaseInfo.Status, true)
    }),
)
```

When `br.namespace.Effective()` is called:
1. The baked-in function runs with the stored config default and the user input
   that was pushed via `SetUserInputs`
2. It calls `resolver.ForStatus` with the cached `current` state
3. Returns `*automa.EffectiveValue[string]` tagged with the winning strategy

`SetUserInputs(inputs)` pushes CLI flag values into each `RuntimeValue` so
that the next `Effective()` call sees them:

```go
rsl.BlockNode.SetUserInputs(inputs.Custom)
// stores inputs.Custom.Namespace into br.namespace.userInput
// stores inputs.Custom.Version  into br.version.userInput
// etc.
```

#### `rsl.Registry` — composition root for runtimes

```
rsl.Registry
  ├── BlockNode *BlockNodeRuntimeState
  └── Cluster   *ClusterRuntime
```

Constructed once at startup by `rsl.NewRegistry(cfg, state, realityChecker, interval)`.
Injected into `bll` via dependency injection — no package-level singletons.

---

### `internal/bll` — Business Logic Layer

`bll` owns intent routing, effective-value preparation, workflow orchestration,
and state flushing.  It sits between the CLI and the workflow/step layer.

#### `NodeHandlerBase` — shared infrastructure

Holds the three dependencies every node handler needs:

```go
type NodeHandlerBase struct {
    StateReader state.Reader   // read-only state access
    StateWriter state.Writer   // write + flush access
    RSL         *rsl.Registry  // runtime state + field resolution
}
```

Two shared operations built on top:

**`RefreshRuntimeState(target, setUserInputsFn)`** — called before every
workflow:
1. Calls `setUserInputsFn()` → pushes CLI inputs into `RSL.BlockNode`
2. `RSL.Cluster.RefreshState(ctx, force=false)` — respect cache
3. `RSL.BlockNode.RefreshState(ctx, force=false)` — respect cache

**`FlushNodeState(base, report, intent, effectiveInputs, patchState)`** —
called after every workflow:
1. Records intent + inputs in action history
2. `RSL.Cluster.RefreshState(ctx, force=true)` — cluster just changed
3. `RSL.BlockNode.RefreshState(ctx, force=true)` — node just changed
4. Calls `patchState` to augment state from fresh rsl snapshot
5. `StateWriter.Set(full).Flush()` — persist to `state.yaml`

#### `ActionHandler[I]` — per-action contract

```go
type ActionHandler[I any] interface {
    PrepareEffectiveInputs(*models.UserInputs[I]) (*models.UserInputs[I], error)
    BuildWorkflow(nodeState, clusterState, *models.UserInputs[I]) (*automa.WorkflowBuilder, error)
}
```

Each action (install, upgrade, reset, uninstall) is a separate file
implementing this interface.  Adding a new action is a new file only.

#### `bll/blocknode.Handler.HandleIntent` — the 5-step pipeline

```
Step 1  Validate intent + inputs (schema, required fields, profile, etc.)
         │
Step 2  RefreshRuntimeState
         │  SetUserInputs(inputs.Custom) → push flags into RuntimeValues
         │  RSL.Cluster.RefreshState(force=false)    ← respects cache
         │  RSL.BlockNode.RefreshState(force=false)  ← respects cache
         │
Step 3  Route to per-action handler
         │  PrepareEffectiveInputs  ← calls rsl accessors + resolver.Field
         │  CurrentState()          ← read cached snapshot
         │  BuildWorkflow           ← precondition checks → WorkflowBuilder
         │
Step 4  Execute workflow
         │  wf.Execute(ctx)         ← automa drives steps with rollback
         │
Step 5  FlushNodeState
         │  [skip entirely if failed + ContinueOnError — state is indeterminate]
         │  AddActionHistory(intent, effectiveInputs)  ← in-memory only
         │  RSL.Cluster.RefreshState(force=true)    ← cluster changed
         │  RSL.BlockNode.RefreshState(force=true)  ← node changed
         │  patchState(BlockNodePatchState)
         │  StateWriter.Set(full).Flush()            ← single write to state.yaml
```

Note: `StopOnError` failures are **not** skipped — rollback will have run and
the post-rollback cluster state should be persisted.

#### Install vs Upgrade: validator differences

Both handlers use `resolver.Field` for every field — the call pattern is
identical.  The difference is which validators are registered:

```go
// install_handler.go — guards against force-overriding a live deployment
resolver.Field(
    func() (*EffectiveValue[string], error) { return h.base.Version() },
    resolver.RequiresExplicitOverride("version", inputs.Custom.Version != "", force, hint),
)

// upgrade_handler.go — no validators; operator explicitly wants to change fields
resolver.Field(
    func() (*EffectiveValue[string], error) { return h.base.Version() },
    // intentionally empty — upgrade is permitted to change any field
)
```

The zero-validator `resolver.Field` calls in `UpgradeHandler` make the absence
of guards **visible at the call site** and allow validators to be added later
with a one-line change.

---

## End-to-End Data Flow for a Single Field

The following traces `namespace` through a `weaver block node install` with
a block node already deployed:

```
1. CLI parses --namespace flag (or omits it)
        │
        ▼
2. prepareBlocknodeInputs() → models.BlocknodeInputs{Namespace: "new-ns"}
        │
        ▼
3. HandleIntent receives inputs
        │
        ▼
4. RefreshRuntimeState
   └── RSL.BlockNode.SetUserInputs(inputs.Custom)
           br.namespace.SetUserInput(automa.NewValue("new-ns")) ← stored in RuntimeValue
   └── RSL.BlockNode.RefreshState(ctx, force=false)
           if stale → reality.BlockNodeState(ctx)
                          Helm list → find StatefulSet 
                          → state.BlockNodeState{ReleaseInfo.Namespace: "block-node-ns",
                                                 ReleaseInfo.Status: StatusDeployed}
           br.current = freshSnapshot
        │
        ▼
5. PrepareEffectiveInputs (InstallHandler)
   └── resolver.Field(
           fn:  h.base.Namespace()
                  └── br.namespace.Effective()
                          └── baked effectiveFunc runs:
                                  def  = "block-node-ns"  (from config)
                                  user = "new-ns"          (from SetUserInputs)
                                  resolver.ForStatus(def, user,
                                      currentFn(br.current) = "block-node-ns",
                                      StatusDeployed,
                                      true)
                                  → isDeployed=true
                                  → StrategyCurrent wins
                                  → EffectiveValue{"block-node-ns", StrategyCurrent}
           validator: RequiresExplicitOverride(
                          hasInput = true,   ← user passed --namespace
                          force    = false,  ← no --force flag
                      )
                      → force=false → guard does NOT fire → OK
       )
   └── effective.Custom.Namespace = "block-node-ns"
        │
        ▼
6. BuildWorkflow — preconditions pass
        │
        ▼
7. wf.Execute(ctx) — Helm install/upgrade steps run
        │
        ▼
8. FlushNodeState
   └── RSL.BlockNode.RefreshState(ctx, force=true)
           reality.BlockNodeState(ctx) → fresh from cluster
           br.current = updatedSnapshot
   └── BlockNodePatchState → bnState = RSL.BlockNode.CurrentState()
   └── StateWriter.Set(full).Flush() → state.yaml updated
```

---

## Adding a New Field

1. **`pkg/models`** — add the field to `BlocknodeInputs` and `BlockNodeConfig`.
2. **`internal/rsl/block_node.go`** — add an `init*Runtime()` method (use
   `initStringField` for scalars), call it in `NewBlockNodeRuntime`, and expose
   a public `Accessor()` method.
3. **`internal/bll/blocknode/internal.go`** — add the method to `rslAccessor`
   and forward it in `registryAccessor`.
4. **`internal/bll/blocknode/install_handler.go`** — add a `resolver.Field`
   call with the appropriate validator(s).
5. **`internal/bll/blocknode/upgrade_handler.go`** — add a `resolver.Field`
   call with zero validators.
6. Add the field to the `effective` struct in both handlers.
7. Add `SetUserInputs` forwarding in `BlockNodeRuntimeState.SetUserInputs`.
8. Write unit tests for the new resolver path in `prepare_inputs_test.go`.

---

## Adding a New Node Type (e.g. MirrorNode)

1. Create `internal/rsl/mirror_node.go` — mirror `block_node.go`.
2. Add `MirrorNode *MirrorNodeRuntimeState` to `rsl.Registry`.
3. Create `internal/bll/mirrornode/` package with:
   - `handler.go` — routing Handler
   - `install_handler.go`, `upgrade_handler.go`, … — ActionHandler implementations
   - `internal.go` — `rslAccessor` interface + `registryAccessor` for MirrorNode
4. Create `cmd/weaver/commands/mirror/node/` with cobra commands.
5. No changes needed to `resolver`, `reality`, or `bll` base.

---

## Why This Layering?

| Concern | Package | Reason for separation |
|---|---|---|
| Live cluster truth | `reality` | Network calls; expensive; always fresh |
| Cached runtime state + field merging | `rsl` | Thread safety; refresh interval; bridges reality → resolver |
| Value selection algorithm | `resolver` | Pure functions; independently testable; reusable across node types |
| Domain constraints (immutability, override guards) | `resolver.Validator` | Co-located with selection; zero I/O; one-line addition |
| Intent routing + workflow lifecycle | `bll` | Single orchestration point; no field-level logic leaks upward |
| Persistence | `state` | Separates "what we've done" from "what is actually there" |
| Configuration defaults | `config` / `models` | Read-only after startup; injected, not global |

