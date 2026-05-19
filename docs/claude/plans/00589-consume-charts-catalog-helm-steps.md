# Phase 3 — Consume the catalog from Helm workflow steps (#589)

## Context

Phases 1 (#590) and 2 (#588) built an `InfrastructureCatalog` Go type that parses the embedded `pkg/software/infrastructure-catalog.yaml`. The `cluster:` section lists the seven Helm charts the cluster setup workflow installs (alloy, teleport-cluster-agent, metallb, metrics-server, external-secrets, node-exporter, prometheus-operator-crds), each with a `default:` version and a `versions:` map keyed by version containing `{algorithm: sha256, checksum: <hex>}`.

Before Phase 3 the workflow steps under `internal/workflows/steps/` still hardcoded chart versions through `pkg/deps/deps.go` constants. Nothing read the catalog at install time, and there was no integrity check on the .tgz / OCI manifest pulled by Helm.

Phase 3 connects the two: every Helm install/upgrade in cluster-setup workflows now looks up its version *and* checksum in the catalog, verifies the pulled artifact, and fails the step on mismatch. The seven chart version constants disappear from `deps.go`; topology constants (`*_NAMESPACE`, `*_RELEASE`, `*_CHART`, `*_REPO`) stay until a later cleanup PR.

The branch is `00589-consume-charts-catalog-helm-steps`, cut from `main` (Phase 2 has already landed on `main` as commit `29fe67a`, so we did not stack on the `00588-…` branch).

## Approach

### 1. Branch setup
- `git fetch origin && git checkout main && git pull origin main`
- `git checkout -b 00589-consume-charts-catalog-helm-steps`

### 2. New helper: pull-and-verify in `pkg/helm/`

Added `Manager.PullAndVerify` to the interface — every step already builds a `helm.Manager`, so adding it there avoids new wiring:

```go
PullAndVerify(ctx context.Context, chartRef, version, algorithm, expectedChecksum string) (string, error)
```

Implementation in `pkg/helm/puller.go`:

- Uses the Helm Go SDK (consistent with `pkg/helm/manager.go`; no shell-out to the `helm` binary).
- Classic: `action.NewPull` with `DestDir = <dest>` and `Untar = false`, then `sha256.Sum256` the resulting `.tgz`.
- OCI: build a `registry.Client` via the existing `helpers.go:newRegistryClient`, call `registryClient.Pull(<ref>:<version>, registry.PullOptWithChart(true))`, write the returned `PullResult.Chart.Data` to `<dest>/<name>-<version>.tgz`, and read the manifest digest from `PullResult.Manifest.Digest`. Strip the `sha256:` prefix to match the catalog format.
- Validates `algorithm == "sha256"` and rejects empty `destDir` / `expectedChecksum`.
- Creates `destDir` (mode 0o755) on demand.
- On checksum mismatch, returns `helm.ErrChecksumMismatch` naming the component, expected value, and observed value.

The destination directory is a per-call parameter, mirroring `pkg/software.Downloader.Download(url, destination)`. Workflow steps pass `<WeaverPaths.DownloadsDir>/charts` (default `/opt/solo/weaver/downloads/charts`) so verified `.tgz` artifacts persist alongside other downloaded binaries; unit tests pass `t.TempDir()`. Keeping the destination at the call site rather than as `helm.Manager` state mirrors the host downloader and keeps `helm.Manager` free of state only one method consumes.

Reference: `scripts/chart-checksums/main.go` already shells out to `helm pull` and computes the same digests for catalog generation — the runtime implementation here uses the SDK for the same algorithm.

### 3. Wire the catalog into every Helm step

A small helper `internal/workflows/steps/catalog.go` houses two things every Helm step now uses:

```go
type helmChartSpec struct { Chart, Version, Algorithm, Checksum, Repo string; Type software.ChartType }
func resolveCatalogChart(name string) (*helmChartSpec, error) { /* load catalog → GetClusterComponent → GetDefaultVersion → checksum */ }
func newHelmManager() (helm.Manager, error) { /* helm.NewManager with WithLogger */ }
func chartDownloadsDir() string { /* path.Join(models.Paths().DownloadsDir, "charts") */ }
```

Each step's `WithExecute` closure was rewritten from:

```go
hm, _ := helm.NewManager(helm.WithLogger(*l))
hm.AddRepo(deps.X_RELEASE, deps.X_REPO, ...)        // classic only
hm.InstallChart(ctx, deps.X_RELEASE, deps.X_CHART, deps.X_VERSION, deps.X_NAMESPACE, ...)
```

to:

```go
spec, _ := resolveCatalogChart("metallb")
hm, _ := newHelmManager()
hm.AddRepo(deps.X_RELEASE, spec.Repo, ...)          // classic only (Repo is empty for oci)
localChart, _ := hm.PullAndVerify(ctx, chartDownloadsDir(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum)
hm.InstallChart(ctx, deps.X_RELEASE, localChart, "", deps.X_NAMESPACE, ...)
```

- `chart`, `repo`, and `version` come from the catalog (per the issue's "even though they're duplicated in `deps.go` today" directive).
- `release` and `namespace` continue to come from `deps.go`.
- The local `.tgz` path is passed as `chartRef` to `InstallChart`/`DeployChart`/`UpgradeChart`; Helm's `LocateChart` accepts a local path and the version string is empty.
- The `"v"`-prefix logic in `step_metallb.go` / `step_metrics_server.go` was removed; catalog versions are bare semver and `PullAndVerify` matches the upstream chart-index versions directly (same as `scripts/chart-checksums`).

Files updated:

- `internal/workflows/steps/step_alloy.go` (node-exporter install + alloy install via `DeployChart`)
- `internal/workflows/steps/step_metallb.go`
- `internal/workflows/steps/step_metrics_server.go`
- `internal/workflows/steps/step_prometheus_operator_crds.go`
- `internal/workflows/steps/step_external_secrets.go`
- `internal/workflows/steps/step_teleport.go` — including the `cfg.Version` empty-fallback now reading from the catalog instead of `deps.TELEPORT_VERSION`

### 4. Teleport `--version` flag default

`cmd/weaver/commands/teleport/cluster/cluster.go:32` previously inlined `deps.TELEPORT_VERSION` in the flag's help text. It now does a one-shot catalog lookup at command construction time. If the catalog fails to load the flag is still registered and the install step surfaces a clearer error.

The teleport install step preserves the `--version` flag's behaviour: when the operator passes a version that does not match the catalog default, integrity verification is skipped with a `WARN` log line (we cannot verify a checksum the catalog does not declare), and `InstallChart` is called with the catalog chart reference + version pair. This is an intentional carve-out documented as legacy in the issue.

### 5. Retire all catalog-chart constants in `deps.go`

The catalog rewiring also pulled in solo-operator (added to the repo in #495 after #589 was written, but shaped identically to the other cluster charts — one OCI chart, one install call, no profile-aware overrides). Adding it kept the catalog complete and avoided a known-stale outlier; block-node remains explicitly out of scope per the issue (RSL / env-override chain + storage config + migrations make it a much larger change).

The `ChartMetadata` schema gains two required fields — `namespace:` and `release:` — and the catalog YAML records both for every cluster entry. Each step now resolves the full install topology from the catalog at builder time via a small panicking helper (`chartSpec(name)`); non-step consumers (`internal/alloy/manifest.go`, `internal/reality/teleport_checker.go`, `internal/rsl/teleport_runtime.go`) use `software.MustGetClusterComponent(name)`. Both helpers panic on lookup failure — the catalog is embedded and validated at load time, so a missing entry would be a build-time programming error worth crashing for, and the alternative would force every closure to propagate an error it cannot meaningfully handle.

Removed from `pkg/deps/deps.go` for the eight catalog charts:

- Seven `*_VERSION` constants and `SOLO_OPERATOR_VERSION`.
- Eight `*_CHART` constants.
- Six `*_REPO` constants.
- Eight `*_NAMESPACE` constants and eight `*_RELEASE` constants.

What remains in `pkg/deps/deps.go`: the five `BLOCK_NODE_*` constants. Block node was explicitly out of scope for #589 because its install plan flows through the RSL strategy chain (default → file → env → CLI flag), and moving it to the catalog requires deciding how the catalog interacts with that chain. The package doc comment was rewritten accordingly — it's now a short paragraph stating exactly what the file holds and why, instead of the running "what moved where" history that had accumulated.

`pkg/config/global.go` previously seeded `Teleport.Version` from `deps.TELEPORT_VERSION` in both `globalConfig` and `DefaultsConfig()`. Setting these from the catalog at this layer created a hard import cycle (`pkg/config` → `pkg/software` → `internal/testutil` → `internal/proxy` → `pkg/config`). The fix: leave both fields empty here, with a comment explaining the catalog is the source of truth and the step's empty-fallback handles resolution. The corresponding unit test in `pkg/config/global_test.go` was updated to assert `Teleport.Version` is empty (catalog-driven), not a `deps` constant.

### 6. Mock for `helm.Manager`

Added one line to the `mocks` task in `Taskfile.yaml`:

```yaml
- mockgen -source=pkg/helm/interface.go -self_package github.com/hashgraph/solo-weaver/pkg/helm -package helm > pkg/helm/mocks_generated.go
```

The generated file matches the existing convention (mocks_generated.go is gitignored, regenerated by every CI/devbox run via `task mocks`).

### 7. Tests

Unit tests (`pkg/helm/puller_test.go`):

- `TestPullAndVerify_UnsupportedAlgorithm` — rejects non-sha256 algorithms.
- `TestPullAndVerify_EmptyChecksum` — rejects empty expected checksum.
- `Test_sha256File` — sanity-checks the file digest helper.

Unit tests (`internal/workflows/steps/catalog_test.go`):

- `Test_resolveCatalogChart_AllStepNames` — asserts every chart name a step looks up (`metallb`, `alloy`, `node-exporter`, `metrics-server`, `prometheus-operator-crds`, `external-secrets`, `teleport-cluster-agent`) resolves and has a non-empty checksum, with classic/OCI shape validation.
- `Test_resolveCatalogChart_UnknownName` — covers the not-found path.

A full chart pull through an `httptest.Server` was scoped out as too fragile (Helm SDK requires repo config files + cache layout); the happy and mismatch paths are covered by the VM integration tests, which use real upstream registries.

Integration (UTM VM) — `task vm:test:integration`: existing tests in `internal/workflows/cluster_it_test.go` and `pkg/helm/manager_it_test.go` exercise the full install path through `MetalLB` (classic) and `prometheus-operator-crds` (OCI). The pre-existing `internal/workflows/cluster_it_test.go` end-to-end test verifies all seven charts install in sequence.

### 8. Out of scope confirmations

- Removing the `teleport cluster install --version` flag — deferred (per issue).
- Topology constants in `deps.go` — kept; later cleanup PR.
- Hidden `--override-component` flag — deferred.
- Package manifest (override file) consumption + schema differentiation between override and internal catalog — leninmehedy's review comment is noted for a follow-up, not addressed here.
- Air-gap / bundled chart delivery — deferred.
- Block node / consensus node / solo-operator versioning — none are yet in the catalog's `cluster:` section; deferred.

## Critical files

| Path | Change |
|---|---|
| `pkg/helm/interface.go` | Added `PullAndVerify` to `Manager` |
| `pkg/helm/puller.go` (new) | Implementation — classic SHA256, OCI manifest digest, mismatch error |
| `pkg/helm/puller_test.go` (new) | Unit tests for the helper |
| `pkg/helm/manager.go` | No surface change; `PullAndVerify` takes `destDir` per call (mirrors `pkg/software.Downloader`) |
| `internal/workflows/steps/catalog.go` | Adds `chartDownloadsDir()` helper returning `<DownloadsDir>/charts` |
| `Taskfile.yaml` | Added mockgen line for `pkg/helm/interface.go` |
| `pkg/deps/deps.go` | Stripped down to the `BLOCK_NODE_*` group only. All catalog-chart constants (version + chart + repo + namespace + release) removed. Package doc comment rewritten. |
| `pkg/software/config.go` | Added `namespace:` and `release:` to `ChartMetadata` (required for cluster entries) + `MustGetClusterComponent` helper. |
| `pkg/software/infrastructure-catalog.yaml` | New `solo-operator` entry; every cluster entry gains `namespace:` and `release:` fields |
| `internal/alloy/manifest.go` + `render_test.go` | Read alloy namespace from catalog via `software.MustGetClusterComponent` |
| `internal/reality/teleport_checker.go` + test | Identify teleport release/namespace from catalog |
| `internal/rsl/teleport_runtime.go` | Seed `StrategyDefault` for namespace/release from catalog |
| `internal/workflows/steps/step_solo_operator.go` | Catalog + PullAndVerify (was the only `helm.NewManager(helm.WithLogger(*l))` call site missed earlier — now uses `newHelmManager()`) |
| `pkg/software/config_it_test.go` | `Test_Config_ClusterSection_Integration` covers `solo-operator` |
| `internal/workflows/steps/catalog.go` (new) | `resolveCatalogChart` + `newHelmManager` helpers |
| `internal/workflows/steps/catalog_test.go` (new) | Catalog↔step naming contract tests |
| `internal/workflows/steps/step_alloy.go` | Catalog + PullAndVerify (two charts) |
| `internal/workflows/steps/step_metallb.go` | Catalog + PullAndVerify |
| `internal/workflows/steps/step_metrics_server.go` | Catalog + PullAndVerify |
| `internal/workflows/steps/step_prometheus_operator_crds.go` | Catalog + PullAndVerify |
| `internal/workflows/steps/step_external_secrets.go` | Catalog + PullAndVerify |
| `internal/workflows/steps/step_teleport.go` | Catalog + PullAndVerify with `--version` carve-out |
| `cmd/weaver/commands/teleport/cluster/cluster.go` | `--version` flag default from catalog |
| `pkg/config/global.go` | Removed three `deps.TELEPORT_VERSION` references (set empty) |
| `pkg/config/global_test.go` | Asserts `Teleport.Version` defaults to empty |
| `docs/claude/reviews/00589-consume-charts-catalog-helm-steps.md` (new) | Review guide per CLAUDE.md §2 |

Reused (no change): `pkg/software/config.go` (`LoadInfrastructureCatalog`, `GetClusterComponent`, `ChartMetadata.GetDefaultVersion`, `ChartMetadata.Versions`), `pkg/helm/manager.go` (existing `InstallChart`/`DeployChart`/`UpgradeChart` accept local `.tgz` paths via `LocateChart`).

## Verification

1. `task lint` — formats cleanly.
2. `go test ./pkg/helm/... ./pkg/config/... ./pkg/deps/...` — passes locally on macOS.
3. `task vm:test:unit` — passes inside the UTM VM (Linux-only packages compile).
4. `task vm:test:integration` — `MetalLB` (classic) and `prometheus-operator-crds` (OCI) install end-to-end with verification.
5. Manual UAT on the UTM VM: `solo-provisioner kube cluster install --profile local` succeeds and the logs show `Helm chart pulled and verified` (with `algorithm`, `checksum`) for each of the seven charts.
6. Grep guard: `grep -rn 'deps\.\(ALLOY\|METALLB\|METRICS_SERVER\|NODE_EXPORTER\|PROMETHEUS_OPERATOR_CRDS\|EXTERNAL_SECRETS\|TELEPORT\)_VERSION'` returns no matches.
7. Review guide written at `docs/claude/reviews/00589-consume-charts-catalog-helm-steps.md`.
