# Effective Value Resolution — RSL Architecture Guide

This document explains how Solo Weaver resolves the **effective value** for each
configurable field (namespace, chart version, storage paths, …) when a CLI command
runs.  It covers the `rsl` package in full: strategies, `EffectiveValue[T]`,
custom selectors, the builder API, and end-to-end data flow.

---

## The Problem

Every configurable field has up to seven possible sources of truth.  Weaver must
pick exactly one winner in a deterministic, auditable priority order, and it must
be possible to inspect which source won — both for logging and for tests.

The answer is the **Runtime State Layer** (`internal/rsl`).

---

## Strategy Precedence

Each source is identified by an `automa.EffectiveStrategy` constant.  Higher
numeric value means higher precedence.

```
Priority  Constant           Source
────────  ─────────────────  ──────────────────────────────────────────────────
  106     StrategyUserInput  CLI flag supplied by the operator (--namespace, …)
  105     StrategyReality    Live Helm query via reality.Checker (after refresh)
  104     StrategyState      Persisted state.yaml on disk (from WithState)
  103     StrategyConfig     config.yaml loaded at startup (from WithConfig)
  102     StrategyEnv        SOLO_PROVISIONER_* env vars (from WithEnv)
  101     StrategyDefault    Hardcoded deps.* constants (from WithDefaults)
  100     StrategyZero       Ultimate fallback — zero value of T
```

> **Note on numeric values vs precedence:** the constants do *not* encode
> precedence by their numeric value.  Precedence is defined by the
> `defaultOrderedStrategies` slice in `effective.go`:
>
> ```
> Reality → State → UserInput → Env → Config → Default → Zero
> ```
>
> `StrategyUserInput (106) > StrategyReality (105)` numerically, but Reality
> wins in the precedence walk because it appears first in that slice.

---

## Core Types

### `EffectiveValue[T]`

The per-field container.  Holds:

- `sources map[EffectiveStrategy]automa.Value[T]` — one entry per registered source
- `selector Selector[T]` — the algorithm that picks the winner
- A lazily-computed cached result (invalidated by any mutation)

```go
// Create with a selector; sources map starts empty.
ev, _ := rsl.NewEffectiveValue[string](&rsl.DefaultSelector[string]{})

// Register sources as they become available.
_ = ev.SetSource(rsl.StrategyDefault, "block-node")
_ = ev.SetSource(rsl.StrategyConfig,  "my-node")     // wins

// Resolve lazily; result cached until next SetSource/ClearSource.
winner, err := ev.Resolve()        // *automa.EffectiveValue[string]
fmt.Println(winner.Get().Val())    // "my-node"
fmt.Println(ev.StrategyName())     // "config"

// Inspect individual layers without triggering resolution.
defVal, _ := ev.DefaultVal()       // automa.Value[string]{"block-node"}
cfgVal, _ := ev.ConfigVal()        // automa.Value[string]{"my-node"}

// Human-readable summary for log lines.
fmt.Println(ev.String())
// strategy=config value=my-node sources=[config:my-node, default:block-node]
```

**Key invariant:** `SetSource(strategy, "")` is equivalent to registering an
empty source, which *will* shadow lower-priority strategies in a
presence-only check (`if v, ok := sources[st]; ok`).  Therefore callers must
**never register empty values**; they should call `ClearSource(strategy)` instead.
The `setOrClearString` helper in `block_node_runtime.go` enforces this for string
fields.

### `Selector[T]`

The algorithm interface:

```go
type Selector[T any] interface {
    Resolve(sources map[automa.EffectiveStrategy]automa.Value[T]) (*automa.EffectiveValue[T], error)
}
```

A `Selector` receives the full sources map and returns the winning
`*automa.EffectiveValue[T]` together with any error.  An error signals that
resolution is impossible (e.g. a deployed release has an empty required field).

### `DefaultSelector[T]`

The standard walk: iterates `defaultOrderedStrategies` and returns the first
strategy whose key exists in the map, regardless of the value.  This is correct
because empty values are never registered (see the invariant above).

---

## Custom Selectors in `block_node_runtime.go`

For fields with domain-specific rules, `BlockNodeRuntimeResolver` defines custom
`Selector` implementations.

