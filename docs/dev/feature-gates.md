# Feature Gates

This document describes how to add a feature gate in Solo Provisioner — a
version-keyed, in-binary switch that lets you land a behaviour or contract change
behind an off-by-default gate instead of forcing every change to be
backward-compatible.

Feature gates are built on [`github.com/automa-saga/gatekit`](https://github.com/automa-saga/gatekit)
(a zero-dependency library). Solo Provisioner's thin adapter lives in
`internal/featuregate`, and operators inspect the catalog with the
`solo-provisioner feature-gate` command.

## What feature gates give you

- **Rapid, safe merges.** Land a change *dormant* — off and unreachable by any opt-in
  — and finish it incrementally on `main` instead of a long-lived feature branch.
  `gate.Enabled(f)` compiles and returns `false` the whole time.
- **Enable / disable without a release.** Flip a *ready* feature on or off per
  invocation with `--experimental` or its env var: dogfood it before rollout, or use it
  as a kill switch after.
- **Coordinated activation + migrations.** When a change must flip the whole fleet
  together, advance the activation version and run the migration that crosses the
  boundary. This is the migration layer — `finalize` plus gated migrations on the same
  registry — being built next in epic
  [#1275](https://github.com/hashgraph/solo-weaver/issues/1275); see
  [Compatibility gates](#compatibility-gates-in-depth).

## What a feature gate is (and is not)

- A gate toggles an **N vs N+1 code path**. Reach for one when a change is *not*
  cheap to keep backward-compatible (a risky refactor, a contract or schema
  change), or when a capability is merged but not yet ready for end users.
- A feature names a **gated behaviour**, not a command and not a stable
  capability. Most commands need no gate. When the change ships both paths in the
  binary and picks one at runtime, that is a gate:

  ```go
  if gate.Enabled(featuregate.BlockNodeOperatorManaged) {
      installViaCapsule()   // N+1
  } else {
      installViaHelm()      // N — today's default, the fallback
  }
  ```

  The *old* path is the `else` branch, never a feature of its own.

## The mental model

A feature gate sits on **one axis** and carries **two independent labels**. Keep
these three ideas separate and everything else follows.

**The axis — activation version.** A monotonic integer. A spec's `ActiveFrom` is the
version it takes effect at; `CurrentActivationVersion` (in `features.go`) is the
in-force value — today's floor is `1`. A feature resolves on when the last spec with
`ActiveFrom <= CurrentActivationVersion` is on.

> **The activation version is _not_ a per-feature counter, and it may stay at `1`
> forever.** It is a **coordination counter**, not a release or roadmap number. You do
> **not** bump it to add a feature: development gates (the common case) all live at the
> current version, so any number of them can land without moving the axis. It advances
> **only** for a compatibility gate — a change that needs a coordinated fleet flip
> during a rolling upgrade. If solo never needs such a flip, the axis simply stays at
> `1` indefinitely.

```text
The activation version — a coordination counter that normally never moves:

  v1  ●──────────────────────────────────────────────────▶   stays at 1
      │
      ├── KubeClusterUpgrade        Alpha   ┐  every gate lives at the current
      ├── ConsensusNodeOperator…    Alpha   │  version; adding one never moves
      └── …                                 ┘  the axis

Only a compatibility gate (a coordinated rolling-upgrade flip) ever bumps it:

  v1  ●───────────────┐
                      │  finalize (Layer 2): every node flips together
                      ▼
  v2  ●──────────────────────────────────────────────────▶
      └── the old v1 spec and the new v2 spec coexist during the upgrade,
          then the v1 spec is contracted away
```

**Label 1 — Maturity: how ready for _users_.** `ALPHA` / `BETA` / `GA` /
`DEPRECATED`. The user-facing stability label: `ALPHA` is hidden and requires an
opt-in; `BETA` is visible, labelled `[beta]`, and runs; `GA` is generally available
(on via the version axis); `DEPRECATED` is on its way out.

**Label 2 — Gate kind: how it _rolls out_ across the fleet.** Decided by one
question — *does turning this on require the whole fleet to flip together?*

- **Development gate** (the common case) — **No.** Lives at the **current** activation
  version and never bumps it, so it couples to nobody else's work. Our replacement for
  a long-lived branch: merge incrementally, off by default, flip on when ready. No
  activation bump, no migration, no finalize.
- **Compatibility gate** — **Yes**, because old and new cannot coexist during a
  rolling upgrade (a shared wire or storage format). Add a **new** spec at **current +
  1** and bump the activation version later, when the fleet is ready to flip together
  (via `finalize` — Layer 2).

> Maturity and gate kind are **orthogonal**. A development gate still has a maturity
> (usually `Alpha` → `GA`); a compatibility gate does too. "Development vs
> compatibility" is about the *activation-version axis*; "Alpha vs GA" is about *user
> readiness*.

**The readiness switch — `Default` on an `ALPHA` spec.** For an `ALPHA`, `Default`
says whether it is wired up enough to opt into:

| `Default` | State | Who can turn it on |
|---|---|---|
| `false` | **dormant** (unfinished) | nobody — a safe home for incremental code |
| `true`  | **ready** (dogfood) | `--experimental` or the env var |

An opt-in enables an alpha **only when it is ready**. A dormant `Alpha, Default:
false` feature is unreachable by any opt-in, yet `gate.Enabled(f)` still compiles and
returns `false` — so code merges incrementally with no risk of exposing a half-done
path. (For `BETA`/`GA`, `Default` is simply whether the feature is on at/after its
`ActiveFrom`.) This adopts gatekit's native `WithExperimental` semantics (which honour
`Default`) while keeping the opt-in **command-scoped** and sharing **one gate** between
a command and its business code.

## Deciding how to define a feature

Walk the tree, then read the spec straight off the matrix.

```text
I want to gate a behaviour.
│
├─ Q1. Does turning it on require the whole fleet to flip together?
│      (shared wire/storage format; old and new can't coexist mid rolling-upgrade)
│      │
│      ├─ NO  → Development gate:   ActiveFrom = current (1)
│      └─ YES → Compatibility gate: ActiveFrom = current + 1,
│                                   bump the activation version later (finalize, Layer 2)
│
├─ Q2. How ready is it for users?
│      ├─ Unfinished / unvalidated → Maturity: Alpha  → go to Q3
│      ├─ Usable, want feedback    → Maturity: Beta   (Default: true)
│      └─ Ready for everyone       → Maturity: GA     (Default: true)
│
└─ Q3. (Alpha only) Is it wired up enough to dogfood?
       ├─ NO  → Default: false  (dormant — nobody can enable it)
       └─ YES → Default: true   (ready — --experimental / env enable it)
```

| I want to… | `ActiveFrom` | `Maturity` | `Default` | Who can turn it on |
|---|---|---|---|---|
| Merge unfinished code, reachable by no one | current (`1`) | `Alpha` | `false` | nobody |
| Let testers dogfood it | current (`1`) | `Alpha` | `true` | `--experimental` / env |
| Offer a visible beta for feedback | current (`1`) | `Beta` | `true` | everyone (labelled `[beta]`) |
| Ship to everyone (no fleet coordination) | current (`1`) | `GA` | `true` | everyone (on) |
| Make a breaking change safe across a rolling upgrade | current + 1 | `Alpha`→`GA` | per stage | after the activation-version bump (finalize, Layer 2) |
| Retire a superseded path | later version | `Deprecated` | — | contract step — delete the `else` branch |

## Feature lifecycle — define and promote

A feature gate is **transitional scaffolding**, not a permanent switch. Its maturity
climbs until the feature is stable — and then the gate is **removed**: the code path
becomes unconditional and the gate row disappears. (Kubernetes works the same way — it
deletes a feature gate a few releases after it reaches GA.)

```text
  Alpha   →   Beta   →   GA    →   (gate removed)
  off,        more        on,       code path becomes unconditional;
  opt-in      baked       locked    the gate row + the old `else` branch deleted
```

So **GA is a hand-off waypoint, not a home.** A GA gate is on by default and briefly
locked on — a rollback tripwire during the transition — and once the new path is proven
and the old `else` branch is deleted, the gate is removed. The catalog should never
accumulate permanent always-on GA rows: a stable capability that no longer toggles
anything has **no gate at all** (it's just how the product works, discoverable via
`--help`).

A development gate's whole life happens at one activation version — only the
`Maturity` and `Default` change (Beta is an optional step):

```text
How one feature moves through its life — the activation version stays at 1:

  Dormant     {ActiveFrom:1, Alpha, Default:false}
    │         off; no opt-in can enable it — gate.Enabled(f) == false
    │         (merge the code incrementally, safely)
    │  flip Default → true
    ▼
  Ready       {ActiveFrom:1, Alpha, Default:true}
    │         off by default; --experimental / env turn it on (dogfood)
    │  graduate Maturity: Alpha → GA   (same ActiveFrom)
    ▼
  GA          {ActiveFrom:1, GA, Default:true}
    │         on for everyone
    │  contract: delete the old `else` path; mark any superseded model Deprecated
    ▼
  Removed     the spec and the old code are deleted — the gate's job is done
```

The same path, flip by flip:

| Stage | Spec in `registry.go` | Who can turn it on | How you get here |
|---|---|---|---|
| **Dormant** (unfinished) | `{ActiveFrom: 1, Default: false, Maturity: Alpha}` | nobody | land the feature + its code; `gate.Enabled(f)` returns `false` |
| **Ready** (dogfood) | `{ActiveFrom: 1, Default: true, Maturity: Alpha}` | `--experimental` / env | flip `Default: false → true` |
| **GA** (on for all) | `{ActiveFrom: 1, Default: true, Maturity: GA}` | everyone | graduate `Alpha → GA` in place |
| **Removed** | delete the spec + the `else` branch | — | contract step; mark any superseded model `Deprecated` |

## How to add a feature

> **Rule:** add a feature in the **same PR that lands the code it gates**, so the
> catalog never advertises a gate with no behaviour behind it. Features whose
> code is not yet in `main` are kept as commented-out placeholders in
> `features.go` and `registry.go`, uncommented by their implementing PR.

### 1. Add the `Feature` constant — `internal/featuregate/features.go`

```go
// BlockNodeOperatorManaged gates the solo-operator (HelmCapsule) deployment
// model for block nodes against the provisioner-managed one.
BlockNodeOperatorManaged gatekit.Feature = "BlockNodeOperatorManaged"
```

Name the behaviour, namespaced by its command area (`BlockNode*`, `ConsensusNode*`,
`Network*`), in CamelCase.

### 2. Add the registry entry — `internal/featuregate/registry.go`

```go
BlockNodeOperatorManaged: {
    Description: "Manage block node lifecycle through solo-operator.",
    Specs: gatekit.VersionedSpecs{
        {ActiveFrom: 1, Default: false, Maturity: gatekit.Alpha},
    },
},
```

Pick the three fields from [Deciding how to define a feature](#deciding-how-to-define-a-feature):

- **`ActiveFrom`** — the activation version it turns on at. A development gate uses the
  **current** version (`1`); a compatibility gate uses **current + 1**.
- **`Default`** — for an `ALPHA` spec this is the **readiness** switch: `false` =
  dormant (no opt-in can enable it, but `gate.Enabled(f)` still compiles and returns
  false); `true` = ready to dogfood via `--experimental`/env. For `BETA`/`GA` it is
  whether the feature is on at/after `ActiveFrom`.
- **`Maturity`** — start at `Alpha` for anything unvalidated (forces opt-in);
  `Beta`/`GA` are default-on once their version is active.
- **`Description`** — a short, human-readable line; it is shown in
  `feature-gate list` and must not repeat the maturity or version.

### 3. Branch the code with `gate.Enabled`

Business code calls `gate.Enabled(Feature)` — **never** compare versions inline.
Obtain the invocation's gate with `featuregate.Gate(cmd.Context())`, so it honours the
same `--experimental` / env opt-in the command resolved (see
[Enabling a feature](#enabling-a-feature)) — not its own `NewDefault()`. (To gate a
whole command or a flag by maturity instead of a code path, see
[Gating commands and flags](#gating-commands-and-flags).)

```go
gate, err := featuregate.Gate(cmd.Context())
if err != nil {
    return err
}
if gate.Enabled(featuregate.BlockNodeOperatorManaged) {
    // new path
}
```

## Gating commands and flags

`gate.Enabled` above gates a *code path*. To gate a whole **command** or a single
**flag** by its feature's maturity — hiding or labelling it in `--help` and
refusing it unless the operator opts in — use the entry-level gate in
`cmd/cli/commands/common`.

### Gate a command — `common.GateFeature(cmd, feature)`

```go
// cmd/cli/commands/consensus/consensus.go
common.GateFeature(consensusCmd, featuregate.ConsensusNodeOperatorManaged)
```

Visibility and availability follow the feature's maturity:

| Maturity | `--help` | Runs? |
|---|---|---|
| `ALPHA` | **hidden** | only when opted in |
| `BETA`  | visible, labelled `[beta]` | always |
| `GA`    | visible | always |

An opted-in operator runs the command with the hidden, command-scoped
`--experimental` flag, or the feature's `SOLO_PROVISIONER_ENABLE_<FEATURE>` opt-in. A
hidden alpha command stays **discoverable** via `solo-provisioner feature-gate list`, which
prints the feature and its exact opt-in env var.

Command availability is *maturity-based*: a `GA`/`BETA` command always runs; an
`ALPHA` command runs only when opted in **and ready** (`Default: true`). An
unfinished `Alpha, Default: false` command is **dormant** — `--experimental` and the
env var cannot enable it, and the refusal says so. (The activation-version axis
governs behaviour rollout, not command availability.)

### Gate a flag — `common.GateFeatureFlag(cmd, flagName, feature)`

When a stable (GA) command gains an unproven flag, gate just the flag, not the
whole command:

```go
cmd.Flags().String("firewall-config-file", "", "declarative firewall input")
common.GateFeatureFlag(cmd, "firewall-config-file", featuregate.SomeAlphaFeature)
```

An `ALPHA` flag is hidden from the command's `--help` and errors if the operator
sets it without opting in; a `BETA` flag is labelled `[beta]`. The flag must
already be registered on the command.

### Enable a code-path gate on a GA command — `common.GateBehavior(cmd, features...)`

`GateFeature`/`GateFeatureFlag` gate a command or flag by *maturity*. But a **GA**
command (always available) may still read a *code-path* gate for an unproven feature —
`if gate.Enabled(f)`. To give operators a **sudo-safe** way to turn that on, declare the
features the command reads:

```go
// cmd/cli/commands/block/node/install.go — a GA command that reads an alpha code path
common.GateBehavior(installCmd, featuregate.BlockNodeOperatorManaged)
```

The command stays visible and always runs; passing `--experimental` turns the declared
*ready* features on for that invocation (scoped to them), shared with business code via
`featuregate.Gate(ctx)`. Because `--experimental` is a **flag**, it survives `sudo` —
unlike the `SOLO_PROVISIONER_ENABLE_<FEATURE>` env var, which `sudo` strips. Declaring a
feature here also makes it a reader, so the orphan-feature lint is satisfied.

### The `--help` surface golden test

`TestRootHelpSurface_Golden` (`cmd/cli/commands/help_surface_golden_test.go`) pins
the **visible** command/flag surface — every non-hidden command (top level and one
level down) plus the root persistent flags — to
`cmd/cli/commands/testdata/help-surface.golden`.

If a change exposes a new command or flag (for example, forgetting to gate an
alpha feature), the surface changes and the test fails with a reviewable diff, so
accidental exposure cannot merge silently. When the change is intended — a feature
graduated, or a new GA command landed — regenerate the golden:

```bash
go test ./cmd/cli/commands -run TestRootHelpSurface_Golden -update-golden
```

## Enabling a feature

An opt-in enables an alpha only when it is ready (`Default: true`) — see
[the readiness switch](#the-mental-model). Because the command gate and business code
share one gate (below), opting a *ready* command in turns the behaviour on for its
business logic too.

Two opt-in surfaces, both readiness-respecting:

- **`--experimental` flag** (command-scoped) — on a gated command, turns *that
  command's* ready feature on for the invocation. Preferred under `sudo`, which strips
  env vars. It is *not* a gate-wide "all alpha" switch, and it cannot wake a dormant
  feature.
- **`SOLO_PROVISIONER_ENABLE_<FEATURE>` env var** — the feature name in
  `UPPER_SNAKE_CASE` (e.g. `SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE=1`). The
  per-feature surface for scripting; the exact variable is printed by `feature-gate list`.
  Note `sudo` drops it unless you use `sudo -E`.

Under the hood each is resolved by gatekit (`resolve`): the opt-in asks whether the
feature is ready (gatekit's `WithExperimental` resolved for that feature — `Alpha`
returns its `Default`), and only a ready feature is forced on via a scoped override.
A raw gatekit override still wins unconditionally, but solo exposes that only as the
internal `Options.Overrides` seam (tests/programmatic forcing), never to operators.

**One gate per invocation.** The command gate resolves these opt-ins once, builds a
`*gatekit.FeatureGate`, and stores it on the command context. Business code reads the
same gate with `featuregate.Gate(ctx)` — never its own `NewDefault()` — so command
availability and in-code behaviour are a single decision.

> **Code-path gates in GA commands under `sudo`.** A GA command doesn't get
> `--experimental` from `GateFeature` (it isn't maturity-gated), so to turn on a code-path
> gate it reads, declare the feature with
> [`common.GateBehavior`](#enable-a-code-path-gate-on-a-ga-command--commongatebehaviorcmd-features)
> — that installs the sudo-safe `--experimental` flag scoped to it. Without that, the only
> opt-in is the env var, which `sudo` strips (use `sudo env VAR=1` or `sudo -E`). A
> per-feature `--enable-feature NAME` flag for finer control is tracked in
> [#1487](https://github.com/hashgraph/solo-weaver/issues/1487).

## Inspecting the catalog

```bash
solo-provisioner feature-gate list
```

Shows each feature's maturity, the activation version it is `ACTIVE FROM`,
whether it is currently enabled, a description, and — for `ALPHA` features — the
exact opt-in env var. Filters: `--activation-version`, `--maturity`, `--enabled`;
`--output json` for machine consumption. The command is read-only and runs
unprivileged.

## Graduating and removing a feature

- **Graduate a development gate** — bump the maturity **in place**, at the same
  `ActiveFrom` (e.g. `ALPHA → GA`, keeping `Default: true`). No new spec, no version
  bump. The old code path stays as the `else` branch until you remove it.
- **Graduate a compatibility gate** — see
  [Compatibility gates in depth](#compatibility-gates-in-depth): the new era is a spec
  at `current + 1`, activated by `finalize`.
- **Remove (contract step)** — once GA is stable, delete the `else` branch, the old
  code, **and the gate row itself**, and mark any superseded model `DEPRECATED`. This
  is not optional cleanup: a gate exists only while two code paths coexist, so a stable
  GA feature should end with its gate removed, never left as a permanent always-on row.
  The code carries at most two eras at once; deleting the old side each cycle is the
  whole point.

## Compatibility gates in depth

A **development gate** needs no more than the [lifecycle table](#feature-lifecycle--define-and-promote):
flip `Default`, graduate maturity, done. A **compatibility gate** is the rarer case —
use it only when turning the behaviour on can't safely coexist with off across a
rolling upgrade (a shared wire or storage format). It adds coordination:

1. Add a **new** spec at **`CurrentActivationVersion + 1`** (leave the current spec in
   place — both eras coexist while the fleet upgrades).
2. Attach a migration and a coordinated `finalize` that advances the activation
   version only once every node is ready (Layer 2).
3. At a later version, contract: delete the old spec and its `else` branch.

A compatibility gate is the **only** thing that bumps the activation version — which is
why the axis moves rarely, and may never move at all if solo never needs a coordinated
rolling-upgrade flip. Full walkthrough: gatekit-spec
[Lesson 7](https://automa-saga.github.io/gatekit-spec/#/tutorial/7-gating-a-feature-when-plans-change).

## Keeping the registry honest: the lints

The catalog is guarded by `go test ./internal/featuregate`, so ordinary CI catches
a drifted registry. `SupportWindow` (in `features.go`) is the single source of
truth for the window `W`; the gate and every lint use it.

- **Removal lint** (`TestRegistryRemovalLint`) — **fails** when a spec has gone
  stale, a feature is past its `RemoveBy`, or a feature is `Deprecated` and due for
  deletion. Delete the named spec and the dead path it guarded.
- **Baseline lint** (`TestRegistryBaselineLint`) — **fails** when the catalog jumps
  the activation version by more than one, or rewrites a spec at or below the last
  released ceiling, both of which break or split a running fleet. It compares the
  catalog against `testdata/baseline_registry.json`, a committed snapshot of the
  **last released** catalog. Regenerate that snapshot **only at release**, never in
  a feature PR (or it would compare the catalog against itself):

  ```bash
  go test ./internal/featuregate -run TestRegistryBaselineLint -update-baseline
  ```

- **Dev-gate window** (`TestDevGateWindow`) — **warns** (never fails) when an
  `Alpha` gate has drifted below the support floor, i.e. outlived its cycle without
  being graduated or deleted. It is a warning on purpose: a feature can legitimately
  slip when more urgent work ships first.
- **Orphan features** (`TestNoOrphanFeatures`) — **fails** when a catalogued feature
  has no reader in production code (no `gate.Enabled` / `GateFeature` /
  `GateFeatureFlag` reference). This is what keeps the catalog honest when the
  activation version is frozen (so the time-based lints above never fire): an inert
  gate — e.g. a GA feature whose old `else` path was deleted — must be removed, not
  left as a permanent always-on row.

**Migration safety (Layer 2).** When gated migrations land, a feature migration
runs only through gatekit's `RunNow`/`Finalize`, keyed to the activation version it
crosses — **never** from the unconditional startup pre-run. A development gate
carries **no migration**. Startup (`ScopeStartup`) migrations are global
CLI-version migrations and must not depend on a feature gate.

## Related

- Epic [#1275](https://github.com/hashgraph/solo-weaver/issues/1275) — the
  decoupled activation-version feature-gate design (Layer 1 + Layer 2).
- [#1392](https://github.com/hashgraph/solo-weaver/issues/1392) — CLI command/flag
  maturity labelling via the registry.
- `internal/featuregate/` — the solo adapter (catalog, gate, env overrides).
- `github.com/automa-saga/gatekit` — the underlying library.
