# #533 — Review guide: external-files.yaml parser

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/533
> **Epic:** https://github.com/hashgraph/solo-weaver/issues/501
> **Branch:** `00533-external-files-parser`
> **Base:** `00532-infrastructure-versions-parser` (stacked; auto-retargets up the chain as upstream PRs merge)

## Problem and solution

`external-files.yaml` declares large remote files (over the ~1 MB ConfigurationFile-CR limit) that the upgrade-controller sidecar or the `solo-provisioner-upgrade` daemon must download and stage on the host before the consensus node restarts. Each entry specifies where to fetch the file from, how to verify its integrity (`algorithm` + `checksum`), where to put it (`destination`), whether a download failure is fatal (`optional`), and the timing of both download and install (`phase`).

The parser strict-decodes the YAML and validates: every entry has a non-empty `url`/`algorithm`/`checksum`/`destination`; `phase.download` is one of `{prepare, freeze}`; `phase.install` is `freeze` (the only legal value today). Destination uniqueness across the file list is enforced — two entries can't install to the same path, since they would silently overwrite each other.

### Spec note — supersedes the HIP draft

The HIP draft (`hip-xxxx0`) shows a single `hash: "sha256:..."` field; the story body for #533 supersedes that with separate `algorithm` + `checksum` fields (matching the convention already used in `pkg/software/config.go::Checksum` and now in `infrastructure-versions.yaml`'s `Binary` from #638). A manifest using the old shape fails parsing — `TestParseExternalFiles_RejectsHIPHashFieldShape` pins this.

### Scope boundary with #536

Story #533 implements the parser *infrastructure* for `destination`, validating that the field is non-empty. Story **#536** layers on the strict allowlist of marker prefixes (`HAPIAPP_DIR`, `SOLO_PROVISIONER_DIR`) — that's intentionally a separate PR so the policy is easy to audit and revisit independently of the parser. The test `TestParseExternalFiles_DestinationPrefixNotYetEnforced` pins this scope split: an arbitrary absolute path passes here, and #536 will be the PR that flips that.

### Stacked-PR layout

This branch is stacked on `00532-infrastructure-versions-parser` (PR #638), which is stacked on `00531-…` (PR #636), which is stacked on `00535-…` (PR #634). All four PRs target the epic branch `00501-manifest-parsing`. As upstream PRs merge, GitHub will auto-retarget each downstream PR down the chain.

## Changed files

| File | What changed |
|---|---|
| `pkg/manifests/external_files.go` (new) | Types (`ExternalFiles`, `ExternalFile`, `Phase`, `DownloadPhase`, `InstallPhase`), `ParseExternalFiles`, per-entry validation |
| `pkg/manifests/external_files_test.go` (new) | Happy path, empty-files no-op, `optional`-defaults-false pin, strict-decode rejections (incl. HIP-shape), table-driven validation failures including duplicate-destination, `phase.download`/`phase.install` enum cases, and the destination-prefix-deferred-to-#536 pin |
| `docs/claude/reviews/00533-external-files-parser.md` (new) | This guide |

## Code review checklist

- [ ] **`DownloadPhase` and `InstallPhase` are typed constants.** Validation switches over the typed constants so misspellings in production callers (e.g. `manifests.DownloadPhasePrepar`) fail to compile rather than fail at runtime.
- [ ] **`Optional` is a bool (not a pointer).** The story specifies a default of `false`; `yaml.v3` zeroes the field when absent, which is the same as the documented default. Pinned by `TestParseExternalFiles_OptionalDefaultsFalse`. Future enhancement (distinguishing nil from explicit-false) would need a pointer, but that's not in scope here.
- [ ] **Strict decode at this layer.** Unknown top-level fields and unknown per-entry fields (most importantly the HIP-shape `hash:`) fail with `ParseError`. Two dedicated test cases pin this.
- [ ] **Duplicate-destination check uses a map keyed by destination string.** The previous index is included in the error message so the operator knows both lines to look at.
- [ ] **Empty files list is valid.** `TestParseExternalFiles_EmptyFilesList` pins that a manifest with only `schemaVersion: v1` is a structurally valid no-op — useful in environments where no external files exist for a given release.
- [ ] **Scope boundary with #536.** `TestParseExternalFiles_DestinationPrefixNotYetEnforced` is the *contract* boundary: this PR validates destination is non-empty, #536 will add the strict allowlist. The test will be replaced (not deleted) in #536 with assertions that the strict allowlist now applies — clean, audit-able transition.

## Test commands

```bash
# Unit tests (this PR brings the package to 98.8% coverage)
go test -race -cover -tags='!integration' ./pkg/manifests/...

# Lint pipeline (errorx gate, gofmt)
task lint:check
```

Expected:

```
ok      github.com/hashgraph/solo-weaver/pkg/manifests    coverage: 98.8% of statements
```

## Manual UAT

No CLI surface, no daemon hook. The fixture at the top of `pkg/manifests/external_files_test.go` (`fullExternalFilesManifest`) is the canonical reference shape — reviewers can read it alongside the type definitions in `pkg/manifests/external_files.go`. The intended user-facing behaviour surfaces once a follow-on story wires this parser into the daemon/UC apply flow.