### `validatedStringResolver`

Used for: `namespace`, `releaseName`, `chartName`.

```
Reality / State present?
  └── value empty?  → error (deployed release cannot have empty <field>)
  └── non-empty?    → win with Reality/State

Not deployed → UserInput → Env → Config → Default → Zero
```

Rationale: if a Helm release is deployed, its namespace and release name are
locked.  An empty value indicates corrupted state and must surface immediately.

### `chartRefResolver`

Used for: `chartRef` (OCI registry URL).

Same as `validatedStringResolver` except that an empty deployed ChartRef logs
a **warning** and soft-falls through to Config/Default, rather than erroring.
This is because Helm does not always persist the chart ref in its metadata.

### `chartVersionResolver`

Used for: `chartVersion`.  Closes over `**models.Intent`.

```
Upgrade intent + not deployed?  → error (cannot upgrade what is not there)

Upgrade intent + deployed?
  → UserInput → Reality → State → Config → Default → error if none

Non-upgrade intent + deployed?
  → Reality / State  (locked; user cannot override the running version)
  → empty deployed version → error

Non-upgrade intent + not deployed?
  → UserInput → Env → Config → Default → Zero
```

Rationale: during upgrade the operator supplies the new target version; the
currently deployed version is still used if no user input is given.  During
any other action the deployed version is authoritative and must not be silently
changed.

### `storageResolver`

Used for: `storage` (`BlockNodeStorage`, a struct with 11 path/size fields).

Storage is a merge, not a winner-takes-all selection.  The resolver builds the
result by cascading `MergeFrom` calls (non-empty source fields fill gaps):

```
1. Start with UserInput (operator-supplied fields are highest priority).
2. MergeFrom Reality / State (validates deployed storage before merging).
3. MergeFrom Config (fills any remaining gaps from config.yaml).
4. MergeFrom Default (fills any remaining gaps from deps constants).

Winning strategy = highest-priority source that contributed a non-empty struct.
```

---

## `BlockNodeRuntimeResolver`

The concrete resolver that owns all per-field `*EffectiveValue[T]` instances
and exposes a fluent builder API.

### Builder API (`With*` methods)

These methods are called at startup and again whenever an input source changes.
Each one simply registers or clears sources on the per-field `EffectiveValue`
instances.  They do **not** trigger resolution.

| Method | Source registered | Precondition |
|---|---|---|
| `WithDefaults(cfg)` | `StrategyDefault` | `cfg` = `config.DefaultsConfig()` (deps constants) |
| `WithConfig(cfg)` | `StrategyConfig` | `cfg` = `config.Get()` (parsed config.yaml) |
| `WithEnv(cfg)` | `StrategyEnv` | `cfg` = `config.EnvConfig()` (SOLO_PROVISIONER_* env vars) |
| `WithUserInputs(inputs)` | `StrategyUserInput` | called after CLI flag parsing |
| `WithState(st)` | `StrategyState` | called after `state.Manager.Refresh()` |
| `WithIntent(intent)` | (no source; invalidates chartVersion cache) | called once per command |

**Important:** all `With*` methods use `setOrClearString` for string fields —
empty values **clear** the source entry rather than registering an empty string.
This is what prevents an absent `config.yaml` field from shadowing a non-empty
default.

### Initialization order

Sources are seeded lowest-priority-first so that the state at the end of
construction is coherent:

```go
// In NewBlockNodeRuntimeResolver:
br.WithConfig(cfg)         // StrategyConfig
br.WithState(blockNodeState) // StrategyState (may clear if not deployed)

// Called from the command layer (cmd/weaver/commands/block/node/init.go):
runtime.BlockNodeRuntime.WithDefaults(config.DefaultsConfig()) // StrategyDefault
runtime.BlockNodeRuntime.WithEnv(config.EnvConfig())           // StrategyEnv
```

`WithDefaults` and `WithEnv` are called from the command layer rather than the
constructor so that the composition root controls which deps-level constants and
env-var reader are injected.

### Field accessors

Each accessor resolves its field lazily and returns the `*EffectiveValue[T]`
so callers can both read the effective value and inspect individual source layers:

