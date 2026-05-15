# Phase 2 (#588) — Rename to `infrastructure-versions.yaml`; add `host:` + `cluster:` sections with Helm chart entries

> **Suggested title for GH issue:**
> `refactor(software): rename artifact.yaml to infrastructure-versions.yaml and add cluster: section with infra Helm charts`
>
> **Related phases:** #587 (`default:` field for binary catalog) · #588 (this) · #589 (wire Helm steps + retire `deps.go` versions)

## Summary

This phase performs the structural rename and adds the seven infra Helm charts
to the catalog with integrity metadata. It builds on **Phase 1** (which
introduced the `default:` field).

**No workflow steps are migrated in this phase** — Phase 3 wires the Helm
install paths to read from the new `cluster:` section and adds checksum/digest
verification.

## Problem

After Phase 1, `pkg/software/artifact.yaml` carries explicit defaults for
binary artifacts, but:

1. The file name (`artifact.yaml`) and top-level key (`artifact:`) describe
   *how* a component is packaged rather than *where* it runs. The package
   manifest vocabulary (`host:` / `cluster:`) is semantically superior — it
   stays correct even if a binary is later delivered as a chart.
2. The seven infra Helm charts (Alloy, Teleport cluster agent, MetalLB,
   Metrics Server, Node Exporter, Prometheus Operator CRDs, External Secrets)
   are not in the catalog at all. Their versions are hardcoded as Go constants
   in `pkg/deps/deps.go`, and there is no integrity verification on chart pulls.

## Decisions

| Question | Decision |
|---|---|
| Catalog structure | Unified `infrastructure-versions.yaml` with `host:` (binaries) and `cluster:` (Helm charts) sections |
| Integrity model | One `algorithm` + `checksum` per version per chart — SHA256 of `.tgz` for classic repos, OCI manifest digest for OCI registries |
| Template variables in `cluster:` | None — charts are platform-agnostic; `repo` and `chart` are static strings |
| Chart sub-schema | No `archives` / `binaries` / `configs` nesting — each chart is one artifact |
| `type:` field | Explicit (`classic` \| `oci`) rather than inferred from `chart:` prefix |

## Scope

### YAML migration

- [ ] Rename `pkg/software/artifact.yaml` → `pkg/software/infrastructure-versions.yaml`.
- [ ] Rename top-level key `artifact:` → `host:` (no transitional alias; same
      PR, full sweep).
- [ ] Add a `cluster:` section with one entry per infra chart:

```yaml
cluster:
  - name: alloy
    type: classic
    repo: https://grafana.github.io/helm-charts
    chart: grafana/alloy
    default: "1.4.0"
    versions:
      1.4.0:
        algorithm: sha256
        checksum: '<hex>'                  # SHA256 of .tgz archive

  - name: node-exporter
    type: oci
    chart: oci://registry-1.docker.io/bitnamicharts/node-exporter
    default: "4.5.19"
    versions:
      4.5.19:
        algorithm: sha256
        checksum: '<hex>'                  # OCI manifest digest

  - name: prometheus-operator-crds
    type: oci
    chart: oci://ghcr.io/prometheus-community/charts/prometheus-operator-crds
    default: "24.0.1"
    versions:
      24.0.1:
        algorithm: sha256
        checksum: '<hex>'

  - name: teleport-cluster-agent
    type: classic
    repo: https://charts.releases.teleport.dev
    chart: teleport/teleport-kube-agent
    default: "18.6.4"
    versions:
      18.6.4:
        algorithm: sha256
        checksum: '<hex>'

  - name: metallb
    type: classic
    repo: https://metallb.github.io/metallb
    chart: metallb/metallb
    default: "0.15.2"
    versions:
      0.15.2:
        algorithm: sha256
        checksum: '<hex>'

  - name: metrics-server
    type: classic
    repo: https://kubernetes-sigs.github.io/metrics-server
    chart: metrics-server/metrics-server
    default: "3.13.0"
    versions:
      3.13.0:
        algorithm: sha256
        checksum: '<hex>'

  - name: external-secrets
    type: classic
    repo: https://charts.external-secrets.io
    chart: external-secrets/external-secrets
    default: "0.20.2"
    versions:
      0.20.2:
        algorithm: sha256
        checksum: '<hex>'
```

### Go changes

- [ ] Update the `//go:embed` directive in `pkg/software/` to embed
      `infrastructure-versions.yaml`.
- [ ] Introduce `ChartMetadata` struct with fields: `Name`, `Type`
      (`classic` | `oci`), `Repo` (optional, classic only), `Chart`, `Default`,
      `Versions` (map of version → `{algorithm, checksum}`).
