# #535 — Review guide: schemaVersion validation across all manifests

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/535
> **Epic:** https://github.com/hashgraph/solo-weaver/issues/501
> **Branch:** `00535-manifest-schema-version-validation`
> **Base:** `00501-manifest-parsing`

## Problem and solution

Epic #501 introduces parsers for the four YAML files under `manifests/` in the CN deployment package. Every one of those files declares a `schemaVersion`, and the epic spec requires that an unknown value is rejected with a clear error *before* any further parsing. Story #535 is the cross-cutting prerequisite: do this once in a shared package so the four parser stories (#531/#532/#533/#534) all plug into a single validator instead of each rolling their own.

This PR lays down a new `pkg/manifests/` package whose only consumer-facing entry point is the schemaVersion validator `ValidateSchemaVersion(kind, data)`; supporting types (`Kind`, `SchemaVersion`, `Header`, `SchemaV1`), error classifications, and the `SupportedVersions` helper round out the surface area but exist solely to serve that one validator. There is no production caller yet — concrete parsers come in #531–#534, each as its own PR targeting `00501-manifest-parsing`. The epic branch is merged to `main` only after all six stories land.

The validator inspects only the `schemaVersion` field (other fields are tolerated at this stage so a future parser can use strict-decode on the *typed* root struct without surprise). The set of accepted versions is a per-kind map seeded with `{v1}` for all four kinds; widening to `v2` later is a one-line change.

## Changed files

| File | What changed |
|---|---|
| `pkg/manifests/manifests.go` (new) | `Kind`, `SchemaVersion`, `Header`, `supportedVersions` registry, `ValidateSchemaVersion`, `SupportedVersions` |
| `pkg/manifests/errors.go` (new) | `errorx` namespace + four error types (`ParseError`, `MissingSchemaVersionError`, `UnsupportedSchemaVersionError`, `UnknownKindError`) and their constructors |
| `pkg/manifests/manifests_test.go` (new) | Table-driven unit tests; 96.7% coverage |
| `docs/claude/reviews/00535-manifest-schema-version-validation.md` (new) | This guide |

## Code review checklist

- [ ] **Errors are classifiable by `errorx.IsOfType`.** Every constructor returns a typed `*errorx.Error` so downstream code (and tests) can branch on classification, mirroring the pattern used by `pkg/software/errors.go` and `pkg/security/principal/errors.go`.
- [ ] **No strict-decode at this layer.** `TestValidateSchemaVersion_ToleratesUnknownFields` pins the contract: extra top-level fields do *not* trigger an error here. This is deliberate — per-kind parsers in #531–#534 will apply strict decoding against their typed root struct, where field-level errors are useful rather than misleading.
- [ ] **Empty `schemaVersion:` is treated as missing.** YAML `schemaVersion:` (no value) decodes to the empty string and is rejected with `MissingSchemaVersionError`, not silently accepted.
- [ ] **Unsupported value error message includes the supported set.** `unsupported_schema_version: manifest "external-files" declares schemaVersion "v2" (supported: [v1])` — both the offending value and the accepted list are in the message, and stored as printable error properties for structured logging.
- [ ] **`Kind` defensive check.** `UnknownKindError` covers the programmer-mistake path where a caller passes an unregistered `Kind`. Not a user-input error, but worth surfacing explicitly so future parser PRs catch it during development.
- [ ] **`sortedSupported` deterministic ordering.** Map iteration order is randomised in Go; the helper sorts before returning so error messages and `SupportedVersions(kind)` are reproducible. (Coverage on the comparator is 83.3% only because we ship a single version today — the comparator branch will be exercised once a second version lands.)
- [ ] **Plan note on the field name.** The HIP draft (`hip-xxxx0`) uses `version:` for `external-files.yaml` and `state-sources.yaml` but `schemaVersion:` for the other two. The story bodies for #535/#533/#534 uniformly say `schemaVersion`. This PR follows the story bodies (newer than the HIP draft); if #533 or #534 confirms the on-disk field is actually `version`, the validator gains a per-kind YAML tag override at that time.

## Test commands

```bash
# Unit tests (the new package has no platform-specific code)
go test -race -cover -tags='!integration' ./pkg/manifests/...

# Lint pipeline (errorx gate, gofmt)
task lint:check
```

Expected:

```
ok      github.com/hashgraph/solo-weaver/pkg/manifests    coverage: 96.7% of statements
```

## Manual UAT

This PR introduces no CLI surface and no production caller, so there is no end-user UAT. The validator is exercised exclusively by its unit tests until a parser story (first one being #531) wires it in. Reviewers can inspect the API shape by reading `pkg/manifests/manifests.go` top-to-bottom — the package comment names the four manifest kinds and the deferred-to-follow-on stories.
