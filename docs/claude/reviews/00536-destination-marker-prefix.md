# #536 — Review guide: destination marker-prefix enforcement on external-files.yaml

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/536
> **Epic:** https://github.com/hashgraph/solo-weaver/issues/501
> **Branch:** `00536-destination-marker-prefix`
> **Base:** `00534-state-sources-parser` (stacked; auto-retargets up the chain as upstream PRs merge)

## Problem and solution

In #639 (story #533), the `external-files.yaml` parser validated that the `destination` field was non-empty but accepted any string. That left a hole: a council-signed manifest could declare a destination like `/etc/cron.d/x.sh` and the downloader would faithfully drop a file there. The scope-boundary test `TestParseExternalFiles_DestinationPrefixNotYetEnforced` pinned that deferral explicitly.

This PR closes the hole. The `destination` field must now start with one of the recognised marker prefixes (`HAPIAPP_DIR` or `SOLO_PROVISIONER_DIR`) followed by `/`. Arbitrary filesystem paths, unknown markers, and near-misses (e.g. `HAPIAPP_DIR_FOO/x`) all fail parsing with a clear error naming both the offending destination and the allowed set.

The scope-boundary test from #533 is **replaced** (not deleted) by a richer `TestParseExternalFiles_DestinationPrefixEnforcement` that pins both directions: positive cases for each allowed marker (including the `marker/` empty-subpath form, which is deliberately accepted because "sensible path under HAPIAPP_DIR" is the downloader's responsibility, not the parser's), and negative cases for absolute paths, relative paths, unknown markers, and near-misses.

### Stacked-PR layout

Top of the six-PR stack: `00501-…` ← #634 ← #636 ← #638 ← #639 ← #640 ← **this PR**. All six target the epic branch via the chain; GitHub auto-retargets each downstream PR as upstreams merge. This is the **final story** for epic #501 — once this lands and the chain unrolls into the epic branch, the epic merges to main.

## Changed files

| File | What changed |
|---|---|
| `pkg/manifests/external_files.go` | Added `AllowedDestinationPrefixes` exported slice and `validateDestinationPrefix(field, dest)` helper; wired into `ExternalFile.validate` after the non-empty check. New import: `strings` |
| `pkg/manifests/external_files_test.go` | Removed `TestParseExternalFiles_DestinationPrefixNotYetEnforced`; added `TestParseExternalFiles_DestinationPrefixEnforcement` (3 accept + 5 reject subtests) and `TestAllowedDestinationPrefixes_PinnedSet` |
| `docs/claude/reviews/00536-destination-marker-prefix.md` (new) | This guide |

## Code review checklist

- [ ] **`AllowedDestinationPrefixes` is exported.** External tooling (e.g. CN deployment-package linters) can reuse the same closed set without forking the list. The slice is package-level, not a function — easier to grep against.
- [ ] **Trailing `/` is required.** Without it, `validateDestinationPrefix("HAPIAPP_DIR")` would pass — bad. With it, the marker must be followed by `/`. `TestParseExternalFiles_DestinationPrefixEnforcement` pins both the rejection of `HAPIAPP_DIR` (alone) and `HAPIAPP_DIR_FOO/x` (prefix-of-longer-token).
- [ ] **`HAPIAPP_DIR/` is accepted (empty subpath).** Marker validation stops at "is the marker recognised". Whether the resulting path is sensible under that marker is the downloader's call, not the parser's. The test names this contract explicitly.
- [ ] **Error message lists the allowed set.** When a destination is rejected, the operator sees both the offending value and the prefixes they could've used — saves a round-trip to the docs.
- [ ] **The scope-boundary test from #533 is gone.** `TestParseExternalFiles_DestinationPrefixNotYetEnforced` was the explicit pin saying "this enforcement is deferred to #536." It's now replaced by the positive enforcement tests — the deferral is satisfied, the boundary is closed.
- [ ] **`TestAllowedDestinationPrefixes_PinnedSet` is the API surface guardrail.** Widening or narrowing the closed set requires deliberately changing that test — surfaces as a focused diff during review.

## Test commands

```bash
# Unit tests (97.9% coverage)
go test -race -cover -tags='!integration' ./pkg/manifests/...

# Targeted: just the new enforcement tests
go test -race -tags='!integration' -run 'DestinationPrefix|AllowedDestinationPrefixes' ./pkg/manifests/...

# Lint pipeline
task lint:check
```

Expected:

```
ok      github.com/hashgraph/solo-weaver/pkg/manifests    coverage: 97.9% of statements
```

## Manual UAT

No CLI surface; the only behavioural change is that an `external-files.yaml` shipping a destination outside the closed marker set will now fail parsing. Worth sanity-checking against any in-flight package fixtures that callers might be testing with — none currently exist in the repo (the parsers have no production caller yet, per the deliberate scope of epic #501).
