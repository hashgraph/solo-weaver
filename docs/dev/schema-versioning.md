# Schema Versioning (`pkg/schema`)

## Why this exists

Solo-weaver reads YAML files from two trust domains:

1. **Owned state files** — written by us (e.g. `self-upgrade.yaml`). We control
   the shape, so unknown fields mean corruption and should be rejected.
2. **Deployment-package manifests** — authored externally by the council and
   signed. We do not control the shape. HIP-1494 mandates that additive fields
   within a `schemaVersion` must not break consumers (lenient parsing).

Both face the same three version-mismatch problems:

1. **File older than binary (forward migration):** A v1 manifest is still in
   the field, but the binary has shipped v2. The binary must decode the old v1
   shape and transform it to the current in-memory struct so callers always
   work with one type.
2. **File newer than binary (version rejection):** A manifest authored by a
   newer tool or deployment package declares a `schemaVersion` the running
   build has never seen. The binary cannot safely decode a structure it does
   not understand, so it rejects early with `ErrUnsupportedVersion`.
3. **Same version, but file has unknown fields (lenient parsing):** The
   council adds a new field to a v1 manifest that only solo-operator uses.
   Solo-weaver's v1 struct does not have that field. With lenient parsing the
   unknown field is silently ignored — the binary decodes what it understands
   and skips the rest. Without it, every additive field would force a
   `schemaVersion` bump or lockstep releases across all consumers.

Before `pkg/schema`, each consumer reimplemented this:

- Probe `schemaVersion` from raw YAML.
- Guard against unsupported versions.
- Decode into the right struct.
- Enforce single-document YAML.

Four manifest parsers × the same five-step boilerplate = the kind of
duplication where a bug fix in one parser gets missed in the others.
`pkg/schema` extracts the cross-cutting orchestration once.

### Is this over-engineering?

Fair question. Today every manifest is `schemaVersion: 1` — there are no v2
structs, no migration chains, no version boundaries to cross. If the schema
never changes, the migration machinery is dead weight.

The bet is that schema evolution is *likely* (the HIPs are still in draft, the
manifest shapes are not final, and the daemon/operator/UC all consume them
independently at different upgrade cadences), and the cost of the abstraction
is *low* (one generic file, ~180 lines, no dependencies beyond `errorx` and
`yaml.v3`). The alternative — bolting migration onto each parser after the
fact — is messier and more error-prone than having the slot ready.

If you find that `schemaVersion` has been 1 for a year and migration has never
fired, the abstraction cost is a few lines of factory registration per parser.
That is a reasonable price for not having to retrofit it later under pressure.

## How it works

```
Raw YAML bytes
    │
    ▼
Phase 1: Probe version ──► "schemaVersion: N" (absent = v1)
    │                       Reject if N > CurrentVersion
    ▼
Phase 2: Decode ──────────► Into the sealed vN struct (strict or lenient)
    │                       Reject multi-document YAML
    ▼
Phase 3: Migrate ─────────► vN.MigrateToLatest() → current in-memory type T
    │
    (optional)
    ▼
Phase 4: Validate ────────► Validate(T) hook for semantic checks
```

## Building blocks

### `Migratable[T]`

Any sealed per-version struct implements this interface:

```go
type Migratable[T any] interface {
    MigrateToLatest() T
}
```

The terminal version (the one matching `CurrentVersion`) returns itself. Earlier
versions transform their fields into the next version's shape and delegate.

#### Migration chains

Each sealed struct carries a one-hop transform to the next version. When
`pkg/schema` calls `MigrateToLatest()`, the chain composes automatically:

```
v1.MigrateToLatest()  →  builds v2  →  v2.MigrateToLatest()  →  returns T
```

Concretely, suppose `Foo` is the current in-memory type and v2 just shipped:

