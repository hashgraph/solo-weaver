# Building a Node-Type Feature — the BLL Handler Pattern

This guide is the map for adding a new deployable component (a node type such as
block node, consensus node, mirror node, …) to solo-provisioner. It explains the
**mental model** — intent, handler, and the business logic layer (BLL) — and the
**stitching**: how a CLI command becomes resolved inputs, an executed workflow,
and persisted state.

For the resolver internals (how a single field is resolved from its sources) see
[effective-value-resolution.md](effective-value-resolution.md). This doc owns the
layer *above* that: routing, orchestration, and feature assembly.

---

## Mental model

Three concepts, in order of increasing concreteness:

### Intent — *what* the operator wants (declarative)

An `Intent` (`pkg/models/intent.go`) is a small value object: an **action** on a
**target**.

```go
type Intent struct {
    Action ActionType // install | upgrade | uninstall | reset | reconfigure | migrate
    Target TargetType // blocknode | consensus | cluster | teleport | ...
}
```

It carries no logic — it is the noun phrase "install a consensus node". Legal
combinations are declared in a compatibility matrix in `intent.go`, so a nonsense
pairing (e.g. `reconfigure teleport`) is rejected before any work starts.
Alongside the intent travels `models.UserInputs[T]` — the raw CLI flag values.
**Intent = what; UserInputs = the parameters for it.**

### Handler — *how* to carry out one action on one target

A handler is the executable counterpart of an intent. The `IntentHandler[I]`
interface (`internal/bll/bll.go`) is a three-method **Template Method** contract:

```
HandleIntent ──► PrepareEffectiveInputs ──► BuildWorkflow ──► (execute + flush)
```

- **`PrepareEffectiveInputs`** — resolve every field from its sources (the
  `EffectiveValue` layers) and apply field-level validation. Answers: *given
  flags + state + reality, what values do we actually use?*
