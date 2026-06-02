# #531 — Review guide: consensus-node-components.yaml parser

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/531
> **Epic:** https://github.com/hashgraph/solo-weaver/issues/501
> **Branch:** `00531-consensus-node-components-parser`
> **Base:** `00535-manifest-schema-version-validation` (stacked; auto-retargets to `00501-manifest-parsing` after #535 merges)

## Problem and solution

`consensus-node-components.yaml` ships inside every CN deployment package and declares the container images (consensus node plus five named sidecars: `recordStreamUploader`, `eventStreamUploader`, `blockStreamUploader`, `backupUploader`, `uc`) along with the layer-hash integrity records used to verify those images before they run. This story is the first concrete parser story under epic #501 and the first consumer of `ValidateSchemaVersion` from PR #634.

The parser strict-decodes the YAML into `ConsensusNodeComponents`, then runs semantic validation that goes beyond what struct shape alone can enforce — most importantly the *placement* rule for `layerHashes`: deterministic builds carry one shared set at the component level and no per-registry overrides; non-deterministic builds invert that. Getting this wrong silently would mean a missing or duplicated integrity record at apply time, so the validator rejects every misplacement explicitly.

### Spec note

The story body lists the five sidecar names above; the older HIP draft (`hip-xxxx0`) shows a single bundled `cheetah` sidecar. Stories supersede the HIP draft (same convention as the `provisioner.cli`/`provisioner.daemon` split flagged in #535's plan). Strict decoding therefore rejects `cheetah` (or any other unknown component name) — `TestParseConsensusNodeComponents_RejectsUnknownComponentName` pins that.

### "Absent = no change"

Per the manifest contract, every entry under `images:` is optional. A nil pointer (Go-side) corresponds to "section absent from the YAML" and signals "no change to that component". A separate `enabled *bool` field per entry encodes the operator's intent to turn a component on or off; nil there means "no change to the current enabled state" — distinct from `enabled: false` which is an explicit disable.

## Changed files

| File | What changed |
|---|---|
| `pkg/manifests/consensus_node_components.go` (new) | Types (`ConsensusNodeComponents`, `Images`, `Image`, `Deterministic`, `Registry`, `LayerHashes`), `SupportedImagePlatforms`, `ParseConsensusNodeComponents`, internal validators |
| `pkg/manifests/consensus_node_components_test.go` (new) | Happy paths (full, partial, empty `images:`), strict-decode rejections, schemaVersion gate, table-driven semantic-validation failures, multi-document rejection |
| `pkg/manifests/decode.go` (new) | Shared `decodeStrictSingleYAMLDoc(kind, data, out)` helper used by every concrete parser: enables `KnownFields(true)` and rejects multi-document inputs |
| `pkg/manifests/errors.go` | Added `ValidationError` type and `NewValidationError(kind, field, reason)` constructor; added `fieldProperty` printable property |
| `docs/claude/reviews/00531-consensus-node-components-parser.md` (new) | This guide |

## Code review checklist

- [ ] **`Image` field types model the "absent = no change" contract.** `Enabled *bool` distinguishes nil (no change), `true` (enable), and `false` (disable). Compare against `Deterministic *Deterministic` and the per-component `*Image` fields under `Images` — the pointer convention is consistent throughout.
- [ ] **`ParseConsensusNodeComponents` runs `ValidateSchemaVersion` first.** A future-versioned manifest is rejected before any current-shape decode can yield misleading errors. `TestParseConsensusNodeComponents_RejectsUnsupportedSchemaVersion` pins it.
- [ ] **Strict decode is enabled at this layer** (via the new `decodeStrictSingleYAMLDoc` helper in `pkg/manifests/decode.go`, which sets `dec.KnownFields(true)` and rejects multi-document inputs). Unknown top-level fields and unknown component names under `images:` both fail with `ParseError`. Multi-document YAML streams fail with `ValidationError` — see `TestParseConsensusNodeComponents_RejectsMultipleYAMLDocuments`. The `KnownFields(true)` strictness at parser level is intentional contrast with the schemaVersion validator from #535, which tolerates unknown fields by design (`TestValidateSchemaVersion_ToleratesUnknownFields` in PR #634).
- [ ] **Deterministic vs non-deterministic placement is enforced both ways.** Six dedicated test cases cover all four combinations (`deterministic.supported` × `layerHashes` present at component-level vs registry-level). The validation error messages explain which direction to move the data.
- [ ] **Platform identifiers are validated against `SupportedImagePlatforms`** (`linux/amd64`, `linux/arm64`). Unknown platform keys produce an error naming the offending value; empty hash lists per platform are rejected (a platform key with `[]` is meaningless).
- [ ] **Field paths in `ValidationError` messages are dotted Go paths** (e.g. `images.backupUploader.registries[0].layerHashes`). The format makes errors greppable against the YAML source and makes review failures easy to locate.
- [ ] **No production caller yet.** This PR ships the parser; #531 says nothing about wiring it into a workflow. That's deferred — the parser is exercised exclusively by its unit tests until a daemon/operator integration story picks it up.

## Test commands

```bash
# Unit tests for the parser (and the existing schemaVersion validator)
go test -race -cover -tags='!integration' ./pkg/manifests/...

# Lint pipeline (errorx gate, gofmt)
task lint:check
```

Expected:

```
ok      github.com/hashgraph/solo-weaver/pkg/manifests    coverage: 97.7% of statements
```

## Manual UAT

No CLI surface, no daemon hook, no production caller yet. Reviewers can spot-check the parser shape against the spec by running this REPL-style snippet in the repo root:

```bash
cat <<'EOF' | go run -trimpath -ldflags='-s -w' ./pkg/manifests/... 2>/dev/null || true
# (no main package — read the fixture instead)
EOF
cat pkg/manifests/consensus_node_components_test.go | sed -n '/fullDeterministicManifest = /,/^$/p' | head
```

The intended user-facing experience surfaces once a follow-on story (outside epic #501) wires this parser into the daemon's package-extraction flow.
