# #501 — Demo guide: Epic A38_4, Manifest Parsing and Validation

> **Epic:** https://github.com/hashgraph/solo-weaver/issues/501
> **Feature branch:** `00501-manifest-parsing` (off `main`)
> **Tip of stack:** `00536-destination-marker-prefix` (PR #641) — transitively contains every story; use this checkout to demo
> **Stories:** #531, #532, #533, #534, #535, #536 — per-story review docs are linked at the bottom

## What this epic delivers

A new `pkg/manifests/` package that parses and validates the four YAML files shipped inside every CN deployment package:

| File | Purpose | Story |
|---|---|---|
| `consensus-node-components.yaml` | Container images + layer-hash integrity for the CN and its five sidecars | #531 |
| `infrastructure-versions.yaml` | Pinned `provisioner.cli`/`provisioner.daemon` versions + audit records for every host binary / Helm chart | #532 |
| `external-files.yaml` | Large remote files (>1 MB) the upgrade-controller / `solo-provisioner-upgrade` daemon downloads and stages | #533, #536 |
| `state-sources.yaml` | GCS / S3 buckets for fast-sync of saved-state snapshots | #534 |

Cross-cutting under the same package:

- `ValidateSchemaVersion(kind, data)` — per-kind allowlist of schema versions, runs before any structural decode (#535).
- `decodeStrictSingleYAMLDoc(kind, data, out)` — strict YAML decode helper (`KnownFields(true)` + multi-document rejection) used by all four parsers.
- Typed `errorx` errors: `ParseError`, `MissingSchemaVersionError`, `UnsupportedSchemaVersionError`, `UnknownKindError`, `ValidationError` — every failure mode is `errorx.IsOfType`-classifiable.
- Field-path errors with dotted Go paths (e.g. `images.backupUploader.registries[0].layerHashes`) — greppable against the YAML source.

## What this epic intentionally does not deliver

- **No production caller.** The parsers exist but nothing in the daemon or CLI imports them yet. Wiring into the package-extraction flow / fast-sync bootstrap / UC apply is a follow-on epic.
- **No CLI surface.** Nothing the operator can run by hand — exercise is exclusively unit-test-driven.
- **No catalog cross-checks.** `infrastructure-versions.yaml` host[]/cluster[] names are not validated against `pkg/software/infrastructure-catalog.yaml`; that's a runtime concern, not a parse concern.

Frame this explicitly during the demo: this is the **foundation layer** that downstream epics will plug into. If the audience expects to see a feature, that's the wrong expectation.

## How to walk the team through it (~15 min)

### 1. Frame the epic (1 min)
Open issue #501. Read the one-liner ("Parse and validate all `manifests/` YAML files from the deployment package") and call out the `order: foundation` label — others can't start until this lands.

### 2. API tour (4 min)
From the tip checkout, walk `pkg/manifests/` top-down in dependency order:

```
manifests.go              # Kind enum, SchemaVersion, supportedVersions registry, ValidateSchemaVersion
decode.go                 # decodeStrictSingleYAMLDoc helper
errors.go                 # errorx namespace + 5 typed errors
consensus_node_components.go
infrastructure_versions.go
external_files.go         # includes AllowedDestinationPrefixes() — cloned slice, callers can't weaken enforcement
state_sources.go
```

Each parser has the same shape: `Parse<Kind>(data)` → `ValidateSchemaVersion` → strict decode → semantic validate. The repetition is the point — adding a fifth YAML kind is a one-file change.

### 3. Validation in action (5 min)
Run the unit suite verbosely and let the test names do the narration:

```bash
go test -v -tags='!integration' ./pkg/manifests/... 2>&1 | head -120
```

Then surface four representative failure modes by name (one per parser):

```bash
# schemaVersion gate (cross-cutting)
go test -v -tags='!integration' -run 'UnsupportedSchemaVersion' ./pkg/manifests/...

# Deterministic vs registry-level layerHashes placement (#531)
go test -v -tags='!integration' -run 'DeterministicLayerHashesPlacement|NonDeterministicLayerHashesPlacement' ./pkg/manifests/...

# Duplicate destination across external-files entries (#533)
go test -v -tags='!integration' -run 'DuplicateDestination' ./pkg/manifests/...

# Marker-prefix allowlist on destination (#536)
go test -v -tags='!integration' -run 'DestinationPrefixEnforcement' ./pkg/manifests/...
```

For each, point out the field-path in the error message — that's the operator-facing contract.

### 4. HIP-draft divergences (2 min)
The HIP draft (`hip-xxxx0`) predates the story bodies on three shapes; the stories supersede the HIP. The strict decoder rejects each old shape with a dedicated regression test — show them as a single grep:

```bash
grep -nE "RejectsHIP|RejectsUnknownComponentName" pkg/manifests/*_test.go
```

The three divergences:

| YAML | HIP draft shape | Epic-#501 shape | Pinned by |
|---|---|---|---|
| `consensus-node-components.yaml` | single `cheetah` sidecar | five named sidecars (`recordStreamUploader`, `eventStreamUploader`, `blockStreamUploader`, `backupUploader`, `uc`) | `TestParseConsensusNodeComponents_RejectsUnknownComponentName` |
| `infrastructure-versions.yaml` | single `provisioner.version` | `provisioner.cli` + `provisioner.daemon` split (matches the two-binary release layout from epic #498) | `TestParseInfrastructureVersions_RejectsHIPSingleProvisionerVersion` |
| `external-files.yaml` | single `hash: "sha256:..."` field | separate `algorithm` + `checksum` fields (matches `pkg/software/config.go::Checksum`) | `TestParseExternalFiles_RejectsHIPHashFieldShape` |

If HIP authors are in the room, this is the conversation to have — confirm the story bodies are the final shape or schedule a HIP update.

### 5. Scope boundary and what's next (2 min)
- Parsers compile but aren't called. Show `grep -rn 'pkg/manifests' --include='*.go' | grep -v _test.go | grep -v 'pkg/manifests'` — empty.
- The integration lands primarily in **Epic #502 — A38_5 Network Upgrade Workflow (Execute Phase)** (`order: core`, depends on this epic). Five of its eight stories directly consume the #501 parsers:

  | Story | Parser consumed | What it wires up |
  |---|---|---|
  | #537 (5.1) | `external-files.yaml` (#533/#536) | Download step + disk-space check + halt/skip policy |
  | #538 (5.2) | `external-files.yaml` (#533/#536) | Atomic-write install during freeze; honours `HAPIAPP_DIR`/`SOLO_PROVISIONER_DIR` markers |
  | #539 (5.3) | `infrastructure-versions.yaml` (#532) | Atomic-write placement before infra upgrade |
  | #540 (5.4) | `infrastructure-versions.yaml` (#532) | Detect declared-vs-installed delta, orchestrate teardown/reinstall |
  | #541 (5.5) | `consensus-node-components.yaml` (#531) | ConsensusConfig CR creation from `data/config/` scan |

- Two smaller integrations live outside #502:
  - **Epic #504 → Story #554** (7.3) — `solo-provisioner consensus node restore` CLI command consumes `state-sources.yaml` (#534).
  - **Epic #506 → Story #562** (9.2) — validate declared infra versions against the built-in catalog (consumes #532). This is also where the catalog cross-check that we deliberately deferred from #532 lands.

- During the demo, the most quotable single forward-pointer is **#540** ("Detect when infrastructure versions declared in the manifest differ from what is installed") — its acceptance criteria read like a one-line user story for why this epic exists.

## Demo prep checklist

- [ ] `git fetch origin && git checkout 00536-destination-marker-prefix && git pull` — tip of the stack has everything.
- [ ] `go test -race -cover -tags='!integration' ./pkg/manifests/...` shows ~97.9% coverage, no failures.
- [ ] `task lint:check` is clean.
- [ ] Stack visualisation ready: epic ← #634 (#535) ← #636 (#531) ← #638 (#532) ← #639 (#533) ← #640 (#534) ← #641 (#536). None of the six has merged at demo time; that's expected — they unroll into the epic branch in order, then the epic merges to `main`.

## Per-story review docs

Each story ships its own review guide; reference these for deep-dives during Q&A.

| Story | Title | PR | Doc |
|---|---|---|---|
| #535 | schemaVersion validator (cross-cutting) | #634 | `docs/claude/reviews/00535-manifest-schema-version-validation.md` |
| #531 | `consensus-node-components.yaml` parser | #636 | `docs/claude/reviews/00531-consensus-node-components-parser.md` |
| #532 | `infrastructure-versions.yaml` parser | #638 | `docs/claude/reviews/00532-infrastructure-versions-parser.md` |
| #533 | `external-files.yaml` parser | #639 | `docs/claude/reviews/00533-external-files-parser.md` |
| #534 | `state-sources.yaml` parser | #640 | `docs/claude/reviews/00534-state-sources-parser.md` |
| #536 | destination marker-prefix enforcement | #641 | `docs/claude/reviews/00536-destination-marker-prefix.md` |

## Risks and open questions to surface during Q&A

- **HIP draft vs story bodies.** Three shape divergences (above). Story bodies have been treated as authoritative; the HIP needs an editorial pass to match.
- **Algorithm field is not enum-restricted.** `Binary.algorithm` accepts any non-empty string; a future "must be sha256" gate would live centrally in `pkg/software/config.go::Checksum`, not in the parsers.
- **`Optional` (on `external-files` entries) is a bool, not `*bool`.** Distinguishing nil from explicit-false would need a pointer — out of scope for this epic; flag if downstream needs it.
- **No catalog membership check** on `infrastructure-versions.yaml` host[]/cluster[] names. Deliberate scope boundary — surface the choice so reviewers don't expect it here.
