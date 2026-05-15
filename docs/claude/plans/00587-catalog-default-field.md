# Phase 1 (#587) — Add explicit `default:` to artifact catalog; retire `GetLatestVersion()` fallback

> **Suggested title for GH issue:**
> `refactor(software): add explicit default: field to artifact catalog and stop relying on GetLatestVersion()`
>
> **Related phases:** #587 (this) · #588 (rename + `cluster:` section) · #589 (wire Helm steps + retire `deps.go` versions)

## Summary

This phase introduces an explicit `default:` field on every entry in
`pkg/software/artifact.yaml` and switches the binary installer off the
implicit "highest semver wins" rule.

**No file rename, no `cluster:` section, no Helm changes in this phase** — those
are Phases 2 and 3 (see "Related work" below).

## Problem

`pkg/software/artifact.yaml` has no `default:` field. When an entry lists
multiple versions, `ArtifactMetadata.GetLatestVersion()` sorts semver
descending and returns the highest (`pkg/software/config.go:285-320`,
called from `pkg/software/base_installer.go:94`).

Consequences:
- Adding a new version entry — for testing, staging a future bump, or pinning a
  fix — silently becomes the new default for every operator.
- A reader of `artifact.yaml` cannot tell which version will actually be selected
  without reading Go code.
- There is no way to ship a manifest where multiple versions are *available* but
  only one is *recommended*.

## Decisions

| Question | Decision |
|---|---|
| Default selection | Explicit `default:` field per entry; implicit-latest selection is removed for `host:` (binary) entries |
| Default for `cluster:` (Helm) | `GetLatestVersion()`-style implicit semver is **not** added to charts — chart versions don't necessarily follow semver and latest is never the right default without explicit intent |

## Scope

- [ ] Add `default: "<version>"` to every entry in `pkg/software/artifact.yaml`.
      All current entries list exactly one version, so the default is unambiguous.
- [ ] Introduce `ArtifactMetadata.GetDefaultVersion() (string, error)` in
      `pkg/software/config.go` that:
      - Returns the value of the `default:` field if present.
      - Returns a load-time / lookup error if `default:` is missing or names a
        version not present in `versions:`.
- [ ] Switch `pkg/software/base_installer.go:94` (and any other callers) from
      `GetLatestVersion()` to `GetDefaultVersion()`.
- [ ] Decide the fate of `GetLatestVersion()`: either delete it (preferred, since
      no caller should rely on implicit-latest going forward) or mark it `//
      Deprecated:` and leave a single line explaining why.
- [ ] Update unit tests in `pkg/software/config_test.go` (and
      `config_it_test.go` for integration) to cover:
      - `default:` resolves to the named version.
      - Missing `default:` is a clear error.
      - `default:` pointing to an unknown version is a clear error.
      - Multi-version fixtures (test-only) confirm `default:` controls the
        selection, not semver ordering.

## Out of scope (this issue)

- Renaming `artifact.yaml` → `infrastructure-versions.yaml` and `artifact:` → `host:`
  (Phase 2).
- Adding a `cluster:` section with Helm chart entries (Phase 2).
- Wiring Helm install paths off `pkg/deps/deps.go` constants (Phase 3).
- Removing version constants from `pkg/deps/deps.go` (Phase 3).
- Checksum/digest verification for Helm charts (Phase 3).
- Topology constants (`*_NAMESPACE`, `*_RELEASE`, `*_CHART`, `*_REPO`) in
  `pkg/deps/deps.go` — separate follow-up.
- Hidden emergency override flag `--override-component <name>=<version>` —
  separate follow-up.

## Acceptance criteria

- [ ] Every entry in `pkg/software/artifact.yaml` has a `default:` field that
      names a version present in its `versions:` map.
- [ ] `ArtifactMetadata.GetDefaultVersion()` exists and is used by the binary
      installer; `GetLatestVersion()` is either deleted or no longer called by
      production code paths.
- [ ] `task lint` passes.
- [ ] `task test:unit` and `task vm:test:integration` pass.
- [ ] A multi-version test fixture confirms that changing `default:` (not adding
      a new entry) controls selection.
- [ ] No behaviour change for any operator: the `default:` value matches what
      `GetLatestVersion()` returned before this PR for every shipped entry.

## Related work

- **Phase 2 (#588) — Unified `infrastructure-versions.yaml` with `host:` + `cluster:` sections.**
  Renames the file and key, adds the seven infra charts with integrity
  metadata.
- **Phase 3 (#589) — Wire Helm steps to the catalog; retire version constants in
  `deps.go`.** Switches the workflow steps off the hardcoded chart versions
  and adds checksum/digest verification before `helm install`/`upgrade`.

## References

- `pkg/software/artifact.yaml` — the catalog to modify.
- `pkg/software/config.go:285-320` — `GetLatestVersion()` implementation.
- `pkg/software/base_installer.go:94` — implicit-latest call site.
- `pkg/software/config_test.go`, `pkg/software/config_it_test.go` — existing
  tests covering `GetLatestVersion()`; update to cover `GetDefaultVersion()`.