```go
ns, err := resolver.Namespace()     // triggers resolution; errors bubble up
val := ns.Get().Val()               // winning string
strategy := ns.StrategyName()       // "config", "state", "default", …
defVal, _ := ns.DefaultVal()        // raw value from StrategyDefault layer
logx.As().Debug().Any("ns", ns)     // calls ns.String() automatically
```

### `RefreshState` — reality integration

```go
err := resolver.RefreshState(ctx, force)
```

- Calls `reality.Checker.RefreshState(ctx)` to get a live `BlockNodeState`.
- Stamps `LastSync` and stores the snapshot.
- Calls `setStateSources(st, StrategyReality)` — populates `StrategyReality`
  for every field if the release is `StatusDeployed`, clears it otherwise.
- Respects `refreshInterval` when `force=false`; always calls when `force=true`.

---

## End-to-End: `namespace` during `block node install`

```
CLI parses --namespace my-ns (or omits it)
         │
         ▼
cmd/block/node/init.go
  NewBlockNodeRuntimeResolver(cfg, state, checker, interval)
    └── WithConfig(cfg)          → StrategyConfig = "config-ns" (from config.yaml)
    └── WithState(state)         → StrategyState = cleared (not deployed yet)
  WithDefaults(DefaultsConfig()) → StrategyDefault = "block-node"
  WithEnv(EnvConfig())           → StrategyEnv = "" → cleared (env var not set)
         │
         ▼
bll/blocknode.Handler.HandleIntent
  Step 1: validate inputs
  Step 2: runtime.Refresh(ctx, false)
    └── BlockNodeRuntime.WithState(refreshedState)  (state.yaml re-read)
    └── BlockNodeRuntime.RefreshState(ctx, false)   (reality check if stale)
  Step 3: WithUserInputs(inputs)
    └── StrategyUserInput = "my-ns"   (operator passed --namespace)
         │
         ▼
bll/blocknode/helpers.go  (PrepareEffectiveInputs)
  effNamespace, err := runtime.BlockNodeRuntime.Namespace()
    └── validatedStringResolver.Resolve(sources):
          Reality?    absent (not deployed) → skip
          State?      absent (not deployed) → skip
          UserInput?  "my-ns" → WIN
    └── returns *EffectiveValue{val: "my-ns", strategy: StrategyUserInput}
         │
         ▼
Workflow steps use "my-ns" as the Helm namespace.

After workflow completes:
  runtime.Refresh(ctx, force=true)    ← reality re-queried
  └── StrategyReality = "my-ns"       ← now deployed; reality wins
  state.yaml updated with new BlockNodeState
```

---

## Precedence Walk: Which Source Wins?

```
Reality deployed?  ──yes──►  Reality wins (always authoritative once live)
     │ no
     ▼
State deployed?    ──yes──►  State wins (non-upgrade locks the running config)
     │ no                    (chartVersionResolver: upgrade allows UserInput override)
     ▼
UserInput set?     ──yes──►  UserInput wins
     │ no
     ▼
Env var set?       ──yes──►  Env wins
     │ no
     ▼
Config field set?  ──yes──►  Config wins
     │ no
     ▼
Default set?       ──yes──►  Default wins
     │ no
     ▼
                             Zero value (empty string / zero struct)
```

---

## Source Inspection API

Every `*EffectiveValue[T]` exposes named accessors that return the raw value
for a specific strategy **without triggering resolution**:

```go
ev.DefaultVal()    // StrategyDefault source
ev.ConfigVal()     // StrategyConfig source
ev.EnvVal()        // StrategyEnv source
ev.UserInputVal()  // StrategyUserInput source
ev.StateVal()      // StrategyState source
ev.RealityVal()    // StrategyReality source
ev.ValOf(st)       // arbitrary strategy
```

All return `(automa.Value[T], error)`.  If the strategy has no registered source,
they return a zero-value `automa.Value[T]` — they never return an error.

Use these in tests to assert layer-level state without depending on the full
precedence walk:

```go
ns, _ := resolver.Namespace()
defVal, _ := ns.DefaultVal()
assert.Equal(t, "block-node", defVal.Val())
```

---

## Logging

Every `*EffectiveValue[T]` implements `fmt.Stringer`:

