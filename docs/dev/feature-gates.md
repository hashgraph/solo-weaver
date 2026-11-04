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
- **Maturity** — `ALPHA` / `BETA` / `GA` / `DEPRECATED`. `ALPHA` is off unless
  the operator opts in (see [Enabling a feature](#enabling-a-feature)); `BETA`
  and `GA` follow the version axis alone.

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
        {ActiveFrom: 2, Default: true, Maturity: gatekit.Alpha},
    },
},
```

- **`ActiveFrom`** — the activation version it turns on at. The GA floor is `1`;
  a new, not-yet-active behaviour is `2`.
- **`Default`** — whether it is on at/after `ActiveFrom` (unless a later spec
  overrides). For an `ALPHA` feature this still only applies once the operator
  opts in.
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

An opted-in operator runs the command with the hidden gate-wide `--experimental`
flag, or the feature's `SOLO_PROVISIONER_ENABLE_<FEATURE>` override. A hidden
alpha command stays **discoverable** via `solo-provisioner feature list`, which
prints the feature and its exact opt-in env var.

Command availability is *maturity-based*, independent of the activation-version
axis: `--experimental` enables an alpha command even when the feature's
`ActiveFrom` is a future version. (The activation-version axis governs behaviour
rollout, not command availability.)

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

Resolution order (gatekit `resolve`):

1. An explicit **override** wins unconditionally (bypasses version and maturity).
2. Otherwise the last spec with `ActiveFrom <= active version`; if none is
   active yet, the feature is **off**.
3. An `ALPHA` spec is off unless the gate is opted in.
4. Otherwise the spec's `Default`.

Two operator opt-ins map onto this:

- **Per-feature override** — the `SOLO_PROVISIONER_ENABLE_<FEATURE>` environment
  variable (the feature name in `UPPER_SNAKE_CASE`, e.g.
  `SOLO_PROVISIONER_ENABLE_BLOCK_NODE_OPERATOR_MANAGED=1`). An override, so it
  forces the feature on (or off) **regardless of the activation version or
  maturity** — including *before* its `ActiveFrom`. This is a non-production
  escape hatch. The exact variable for each `ALPHA` feature is printed by
  `feature list`.
- **Gate-wide alpha opt-in** — `--experimental`. Opts into *all* `ALPHA`
  features, but only once the active version has reached each feature's
  `ActiveFrom`; it does **not** bypass the version gate.

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

## Related

- Epic [#1275](https://github.com/hashgraph/solo-weaver/issues/1275) — the
  decoupled activation-version feature-gate design (Layer 1 + Layer 2).
- [#1392](https://github.com/hashgraph/solo-weaver/issues/1392) — CLI command/flag
  maturity labelling via the registry.
- `internal/featuregate/` — the solo adapter (catalog, gate, env overrides).
- `github.com/automa-saga/gatekit` — the underlying library.
