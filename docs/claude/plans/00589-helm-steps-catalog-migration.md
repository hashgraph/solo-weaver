# Phase 3 (#589) — Wire Helm workflow steps to `infrastructure-versions.yaml`; verify checksum/digest; retire chart version constants in `deps.go`

> **Suggested title for GH issue:**
> `refactor(workflows): consume cluster: catalog for Helm steps, verify chart integrity, retire version constants in deps.go`
>
> **Related phases:** #587 (`default:` field for binary catalog) · #588 (rename + `cluster:` section) · #589 (this)

## Summary

This phase switches the Helm workflow steps off the hardcoded
`pkg/deps/deps.go` constants and onto the `cluster:` section of
`pkg/software/infrastructure-versions.yaml` added in **Phase 2**. It also introduces
chart integrity verification (SHA256 of `.tgz` for classic repos, OCI manifest
digest for OCI registries) before `helm install`/`upgrade`.

After this PR, `pkg/deps/deps.go` no longer carries any chart **version**
constants; topology constants (namespace, release name, chart name, repo URL)
stay until a separate cleanup PR.

## Problem

After Phases 1 and 2, the catalog exists with the seven infra charts and their
defaults, but nothing reads from it. The workflow steps still pull their chart
versions from the Go constants in `pkg/deps/deps.go`:

- `internal/workflows/steps/step_alloy.go:265` → `deps.NODE_EXPORTER_VERSION`
- `internal/workflows/steps/step_alloy.go:446` → `deps.ALLOY_VERSION`
- `internal/workflows/steps/step_metallb.go:87` → `deps.METALLB_VERSION`
- `internal/workflows/steps/step_metrics_server.go:69` → `deps.METRICS_SERVER_VERSION`
- `internal/workflows/steps/step_prometheus_operator_crds.go:68` → `deps.PROMETHEUS_OPERATOR_CRDS_VERSION`
- `internal/workflows/steps/step_external_secrets.go:77` → `deps.EXTERNAL_SECRETS_VERSION`
- Teleport cluster agent step → `deps.TELEPORT_VERSION` (overridable via the
  pre-existing `--version` flag at `cmd/weaver/commands/teleport/cluster/cluster.go:32`)

There is also no integrity verification on chart pulls today.

## Decisions

| Question | Decision |
|---|---|
| Version selection | Catalog defaults drive installation; no per-component CLI overrides |
| Integrity model | Verify `.tgz` SHA256 (classic) / OCI manifest digest (OCI) before `helm install` / `upgrade` |
| `deps.go` retirement | Version constants move out; topology constants stay until a separate cleanup PR |
| Teleport `--version` flag | Pre-existing anomaly; per-component flags are treated as legacy, but the flag is **not** removed in this phase |

## Scope

### Wire workflow steps to the catalog

- [ ] Inject `*ComponentCatalog` (from Phase 2) into each Helm workflow step,
      or expose a package-level accessor — pick the pattern that fits the
      existing step dependency-injection style.
- [ ] Replace each `deps.<CHART>_VERSION` reference with a call to
      `catalog.GetClusterComponent("<name>").GetDefaultVersion()` (or the
      idiomatic equivalent).
- [ ] Read chart coordinates (`chart`, `repo`, `type`) from the catalog as
      well, **even though they're duplicated in `deps.go` today** — this avoids
      future drift between the catalog and the install path. Topology
      constants (namespace, release name) continue to come from `deps.go`.

Workflow step files to update:

- `internal/workflows/steps/step_alloy.go` (uses `ALLOY_VERSION` and
  `NODE_EXPORTER_VERSION`)
- `internal/workflows/steps/step_metallb.go`
- `internal/workflows/steps/step_metrics_server.go`
- `internal/workflows/steps/step_prometheus_operator_crds.go`
- `internal/workflows/steps/step_external_secrets.go`
- Teleport cluster agent workflow step (find via `grep -rn TELEPORT_VERSION
  internal/workflows`)

### Add chart integrity verification

- [ ] Before `helm install` / `helm upgrade`, the workflow step must:
      1. `helm pull` the chart to a temporary directory.
      2. Compute the SHA256 of the resulting `.tgz` (classic) or read the OCI
         manifest digest from the pull metadata (OCI).
      3. Compare against the `algorithm` + `checksum` recorded in the catalog
         for the selected version.
      4. Abort the step on mismatch with a clear error identifying the
         component, expected checksum, and observed checksum.
      5. Pass the locally pulled chart path to `helm install`/`upgrade` to
         avoid a second download.
- [ ] Factor the pull-and-verify logic into a helper in
      `pkg/helm/` (or `internal/...` if the helm package is intentionally thin)
      so all six steps share one implementation.
