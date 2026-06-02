# #532 — Review guide: infrastructure-versions.yaml parser

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/532
> **Epic:** https://github.com/hashgraph/solo-weaver/issues/501
> **Branch:** `00532-infrastructure-versions-parser`
> **Base:** `00531-consensus-node-components-parser` (stacked; auto-retargets up the chain as upstream PRs merge)

## Problem and solution

`infrastructure-versions.yaml` ships inside every CN deployment package and pins two things the provisioner needs at apply time:

1. **The exact `solo-provisioner` versions** to install — split into `provisioner.cli` (CLI binary) and `provisioner.daemon` (daemon binary), each with `version` + `algorithm` + `checksum`. The split reflects the existing two-binary release layout from epic #498 (CLI tags `vX.Y.Z`; daemon tags `daemon-vX.Y.Z` — independently released).
2. **An audit record** of every host-level binary (`cri-o`, `kubelet`, `kubeadm`, etc.) and Helm chart (`alloy`, `metallb`, `metrics-server`, etc.) whose installed version the provisioner will reconcile against its embedded catalog at runtime. These are deliberately **audit-only** records — they do not select versions; `provisioner.<cli|daemon>.version` does.

The parser strict-decodes the YAML, then validates: provisioner sub-sections have all three integrity fields when present, host[] and cluster[] entries have non-empty name + version, and names are unique within each section (duplicate names would produce ambiguous audit records).

### Spec note — supersedes the HIP draft

The HIP draft (`hip-xxxx0`) shows a single `provisioner.version` field. The story body for #532 supersedes that with the `cli`/`daemon` split — same convention as the `cheetah` → 5-sidecar split flagged in #531. Strict decoding therefore rejects a manifest with `provisioner.version` at the top level of `provisioner:` — `TestParseInfrastructureVersions_RejectsHIPSingleProvisionerVersion` pins this contract.

### Stacked-PR layout

This branch is stacked on `00531-consensus-node-components-parser` (PR #636), which is itself stacked on `00535-manifest-schema-version-validation` (PR #634). The reason is the shared `ValidationError` errorx type and `NewValidationError` constructor that #531 added to `pkg/manifests/errors.go` — both #532 and #531 use it. Stacking avoids a merge conflict on `errors.go` and keeps the typed-error surface defined exactly once.

As the upstream PRs merge into the epic branch, GitHub will auto-retarget this PR down the chain (PR #636 first, then this one), arriving at `00501-manifest-parsing` cleanly.

## Changed files

| File | What changed |
|---|---|
| `pkg/manifests/infrastructure_versions.go` (new) | Types (`InfrastructureVersions`, `Provisioner`, `Binary`, `HostComponent`, `ClusterChart`), `ParseInfrastructureVersions`, internal validators |
| `pkg/manifests/infrastructure_versions_test.go` (new) | Happy paths (full + partial), schemaVersion gate, strict-decode rejections (incl. the HIP-shape rejection), table-driven validation failures including duplicate-name cases |
| `docs/claude/reviews/00532-infrastructure-versions-parser.md` (new) | This guide |

## Code review checklist

- [ ] **`Provisioner`, `Provisioner.CLI`, `Provisioner.Daemon` are pointer-typed.** A manifest with only `provisioner.cli` present (or with no `provisioner:` block at all) is valid under "absent = no change" — verified by `TestParseInfrastructureVersions_OnlyCLIPresent` and `TestParseInfrastructureVersions_AllSectionsAbsent`.
- [ ] **Strict decode at this layer.** Unknown top-level fields and unknown sub-keys under `provisioner:` (most importantly the old HIP-shape `provisioner.version`) fail with `ParseError`. Two dedicated test cases pin this.
- [ ] **All three integrity fields required when a `Binary` is present.** `Binary.validate` checks `version`, `algorithm`, and `checksum` individually so a missing one produces a precise field-path error.
- [ ] **Host and cluster name uniqueness is per-section.** Same name in both `host[]` and `cluster[]` is allowed (`TestParseInfrastructureVersions_NamesShareableAcrossSections`) — but duplicates *within* a section are rejected.
- [ ] **Algorithm is not enum-restricted.** The Binary struct accepts any non-empty algorithm string, matching the convention in `pkg/software/config.go::Checksum`. A future "must be sha256" enforcement can be added centrally there once the catalog standardises on it.
- [ ] **Validator does not depend on host[]/cluster[] catalog membership.** The provisioner catalog membership check (whether the named host or cluster component is actually recognised by the embedded `pkg/software/infrastructure-catalog.yaml`) is intentionally a runtime concern, not a manifest-parse concern — keeping this PR focused on syntax + per-entry semantics.
- [ ] **No production caller yet.** Like #531, this story ships the parser only. Wiring into the daemon/UC apply flow is a follow-on outside epic #501.

## Test commands

```bash
# Unit tests (this PR brings the package to 98.5% coverage)
go test -race -cover -tags='!integration' ./pkg/manifests/...

# Lint pipeline (errorx gate, gofmt)
task lint:check
```

Expected:

```
ok      github.com/hashgraph/solo-weaver/pkg/manifests    coverage: 98.5% of statements
```

## Manual UAT

No CLI surface, no daemon hook. Reviewers can spot-check the parser shape by reading the fixture at the top of `pkg/manifests/infrastructure_versions_test.go` (`fullInfrastructureVersionsManifest`) against `pkg/manifests/infrastructure_versions.go::InfrastructureVersions`. The intended user-facing behaviour surfaces once a follow-on story wires this parser into the daemon's package-extraction flow.
