# Feature Gates

This document describes how to add a feature gate in Solo Provisioner — a
version-keyed, in-binary switch that lets you land a behaviour or contract change
behind an off-by-default gate instead of forcing every change to be
backward-compatible.

Feature gates are built on [`github.com/automa-saga/gatekit`](https://github.com/automa-saga/gatekit)
(a zero-dependency library). Solo Provisioner's thin adapter lives in
`internal/featuregate`, and operators inspect the catalog with the
`solo-provisioner feature` command.

> This is the Layer 1 (gate + maturity) surface. The activation version is a
> compile-time constant today; advancing it at runtime via `finalize`, migration
> gating, and the run invariant are Layer 2 — see epic
> [#1275](https://github.com/hashgraph/solo-weaver/issues/1275). CLI maturity
> labelling / hiding in `--help` is [#1392](https://github.com/hashgraph/solo-weaver/issues/1392).

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

## The two axes

Every spec is placed on two axes:

- **Activation version** — a monotonic integer (the axis). A spec's `ActiveFrom`
  is the activation version at which it takes effect. `CurrentActivationVersion`
  (in `features.go`) is the in-force value — today's GA floor is `1`; a new
  behaviour schedules at the next integer. A feature is on when the last spec
  whose `ActiveFrom <= CurrentActivationVersion` resolves on.
- **Maturity** — `ALPHA` / `BETA` / `GA` / `DEPRECATED`. `ALPHA` is off unless the
  operator opts in **and** the spec is ready — i.e. `Default: true` (see
  [Enabling a feature](#enabling-a-feature)); an unfinished `Alpha, Default: false`
  feature is **dormant** and cannot be enabled by any opt-in. `BETA` and `GA` follow
  the version axis alone.

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

- **`ActiveFrom`** — the activation version it turns on at. Alpha **dev gates** live
  at the **current** activation version (`1`); only a compatibility change that must
  flip the whole fleet together bumps the axis (see
  [Development gates vs compatibility gates](#development-gates-vs-compatibility-gates)).
- **`Default`** — for an `ALPHA` feature this is the **readiness** switch
  ([Model C](#enabling-a-feature)): `false` = dormant (unfinished — no opt-in can
  enable it, but code may already reference `gate.Enabled(f)`, which returns false);
  `true` = ready to dogfood via `--experimental`/env. For `BETA`/`GA` it is whether
  the feature is on at/after `ActiveFrom`.
- **`Maturity`** — start at `Alpha` for anything unvalidated (forces opt-in);
  `Beta`/`GA` are default-on once their version is active.
- **`Description`** — a short, human-readable line; it is shown in
  `feature list` and must not repeat the maturity or version.

### 3. Branch the code with `gate.Enabled`

Business code calls `gate.Enabled(Feature)` — **never** compare versions inline.
Obtain a gate with `featuregate.NewDefault()`. (To gate a whole command or a flag
by maturity instead of a code path, see
[Gating commands and flags](#gating-commands-and-flags).)

```go
gate, err := featuregate.NewDefault()
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
| `BETA`  | visible, labelled `[beta]` | only when opted in |
| `GA`    | visible | always |

An opted-in operator runs the command with the hidden, command-scoped
`--experimental` flag, or the feature's `SOLO_PROVISIONER_ENABLE_<FEATURE>` opt-in. A
hidden alpha command stays **discoverable** via `solo-provisioner feature list`, which
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

**Model C — the opt-ins respect readiness.** An operator opt-in may enable an alpha
feature **only when its spec is ready** (`Default: true`). An unfinished
`Alpha, Default: false` feature is **dormant**: neither `--experimental` nor the env
var can switch it on — only changing its spec (flip to `Default: true`, or graduate)
can. This lets code merge incrementally (`gate.Enabled(f)` compiles and returns false)
without any risk of a half-done path being turned on. Because the command gate and
business code share one gate (see below), opting a *ready* command in turns the
behaviour on for its business logic too.

Two opt-in surfaces, both readiness-respecting:

- **`--experimental` flag** (command-scoped) — on a gated command, turns *that
  command's* ready feature on for the invocation. Preferred under `sudo`, which strips
  env vars. It is *not* a gate-wide "all alpha" switch, and it cannot wake a dormant
  feature.
- **`SOLO_PROVISIONER_ENABLE_<FEATURE>` env var** — the feature name in
  `UPPER_SNAKE_CASE` (e.g. `SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE=1`). The
  per-feature surface for scripting; the exact variable is printed by `feature list`.
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

## Inspecting the catalog

```bash
solo-provisioner feature list
```

Shows each feature's maturity, the activation version it is `ACTIVE FROM`,
whether it is currently enabled, a description, and — for `ALPHA` features — the
exact opt-in env var. Filters: `--activation-version`, `--maturity`, `--enabled`;
`--output json` for machine consumption. The command is read-only and runs
unprivileged.

## Graduating and removing a feature

- **Graduate** — add a later spec at a higher `ActiveFrom` with a more mature
  stage (e.g. `ALPHA` → `GA`). The old path stays as the `else` branch.
- **Remove (contract step)** — at a later activation version, delete the `else`
  branch and the old code, and mark any superseded model `DEPRECATED`. The code
  carries at most two eras (N and N+1) at once; deleting the old side each cycle
  is the whole point of the window.

## Development gates vs compatibility gates

Both are the same mechanism; they differ by one question — **does turning the
feature on require the whole fleet to flip together?**

- **Development gate** (the common case — our replacement for long-lived
  branches). One `Alpha` spec at the **current** activation version. It never raises
  the max activation version, so it couples to nobody else's work. While the code is
  unfinished, keep it `Default: false` — **dormant**, so no opt-in can switch the
  half-done path on, yet `gate.Enabled(f)` already compiles and returns false. When
  the path is ready to dogfood, flip it to `Default: true` (now `--experimental` /
  env enable it); when it is ready for everyone, graduate it (`Beta`/`GA`) **in
  place** (or just delete the gate). **No activation bump, no migration, no
  finalize.**
- **Compatibility gate** (a breaking change where old and new must coexist during
  a rolling upgrade — a shared wire or storage format). A **new** spec at the next
  activation version, with — in Layer 2 — a migration and a coordinated `finalize`.

So the activation version is a coordination counter, **not** a milestone: bump it
only when turning the behavior on can't safely coexist with off across a rolling
upgrade. Full walkthrough: gatekit-spec
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