```go
// fooV1 is sealed — never modify after shipping.
type fooV1 struct { OldField string `yaml:"oldField"` }

func (v1 *fooV1) MigrateToLatest() *Foo {
    v2 := &fooV2{NewField: v1.OldField}  // v1 → v2 transform
    return v2.MigrateToLatest()           // delegate forward
}

// fooV2 is the current version — identity migration.
type fooV2 struct { NewField string `yaml:"newField"` }

func (v2 *fooV2) MigrateToLatest() *Foo {
    return &Foo{NewField: v2.NewField}    // v2 IS latest
}
```

When v3 ships later, only v2's `MigrateToLatest()` changes (it now builds a
v3 and delegates). v1's stays the same — it still delegates to v2, which now
delegates to v3. Each version only knows its immediate successor:

```
v1 → v2 → v3 (current)
```

This keeps each migration step small, testable, and independent.

#### Prior art: stepped migration

This pattern is sometimes called "stepped migration" or "chain migration" and
is widely used across the industry:

The key property of stepped migration is **locality**: each version N only
knows how to produce version N+1. When v4 ships, you write one new transform
(v3 → v4) and update v3's `MigrateToLatest()` to delegate. Versions 1 and 2
are untouched — they already chain through v3. This means:

- **No combinatorial explosion** — you never write a direct v1 → v4
  transform. The chain composes.
- **Each hop is independently testable** — feed it a v(N) fixture, assert
  it produces a valid v(N+1).
- **Old versions are frozen** — once shipped, a sealed struct and its
  one-hop transform never change, reducing regression risk.

The alternative — direct "jump" migrations (v1 → latest, v2 → latest, etc.)
— requires updating every prior version's migration whenever the latest shape
changes. That scales as O(versions) per release instead of O(1).

### `Versioned[T]` and `Decode`

To define a new versioned schema, you need three things:

**Step 1 — Define the struct** with standard `yaml` tags:

```go
type ExternalFiles struct {
    Header `yaml:",inline"`                        // embeds schemaVersion, kind
    Files  []ExternalFile `yaml:"files,omitempty"`
}
```

**Step 2 — Implement `Migratable[T]`** on the struct. Since v1 is the only
(and therefore latest) version, this is an identity return:

```go
func (ef *ExternalFiles) MigrateToLatest() *ExternalFiles { return ef }
```

**Step 3 — Declare the schema:**

```go
var externalFilesSchema = schema.Versioned[*ExternalFiles]{
    CurrentVersion: 1,
    Lenient:        true,
    Factories: map[int]func() schema.Migratable[*ExternalFiles]{
        1: func() schema.Migratable[*ExternalFiles] { return &ExternalFiles{} },
    },
}
```

Field-by-field:

- **`Versioned[*ExternalFiles]`** — the type parameter `T` is
  `*ExternalFiles`. Every version's `MigrateToLatest()` must return this
  type. Callers always receive `T` regardless of which `schemaVersion` was
  in the YAML.
- **`CurrentVersion: 1`** — the highest version this binary understands.
  Any YAML with `schemaVersion` > 1 is rejected with `ErrUnsupportedVersion`.
- **`Lenient: true`** — unknown YAML fields are silently ignored (HIP-1494).
  Set to `false` for owned state files where unknown fields mean corruption.
- **`Factories`** — maps version number → constructor. When `Decode` probes
  the YAML and finds `schemaVersion: 1`, it calls `Factories[1]()` to get a
  fresh empty struct, YAML-decodes into it, then calls `MigrateToLatest()`
  on the result to produce `T`.
- **`VersionKey`** (optional) — the YAML key to probe. Defaults to
  `"schemaVersion"`.
- **`Validate`** (optional) — a `func(T) error` hook called after migration.
  Useful for cross-field semantic checks that belong in `pkg/schema` rather
  than in the caller.

**Step 4 — Write the parser function:**