- [ ] Wire it through the existing `pkg/helm/` interface and its mock for unit
      tests.

### Retire chart version constants in `deps.go`

- [ ] Delete the following constants from `pkg/deps/deps.go`:
      `ALLOY_VERSION`, `METALLB_VERSION`, `METRICS_SERVER_VERSION`,
      `NODE_EXPORTER_VERSION`, `PROMETHEUS_OPERATOR_CRDS_VERSION`,
      `EXTERNAL_SECRETS_VERSION`, `TELEPORT_VERSION`.
- [ ] Keep all `*_NAMESPACE`, `*_RELEASE`, `*_CHART`, `*_REPO` constants —
      topology cleanup is deferred to a separate PR.
- [ ] Update `cmd/weaver/commands/teleport/cluster/cluster.go:32`. The help
      text references `deps.TELEPORT_VERSION` — switch it to read the default
      from the catalog (or hardcode "teleport cluster agent" and show the value
      at runtime by looking it up). The `--version` flag itself stays
      functional, sourcing its default from the catalog instead of a deleted
      constant.

### Tests

- [ ] Unit tests for each updated workflow step verify it reads from the
      catalog and not from `deps.go`. (Most steps already have unit tests using
      the helm mock — extend those.)
- [ ] Unit tests for the new pull-and-verify helper cover: checksum match,
      checksum mismatch (must abort), classic vs OCI code paths, missing
      catalog entry.
- [ ] Integration test (in the UTM VM) for at least one classic and one OCI
      chart confirms a real install succeeds end-to-end with verification.
- [ ] Negative integration test: tamper with the embedded checksum, confirm the
      step aborts with the expected error message (or, if patching the
      embedded YAML is hard, simulate the mismatch in a unit-level test with
      a fake catalog).

## Out of scope (this issue)

- **Removing the teleport `cluster install --version` flag.** Operator-visible
  change; deserves its own issue.
- **Topology constants in `deps.go`** (`*_NAMESPACE`, `*_RELEASE`, `*_CHART`,
  `*_REPO`) — separate follow-up.
- **Hidden emergency override** `--override-component <name>=<version>` —
  separate follow-up.
- **Package manifest (`infrastructure-versions.yaml`) consumption** — the
  apply-time validation against catalog defaults is its own issue.
- **Air-gap / bundled chart delivery** — separate follow-up.
- **Block node / consensus node** versioning.

## Acceptance criteria

- [ ] No file under `internal/workflows/steps/` references `deps.ALLOY_VERSION`,
      `deps.METALLB_VERSION`, `deps.METRICS_SERVER_VERSION`,
      `deps.NODE_EXPORTER_VERSION`, `deps.PROMETHEUS_OPERATOR_CRDS_VERSION`,
      `deps.EXTERNAL_SECRETS_VERSION`, or `deps.TELEPORT_VERSION`. All chart
      versions come from `*ComponentCatalog`.
- [ ] All seven chart version constants are removed from `pkg/deps/deps.go`.
- [ ] Every Helm install/upgrade in the workflow steps passes through the
      pull-and-verify helper; checksum mismatch aborts the step.
- [ ] `teleport cluster install --version` still works; its default value comes
      from the catalog rather than a deleted constant.
- [ ] `task lint`, `task test:unit`, `task vm:test:integration` pass.
- [ ] Manual UAT: a fresh cluster install on the UTM VM succeeds, installing
      Alloy, MetalLB, Metrics Server, Node Exporter, Prometheus Operator CRDs,
      and External Secrets at the versions named in the catalog; logs show
      checksum verification passing for each chart.

## Related work

- **Phase 1 (#587) — `default:` field for binary catalog.** Prerequisite.
- **Phase 2 (#588) — Unified `infrastructure-versions.yaml` with `cluster:` section.** Prerequisite
  — this phase consumes what Phase 2 produces.

## References

- Current code (consumers to migrate):
  - `internal/workflows/steps/step_alloy.go:265, 446`
  - `internal/workflows/steps/step_metallb.go:87`
  - `internal/workflows/steps/step_metrics_server.go:69`
  - `internal/workflows/steps/step_prometheus_operator_crds.go:68`
  - `internal/workflows/steps/step_external_secrets.go:77`
  - Teleport cluster agent step (find via `grep -rn TELEPORT_VERSION internal/`)
  - `cmd/weaver/commands/teleport/cluster/cluster.go:32`
- `pkg/deps/deps.go` — chart version constants to delete; topology constants
  to keep.
- `pkg/helm/` — interface where the pull-and-verify helper should live.
- Helm OCI support: https://helm.sh/docs/topics/registries/
