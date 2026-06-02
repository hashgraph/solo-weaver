# #534 — Review guide: state-sources.yaml parser

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/534
> **Epic:** https://github.com/hashgraph/solo-weaver/issues/501
> **Branch:** `00534-state-sources-parser`
> **Base:** `00533-external-files-parser` (stacked; auto-retargets up the chain as upstream PRs merge)

## Problem and solution

`state-sources.yaml` declares the cloud storage locations from which a new or rejoining consensus node can fast-sync the latest saved-state snapshot — avoiding a full replay of the event stream from genesis. Multiple buckets across GCS, S3, and different regions are listed for redundancy and geographic locality. For each bucket, two parallel maps keyed by node ID locate the per-node index file (which names the latest available round) and the per-node base path (under which the round's state files live).

The parser strict-decodes the YAML and validates per source: `bucket` non-empty; `type ∈ {gcs, s3}`; `location` non-empty; `index` and `paths` both non-empty maps; *and* their key sets must match exactly (an index pointing at a node with no path is unfollowable; a path with no index has no latest-round pointer). Bucket URLs are unique across sources — two entries pointing at the same bucket would be ambiguous.

### Stacked-PR layout

At the top of the four-parser stack: `00501-…` ← #634 (#535) ← #636 (#531) ← #638 (#532) ← #639 (#533) ← **this PR**. All five target the epic branch via the chain; GitHub auto-retargets each downstream PR as upstreams merge.

This PR is the first concrete consumer of the `decodeStrictSingleYAMLDoc` helper introduced in #636's amend pass — written from the start to use it (the three earlier parsers were retrofitted via the cascade).

## Changed files

| File | What changed |
|---|---|
| `pkg/manifests/state_sources.go` (new) | Types (`StateSources`, `StateSource`, `BucketType`), `ParseStateSources`, per-source validation including index/paths key-set consistency and cross-source bucket uniqueness |
| `pkg/manifests/state_sources_test.go` (new) | Happy path, empty-stateSources no-op, schemaVersion gate (including the HIP-shape `version:` rejection), strict-decode + multi-doc rejection, 10 table-driven validation-failure cases, same-node-across-buckets-allowed pin |
| `docs/claude/reviews/00534-state-sources-parser.md` (new) | This guide |

## Code review checklist

- [ ] **`BucketType` is a typed constant.** Validation switches on `BucketTypeGCS` / `BucketTypeS3`; an unknown value yields a clear error naming both the offending value and the supported set.
- [ ] **`Sources` is the Go field name; YAML key is `stateSources`.** Avoids the package-level naming collision with the `StateSources` struct itself while preserving the on-disk schema.
- [ ] **`validateNodeKeysMatch` enforces set equality between `index` and `paths`.** Sorted alphabetically before reporting, so the same node name appears in the error on every run. Two dedicated test cases pin both directions (missing from paths; missing from index).
- [ ] **Cross-source bucket uniqueness.** Duplicate buckets across `stateSources[]` are rejected with a message naming the previous index, mirroring the duplicate-destination pattern in #533.
- [ ] **Same node ID across multiple buckets is allowed.** That's the whole point of multi-bucket redundancy. `TestParseStateSources_SameNodeAcrossBucketsAllowed` pins it.
- [ ] **Empty `stateSources:` is valid.** Some networks may not use fast-sync; the parser must not require at least one source. `TestParseStateSources_EmptyStateSourcesTolerated` pins it.
- [ ] **HIP `version: v1` rejection is via the schemaVersion validator**, not via strict-decode. `version:` is silently ignored at the schemaVersion stage (the validator inspects only `schemaVersion`), so the manifest surfaces as `MissingSchemaVersionError`. The test names this contract explicitly.

## Test commands

```bash
# Unit tests (this PR brings the package to 97.8% coverage with the parser + 30 tests)
go test -race -cover -tags='!integration' ./pkg/manifests/...

# Lint pipeline (errorx gate, gofmt)
task lint:check
```

Expected:

```
ok      github.com/hashgraph/solo-weaver/pkg/manifests    coverage: 97.8% of statements
```

## Manual UAT

No CLI surface, no daemon hook. The fixture at the top of `pkg/manifests/state_sources_test.go` (`fullStateSourcesManifest`) is the canonical reference shape, mirroring the example in the HIP for `state-sources.yaml`. The intended user-facing behaviour surfaces once a follow-on story wires this parser into the daemon's fast-sync bootstrap flow.