```go
func ParseExternalFiles(data []byte) (*ExternalFiles, error) {
    doc, err := externalFilesSchema.Decode(data)  // probe → decode → migrate
    if err != nil { return nil, err }
    if err := doc.validate(); err != nil { return nil, err }  // domain validation
    return doc, nil
}
```

`Decode(data []byte) (T, error)` runs all four phases (probe → decode →
migrate → validate) and returns the migrated result. The parser function is
a thin wrapper that calls `Decode` and then runs any domain-specific
validation that doesn't belong in `pkg/schema`.

## Strict vs Lenient

| | Strict (`Lenient: false`) | Lenient (`Lenient: true`) |
|---|---|---|
| **Use for** | Owned state files (we write them) | External manifests (council-authored) |
| **Unknown fields** | Error | Silently ignored |
| **Why** | Unknown field = corruption or format drift | HIP-1494: additive changes must not break consumers |

### Why lenient parsing is safe

Lenient and version rejection solve two different problems:

- **Lenient parsing** handles additive evolution *within* a version boundary.
  The council adds a `metadata` key to a v1 manifest — our v1 struct doesn't
  have that field, so `yaml.v3` skips it. The document is still v1; its shape
  is compatible.

- **Version rejection** handles breaking evolution *across* version boundaries.
  A v3 manifest might have renamed, removed, or restructured fields that our
  v1/v2 structs depend on. Silently ignoring those changes would produce a
  corrupt in-memory object, so we reject early with `ErrUnsupportedVersion`.

This matters because multiple consumers (solo-weaver, solo-operator, the
daemon) read the same council-signed manifests at different upgrade cadences.
If the council adds a field that only solo-operator uses, solo-weaver should
not break — it just ignores what it does not understand. Without lenient
parsing, every additive field would force either a `schemaVersion` bump
(requiring all consumers to upgrade simultaneously) or lockstep releases
(defeating independent release cycles).

The risk — "what if we silently ignore a field we *should* have read?" — is
bounded: if a field is critical for a consumer, it belongs in that consumer's
struct and gets decoded normally. If it is not in the struct, that consumer
does not need it. The `validate()` hook catches the dangerous direction: a
required field that is *missing*.

This is the standard forwards-compatibility contract, similar to protobuf's
unknown-field handling: additive changes within a version are safe; structural
changes bump the version number and require migration code.

## Error types

| Error | When |
|-------|------|
| `schema.ErrMalformed` | Invalid YAML, multi-document, decode failure |
| `schema.ErrUnsupportedVersion` | `schemaVersion` > `CurrentVersion` or not in `Factories` |
| `schema.ErrValidation` | `Validate` hook returned an error |

## Strict consumer example (`internal/selfupgrade`)

The steps above used a lenient manifest (`Lenient: true`) as the example. For
an owned state file, the only difference is `Lenient: false` — everything
else follows the same pattern:

```go
type SelfUpgradeYAML struct { ... }  // current in-memory shape

type selfUpgradeV1 struct { ... }    // sealed v1 on-disk shape (never modify)

func (v *selfUpgradeV1) MigrateToLatest() SelfUpgradeYAML {
    return SelfUpgradeYAML{ /* field-by-field copy */ }
}

var stateSchema = schema.Versioned[SelfUpgradeYAML]{
    CurrentVersion: 1,
    Lenient:        false,  // strict — we wrote this file, unknown fields = corruption
    Factories: map[int]func() schema.Migratable[SelfUpgradeYAML]{
        1: func() schema.Migratable[SelfUpgradeYAML] { return &selfUpgradeV1{} },
    },
}

func Load(path string) (SelfUpgradeYAML, error) {
    data, _ := os.ReadFile(path)
    return stateSchema.Decode(data)
}
```

## Adding a v2 schema

When a manifest evolves (e.g., `external-files.yaml` v2 renames `algorithm`
to `hashAlgo`), you make four changes. The key insight is that `ExternalFiles`
always means "the latest shape" — its identity `MigrateToLatest()` never
changes. Old versions get frozen copies with versioned names.