- [ ] Introduce `ComponentCatalog` container type:

```go
type ComponentCatalog struct {
    Host   []ArtifactMetadata `yaml:"host"`
    Cluster []ChartMetadata  `yaml:"cluster"`
}
```

- [ ] Replace `LoadArtifactConfig()` with `LoadComponentCatalog()` returning
      `*ComponentCatalog`. Migrate every caller.
- [ ] Add a `GetClusterComponent(name string) (*ChartMetadata, error)` lookup
      on the catalog.
- [ ] Add `(c *ChartMetadata) GetDefaultVersion() (string, error)` mirroring
      Phase 1's `ArtifactMetadata.GetDefaultVersion()`.
- [ ] Schema validation at load time:
      - `host:` and `cluster:` entries must each have a `default:` that points
        to a version present in `versions:`.
      - `cluster:` entries must have an explicit `type` (`classic` or `oci`).
      - `classic` entries must have `repo:`; `oci` entries must not.
      - Every version must have non-empty `algorithm` and `checksum`.

### Checksum seeding

- [ ] Decide who computes the seed `algorithm` + `checksum` values for each
      chart at PR-time. Likely options:
      - One-shot helper script (`hack/compute-chart-checksums.sh`) that runs
        `helm pull` for classic charts and reads the OCI manifest digest for
        OCI charts. Run it locally to generate the seeds.
      - Manual values, with a follow-up CI job in Phase 3 or later.
- [ ] Document the procedure in `docs/dev/` (e.g. `docs/dev/chart-checksums.md`).

## Out of scope (this issue)

- Wiring `internal/workflows/steps/step_alloy.go`, `step_metallb.go`,
  `step_metrics_server.go`, `step_prometheus_operator_crds.go`,
  `step_external_secrets.go`, and the teleport cluster agent step to read
  from `cluster:` (Phase 3).
- Verifying chart checksum/digest before `helm install`/`upgrade` (Phase 3).
- Deleting version constants in `pkg/deps/deps.go` (Phase 3).
- A `schemaVersion:` top-level field. Worth deciding in this PR review as a
  future-facing contract.
- Topology constants (`*_NAMESPACE`, `*_RELEASE`, `*_CHART`, `*_REPO`) — stay
  in `deps.go` for now.
- Block node / consensus node — Hedera software, separate concern.

## Acceptance criteria

- [ ] `pkg/software/infrastructure-versions.yaml` exists; `pkg/software/artifact.yaml` is
      deleted in the same PR.
- [ ] Top-level keys are `host:` and `cluster:`.
- [ ] All seven infra charts are present under `cluster:` with the fields above,
      seeded with real checksum/digest values for the currently shipped versions
      (`1.4.0` Alloy, `18.6.4` Teleport cluster agent, `0.15.2` MetalLB,
      `3.13.0` Metrics Server, `4.5.19` Node Exporter, `24.0.1` Prometheus
      Operator CRDs, `0.20.2` External Secrets).
- [ ] `LoadComponentCatalog()` returns a `*ComponentCatalog`; all former
      `LoadArtifactConfig()` callers compile and pass tests.
- [ ] Schema validation errors on missing `default:`, missing `type:`,
      `classic` without `repo`, `oci` with `repo`, and `default:` pointing to
      an unknown version.
- [ ] Unit tests cover the loader, `ComponentCatalog.GetClusterComponent`,
      `ChartMetadata.GetDefaultVersion`, and all the validation failure modes.
- [ ] `task lint`, `task test:unit`, `task vm:test:integration` pass.
- [ ] **No behaviour change at install time** — Phase 3 is responsible for
      switching workflow steps off `deps.go`. After Phase 2 alone, the new
      catalog exists but is not yet consulted by Helm install paths.

## Related work

- **Phase 1 (#587) — `default:` field for binary catalog.** Prerequisite. Adds
  `ArtifactMetadata.GetDefaultVersion()` and removes implicit-latest selection.
- **Phase 3 (#589) — Wire Helm steps to the catalog; retire version constants in
  `deps.go`.** Consumes the `cluster:` section added by this phase.

## References

- `pkg/software/artifact.yaml` — to be renamed.
- `pkg/software/config.go` — loader and `ArtifactMetadata` type.
- `pkg/deps/deps.go` — current chart version constants and topology constants
  (versions move to `infrastructure-versions.yaml`; topology stays).
- Helm OCI support: https://helm.sh/docs/topics/registries/