```go
logx.As().Debug().
    Any("namespace",     effNamespace).
    Any("chartVersion",  effChartVersion).
    Any("storage",       effStorage).
    Msg("Resolved effective block node inputs")
```

`logx.Any` calls `.String()` automatically, producing structured lines like:

```
namespace:    strategy=userInput value=my-ns sources=[userInput:my-ns, config:config-ns, default:block-node]
chartVersion: strategy=default   value=0.29.0 sources=[default:0.29.0]
storage:      strategy=default   value={/mnt/fast-storage  ...} sources=[default:{/mnt/fast-storage  ...}]
```

---

## Key Invariants and Known Pitfalls

### Empty values must never be registered

The presence-only check in selectors (`if v, ok := sources[st]; ok`) means
that registering an empty string under `StrategyConfig` **shadows** a non-empty
`StrategyDefault` entry.  The result: defaults are silently ignored whenever a
field is absent from `config.yaml`.

**Rule:** always use `setOrClearString` (not `SetSource` directly) for string
fields in `With*` methods.  Use `IsEmpty()` before calling `SetSource` for
struct fields like `BlockNodeStorage`.

```go
// Wrong — registers an empty StrategyConfig that shadows StrategyDefault:
_ = b.namespace.SetSource(StrategyConfig, cfg.BlockNode.Namespace)  // "" shadows default

// Correct — clears the entry when empty, so resolution falls through:
setOrClearString(b.namespace, StrategyConfig, cfg.BlockNode.Namespace)
```

### `storageResolver` strategy reflects the actual contributor

`storageResolver` uses `MergeFrom` across all sources, so multiple strategies
can contribute fields to the final struct.  The reported strategy is the
highest-priority source that contributed a **non-empty** struct.  If only
`StrategyDefault` has values, the strategy is `StrategyDefault` — not
`StrategyConfig`, even if Config was processed earlier.

### `chartVersionResolver` closes over a double pointer

`chartVersionResolver` closes over `**models.Intent` so that intent changes
made by `WithIntent` are visible to the selector without rebuilding it.
`WithIntent` calls `chartVersion.Invalidate()` to force recomputation.

---

## Package Responsibilities

| Package | Role |
|---|---|
| `pkg/models` | Data shapes only — `Config`, `BlockNodeStorage`, `BlockNodeInputs`, etc. |
| `pkg/config` | Parses `config.yaml` (`Get()`), reads env vars (`EnvConfig()`), exposes deps constants (`DefaultsConfig()`) |
| `internal/state` | Persists `state.yaml`; exposes `Reader` / `Writer` / `Persister` interfaces |
| `internal/reality` | Makes live cluster queries; stateless; always fresh |
| `internal/rsl` | Bridges all sources into deterministic `*EffectiveValue[T]` per field |
| `internal/bll` | Calls RSL accessors; owns intent routing and workflow orchestration |

---

## Adding a New Scalar Field

1. **`pkg/models`** — add to `BlockNodeConfig` and `BlockNodeInputs`.
2. **`internal/rsl/block_node_runtime.go`**:
   - Add `*EffectiveValue[string]` field to `BlockNodeRuntimeResolver`.
   - Construct it in `NewBlockNodeRuntimeResolver` with the appropriate selector.
   - Add `setOrClearString(b.<field>, Strategy*, cfg.BlockNode.<Field>)` calls
     to `WithDefaults`, `WithConfig`, `WithEnv`, and `WithUserInputs`.
   - Add `SetSource` / `ClearSource` calls in `setStateSources`.
   - Expose a public accessor method.
3. **`internal/bll/blocknode/helpers.go`** — call the accessor and log it.
4. **Add tests** in `block_node_runtime_test.go` covering:
   - Empty config falls back to default.
   - Non-empty config wins over default.
   - Env wins over config.
   - UserInput wins over env.
   - Deployed state wins over all lower sources.

## Adding a New Struct Field (like `BlockNodeStorage`)

Follow the same steps, but:
- Implement a custom `Selector` using `MergeFrom` for field-level merging.
- Guard registration with `IsEmpty()` in all `With*` methods.
- Track the winning strategy as the highest-priority source with a non-empty struct.