- **`BuildWorkflow`** — check action-level preconditions ("must be deployed
  before upgrade") and assemble the ordered automa steps. Answers: *is this
  action legal right now, and what steps execute it?*
- **`HandleIntent`** — the end-to-end entry point; delegates the fixed sequence
  to the shared `BaseHandler`.

There is **one handler per action** (`InstallHandler`, `UpgradeHandler`, …), not
one per target. Differences between install and upgrade (preconditions, which
state to persist) live in separate handler types, so each stays small.

### BLL — the layer that owns handlers, intents, and resolution

The **business logic layer** (`internal/bll/...`) sits between the CLI commands
(`cmd/cli`) and the workflow/step layer (`internal/workflows`). It owns three
responsibilities, and the pattern maps one-to-one onto them:

| BLL responsibility          | Mechanism                                        |
|-----------------------------|--------------------------------------------------|
| Intent routing              | `HandlerFactory.ForAction(action)` → a handler   |
| Effective-value resolution  | `PrepareEffectiveInputs` + the `rsl` resolvers   |
| Workflow orchestration      | `BuildWorkflow` + `BaseHandler.HandleIntent`     |

---

## How it is stitched (consensus node, end to end)

```
CLI command  (cmd/cli/commands/consensus/node/install.go)
  │  builds Intent{install, consensus} + UserInputs from cobra flags
  ▼
HandlerFactory.ForAction(ActionInstall)          ← routing: intent → handler
  │  returns *InstallHandler
  ▼
InstallHandler.HandleIntent(ctx, intent, inputs)
  │  delegates to BaseHandler.HandleIntent(..., self, patchCallback)
  ▼
BaseHandler runs the fixed sequence (internal/bll/base_handler.go):
  1. ValidateIntent          ← target match + inputs.Validate()
  2. Runtime.Refresh(force)  ← every runtime refreshes; populates Reality + State
  3. PrepareEffectiveInputs  ← [handler] resolve via EffectiveValue, validate
  4. BuildWorkflow           ← [handler] preconditions + ordered steps
  5. wf.Execute              ← run the automa workflow (steps do the real work)
  6. FlushState(callback)    ← [handler] persist what THIS action decided
```

The **invariant sequence** lives once in `BaseHandler`; the concrete handler
supplies only the three variable pieces — `PrepareEffectiveInputs`,
`BuildWorkflow`, and the state-patch callback. That is what keeps every action of
every node type consistent: adding "consensus upgrade" does not mean re-inventing
orchestration — you fill in three holes and register the handler.

### The runtime resolver and reality checker

`PrepareEffectiveInputs` does not read the cluster itself. By the time it runs,
step 2 (`Runtime.Refresh`) has already driven each node type's
`RuntimeResolver` → `RefreshState` → **reality checker**, which queries live
cluster resources and merges them over persisted state. The handler then just
seeds the remaining source layers (user input, deployment package, persisted
state) and pulls the resolved winner per field. See
[effective-value-resolution.md](effective-value-resolution.md) for the
precedence walk (Reality > State > UserInput > Env > Config > Default > Zero).

### The state-patch callback (why install ≠ upgrade)

`BaseHandler.FlushState` refreshes state a final time, then calls the handler's
callback to write back **exactly the fields this action is authoritative for**.
This is where install and upgrade diverge:

- Install persists the full record (image, ledger, config hashes, secrets).
- Upgrade persists only what it changed and must **not** clobber fields it never
  resolved (a zero value would silently erase a prior decision).

Pass `nil` as the callback when an action persists nothing beyond the base flush.

---

## When to use `EffectiveValue` (and when not to)

`EffectiveValue[T]` is not free. Each field costs a resolver struct field, a
constructor block, an accessor, and a source-seeding call in every `With*`
method — plus the gob-encoding, caching, and selector machinery underneath. That
cost is worth paying for a field that genuinely arbitrates between competing
sources. It is pure ceremony for a field that merely falls back.

**This is a judgement call every author must make — do not apply the full
machinery uniformly.** A reference implementation that wraps everything in
`EffectiveValue` teaches the next author to over-apply it. Model proportionality
instead.

Use `EffectiveValue[T]` when **both** hold:

1. The field has **≥2 sources that can genuinely disagree** (e.g. a deployed
   value from Reality vs. a CLI flag vs. a package default), **and**
2. You need **precedence-aware arbitration** (Reality/State must win) **or**
   **per-field validation** (a custom `Selector` that errors on an invalid
   deployed value, or is intent-aware for upgrade vs. install).

Use a **plain coalesce helper** — `firstNonEmpty(userInput, pkg, embedded)` or
equivalent — when the field is:

- **Bulk content** rendered into a CR or file (config-file bodies, templates)
  that only ever coalesces package-over-default, or
- A simple **2-source fallback** with no reality/state layer and no validation.

### Worked example — the consensus node split

The consensus resolver deliberately splits its fields:

| Fields | Mechanism | Why |
|--------|-----------|-----|
| `imageRepo`, `imageTag`, `ledgerId`, `chainId` | `EffectiveValue[string]` | Real contention: deployed **Reality** vs. `--image-tag` **UserInput** vs. deployment-package **Config**; Reality must win for idempotency. |
| The 11 config-file contents (`log4j2`, `throttles`, …) | plain coalesce helper | Only ever userInput → package → embedded default. No reality/state layer, no validation — the machinery would add ~150 lines of accessors and constructors for a three-line fallback. |

If a field starts as a coalesce and later grows a reality/state layer or a
validation rule, promote it to `EffectiveValue` then — not speculatively now.
The concrete trigger for the config-file contents is a **`reconfigure`** command
(consensus has no CLI upgrade — version changes ride a separate protocol): at
reconfigure, config contents gain a **Reality** layer (the deployed config-CR
content) and a **State** layer (the last persisted content), so that changing one
file preserves the rest as deployed instead of reverting to defaults. That
four-source, precedence-sensitive arbitration is exactly what earns
`EffectiveValue`. Promotion is cheap; carrying unused machinery across every
future node type is not.

### The reconfigure resolution model (for when it is built)

Per config file, three values are in play: **R** = deployed (reality), **S** =
what we last set (state, via the persisted per-file hash), **C** = what the
operator supplies this run. The correct, robust policy:

| Case | Situation | Action |
|------|-----------|--------|
| A | operator does not supply the file | keep deployed (tolerate any drift on untouched files) |
| B | supplies it, `C == R` | no-op |
| C | supplies it, `C != R`, **`R == S`** (no drift) | **apply** — the command is the intent |
| D | supplies it, `C != R`, `R != S` (out-of-band drift) | **refuse unless `--force`** |

Why this over the simpler "reality always wins unless `--force`": that simpler
rule requires `--force` for **every** intended change (Case C), which trains
operators to pass `--force` reflexively — so in Case D, the one place `--force`
must make someone stop and think, it provides no friction. **A confirmation that
is always required is not a confirmation.** This model reserves `--force` for
genuine drift (Case D), keeping it meaningful.

It also **fails closed**: the only no-`--force` write is Case C, gated on
`R == S` — deployed content exactly matching our own record (compared by hash),
so there is no third-party edit to lose. If State is stale or missing, `R != S`
falsely and the model *refuses* rather than clobbers. It therefore never silently
destroys an out-of-band edit, even when State is unreliable. The drift signal
needs only the persisted per-file hash (`state.ConfigHashEntry`) — no stored
content — plus a read of the deployed CR (already done by the apply step). It
requires a **custom selector** because Config (the supplied file) must outrank
Reality for Case C, inverting the default `Reality > Config` order.

---

## Checklist — adding a new node type

Use the consensus node as the worked reference. To add a *field* to an existing
resolver instead, stop here and follow
[effective-value-resolution.md § Adding a New Scalar Field](effective-value-resolution.md).

### 1. Models (`pkg/models`)
- Add `Target<Name>` and legal actions to the compatibility matrix in `intent.go`.
- Add `<Name>Inputs` with a `Validate()` method (required-field checks that do
  **not** depend on resolution — resolution-dependent checks belong in the
  handler, after `PrepareEffectiveInputs`).
- If any inputs are sensitive, implement `Redacted()` (see `Redactable[T]`).
- Add any shared naming helpers here (e.g. `ConsensusNodeScope`,
  `ConsensusCapsuleName`) so the CLI, handler, steps, and reality checker all
  compute names from one source — never inline the `fmt.Sprintf`.

### 2. State (`internal/state`)
- Add `<Name>State` (and any sub-structs) to `state.State`, with `yaml`/`json`
  tags. Update the deep-copy logic if the state holds maps/slices.

### 3. Reality checker (`internal/reality`)
- Implement `Checker[<StateType>]` that reads live cluster resources and merges
  them over persisted state. Return persisted state unchanged when the cluster
  is unreachable — reality *refines* state, it does not replace it.
- Add the field to `reality.Checkers` and construct it in the checker factory.

### 4. Runtime resolver (`internal/rsl`)
- Add a `<Name>RuntimeResolver` implementing `Resolver[<StateType>, <Inputs>]`.
- **Decide per field: `EffectiveValue` or plain coalesce.** Apply the rule in
  [§ When to use `EffectiveValue`](#when-to-use-effectivevalue-and-when-not-to)
  above — arbitrating scalars get `EffectiveValue`, bulk/fallback content gets a
  coalesce helper. Do not pad the layer count to match another node type.
- For each `EffectiveValue` field: seed `StrategyDefault` at construction; seed
  the other layers via `With*` / `Set*` methods.
- Capture persisted state separately from the reality-refreshed map so the
  **State and Reality layers stay distinct** (see the consensus resolver's
  `persistedNodes` vs `nodes`). Return defensive copies from state accessors.
- Register the resolver in `rsl.RuntimeResolver` and its `Refresh` /
  `CurrentState` fan-out (`internal/rsl/runtime.go`).

### 5. Handler + factory (`internal/bll/<name>`)
- `handler.go` — a `Handlers` struct, `NewHandlerFactory(runtime)` that
  type-asserts the concrete resolver, and `ForAction(action)` routing.
- One handler file per action. Each embeds `bll.BaseHandler[<Inputs>]`,
  implements `PrepareEffectiveInputs` and `BuildWorkflow`, and defines
  `HandleIntent` as a one-line delegation to `BaseHandler.HandleIntent(...)`
  with its state-patch callback.

### 6. Steps (`internal/workflows/steps`)
- Implement the automa steps the workflow needs (create CRs, precheck operator,
  etc.). Read cluster state through the injected `kube` client — do **not** exec
  external binaries (see [security-model.md](security-model.md)).
- Attach a reason code and operator hints to every `StepFailureReport` (see
  [error-handling.md](error-handling.md)).

### 7. CLI command (`cmd/cli/commands/<name>`)
- A cobra command group with per-action subcommands. Register flags, build the
  `Intent` and `UserInputs`, call `common.Setup()` to get the `RuntimeResolver`,
  then `factory.ForAction(...)` → `HandleIntent`, wrapped in
  `common.RunWorkflow`.
- Update the command docs under `docs/commands/` (see the CLI-surface rule in
  `CLAUDE.md`).

### 8. Tests
- Resolver: per-field precedence (default < config < env < userInput < state <
  reality).
- Handler: `ForAction` routing, `PrepareEffectiveInputs` validation, and the
  `BaseHandler` sequence (validate → refresh → prepare → build → execute →
  flush). Mirror the `TC-BLL-*` cases in
  [functionality-test-suite.md](functionality-test-suite.md).

---

## One-line summary

- **Intent** is the declarative request (`install` × `consensus`).
- **Handler** is the executable procedure for exactly that request, expressed as
  three fill-in-the-blank steps.
- **BLL** is the machinery that routes an intent to its handler and runs it
  through the shared `BaseHandler` skeleton, with `EffectiveValue` resolution as
  the "prepare inputs" step in the middle.

The intent is the question; the handler is the answer; the BLL guarantees every
answer is produced the same disciplined way.

## Related docs

- [effective-value-resolution.md](effective-value-resolution.md) — the resolver
  layer: `EffectiveValue`, selectors, precedence, adding a field.
- [error-handling.md](error-handling.md) — reason codes and hints on failures.
- [functionality-test-suite.md](functionality-test-suite.md) — the BLL test
  checklist.
- [security-model.md](security-model.md) — why steps read the API, not binaries.
- [migration-framework.md](migration-framework.md) — startup/upgrade migrations
  that run around these handlers.