```go
// 1. Copy the current ExternalFiles struct to a frozen v1 type.
//    This is a snapshot of the v1 shape — never modify it after shipping.
type externalFilesV1 struct {
    Header `yaml:",inline"`
    Files  []externalFileV1 `yaml:"files,omitempty"`
}

// 2. Add MigrateToLatest on the frozen v1 type.
//    This is the ONLY new method — it transforms v1 fields into the
//    current ExternalFiles shape.
func (v1 *externalFilesV1) MigrateToLatest() *ExternalFiles {
    // transform v1 fields -> current ExternalFiles shape
    // e.g., copy v1.Files[i].Algorithm -> current.Files[i].HashAlgo
}

// 3. Modify ExternalFiles to be the new v2 shape (rename fields, etc.).
//    Its MigrateToLatest is UNCHANGED — still the identity return.
//    This line was written when v1 was defined and never needs updating,
//    because ExternalFiles is always the latest version by definition.
func (ef *ExternalFiles) MigrateToLatest() *ExternalFiles { return ef }

// 4. Register both versions and bump CurrentVersion.
var externalFilesSchema = schema.Versioned[*ExternalFiles]{
    CurrentVersion: 2,
    Lenient:        true,
    Factories: map[int]func() schema.Migratable[*ExternalFiles]{
        1: func() schema.Migratable[*ExternalFiles] { return &externalFilesV1{} },
        2: func() schema.Migratable[*ExternalFiles] { return &ExternalFiles{} },
    },
}
```

Callers of `ParseExternalFiles` change nothing — they always receive
`*ExternalFiles` in its current shape, whether the input was v1 or v2.

## Deployment ordering for version bumps

Because the binary rejects any `schemaVersion` higher than its
`CurrentVersion`, a schema version bump is a **coordinated breaking change**.
The deployment must follow this order:

```
Step 1: Ship new binaries          Step 2: Ship v2 manifests
┌─────────────────────────┐        ┌─────────────────────────┐
│ solo-weaver   v1.8.0    │        │ council signs and       │
│ solo-operator v2.3.0    │  then  │ publishes deployment    │
│ daemon        v1.8.0    │ ────►  │ packages with           │
│                         │        │ schemaVersion: 2        │
│ All register v2 in      │        │                         │
│ their Factories map     │        │                         │
└─────────────────────────┘        └─────────────────────────┘
```

If a v2 manifest arrives before a consumer supports it, that consumer rejects
it with `ErrUnsupportedVersion` — by design. A binary that silently
half-parses a structure it does not understand is worse than a clean rejection.

This is the same constraint Kubernetes enforces: deploy the new controller
before creating resources with a new `apiVersion`.

**This is precisely why lenient parsing matters so much.** It keeps the
`schemaVersion` bump rare. Most manifest evolution is additive — new fields
within v1 — and does not require any coordination. Only genuinely structural
changes (renames, removals, reshaping) force a version bump. In practice,
that should be infrequent, and when it does happen, it is a deliberate,
planned rollout — not a surprise.

## Relationship to the migration framework

This is **not** the same as `internal/migration/` (documented in
`docs/dev/migration-framework.md`). That framework handles runtime migrations
— startup tasks that reconcile on-disk cluster state when the CLI binary
version crosses a boundary (e.g., re-rendering a Cilium config after an
upgrade). `pkg/schema` handles file-format migrations — loading a YAML file
whose structure has evolved across `schemaVersion` boundaries.

| | `pkg/schema` | `internal/migration` |
|---|---|---|
| **What migrates** | A single YAML file's struct shape | Cluster/host state across CLI upgrades |
| **When it runs** | At file load time | At CLI startup or during block-node upgrade |
| **Triggered by** | `schemaVersion` field in the file | CLI binary version boundary |
| **Side effects** | None (pure transform) | May re-render templates, re-apply Helm charts |
