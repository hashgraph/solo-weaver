# Review guide — #589: consume catalog for Helm steps, verify chart integrity

## Summary

Phase 3 of the infrastructure-catalog refactor. The six Helm workflow steps under
`internal/workflows/steps/` (alloy + node-exporter, metallb, metrics-server,
prometheus-operator-crds, external-secrets, teleport) now read their chart
version, repo, and reference from `pkg/software/infrastructure-catalog.yaml`
(populated in Phase 2 — #588) instead of the `*_VERSION` constants in
`pkg/deps/deps.go`. The seven chart-version constants are removed.

Each install also pulls the chart through a new `helm.Manager.PullAndVerify`
method which downloads the chart to `<WeaverPaths.DownloadsDir>/charts`
(default `/opt/solo/weaver/downloads/charts`), computes SHA256 of the `.tgz`
(classic) or reads the OCI manifest digest from the registry client (OCI),
and aborts the step on mismatch with the catalog-recorded checksum. The local
`.tgz` path is then passed to `InstallChart`/`DeployChart`/`UpgradeChart` so
there is no second network pull.

The teleport `cluster install --version` flag still works; its default now
comes from the catalog. Integrity verification is skipped (with a WARN log
line) only when the operator explicitly pins a non-catalog version, since the
catalog has no checksum to compare against.

## Changed files

| File | Description |
|------|-------------|
| `pkg/helm/interface.go` | Adds `PullAndVerify(ctx, chartRef, version, algorithm, expectedChecksum) (string, error)` to `Manager`. |
| `pkg/helm/puller.go` (new) | SDK-based implementation: classic via `action.NewPull` + `sha256.Sum256` of the .tgz; OCI via `registry.Client.Pull` + manifest digest. Returns `helm.ErrChecksumMismatch` on mismatch. Takes `destDir` as a parameter and creates it on demand (mode 0o755). |
| `pkg/helm/manager.go` | No surface change. (`PullAndVerify` is a new method on `Manager`; the destination dir is a parameter, not Manager state — mirrors `pkg/software.Downloader.Download(url, destination)`.) |
| `pkg/helm/puller_test.go` (new) | Unit tests for algorithm validation, empty-checksum guard, and the SHA256 helper. |
| `Taskfile.yaml` | Adds `mockgen -source=pkg/helm/interface.go ...` line so step unit tests can mock `helm.Manager`. |
| `pkg/deps/deps.go` | Stripped to the `BLOCK_NODE_*` group only. All eight catalog charts' constants — version, chart, repo, namespace, release — moved to the catalog. Package doc comment rewritten. |
| `pkg/software/config.go` | `ChartMetadata` gains `namespace:` and `release:` (required for cluster entries, validated at load time). New `software.MustGetClusterComponent(name) *ChartMetadata` panics if the catalog can't load or the name isn't present — used by callers outside `internal/workflows/steps/` that read catalog entries by literal name. |
| `pkg/software/infrastructure-catalog.yaml` | New `solo-operator` entry (OCI, `0.3.1`, manifest digest `b77cb6…6a73a`). Every cluster entry now declares `namespace:` and `release:`. Catalog covers all eight cluster charts we install. |
| `internal/workflows/steps/catalog.go` | New `chartSpec(name) *helmChartSpec` panicking helper alongside the existing `resolveCatalogChart`; `helmChartSpec` gains `Namespace`/`Release` fields. |
| `internal/workflows/steps/step_solo_operator.go` | Catalog + PullAndVerify + `newHelmManager()`. |
| `internal/alloy/manifest.go` + `internal/alloy/render_test.go` | Read alloy namespace from the catalog (was `deps.ALLOY_NAMESPACE`). |
| `internal/reality/teleport_checker.go` + test | Resolve teleport release/namespace from the catalog. |
| `internal/rsl/teleport_runtime.go` | `StrategyDefault` for namespace/release sourced from the catalog. |
| `pkg/software/config_it_test.go` | `Test_Config_ClusterSection_Integration` covers `solo-operator` (8 charts). |
| `internal/workflows/steps/catalog.go` (new) | `resolveCatalogChart(name)` resolves the install plan (chart, version, algorithm, checksum, repo, type) from the catalog; `newHelmManager()` constructs `helm.Manager` with the shared downloads-dir option. |
| `internal/workflows/steps/catalog_test.go` (new) | Asserts every chart name a step looks up resolves in the catalog with sha256 + non-empty checksum and the right classic/OCI shape. |
| `internal/workflows/steps/step_alloy.go` | Both node-exporter and alloy install paths read from the catalog and call PullAndVerify. AddRepo for alloy now sources the URL from the catalog. |
| `internal/workflows/steps/step_metallb.go` | Catalog + PullAndVerify; the `"v"` version-prefix workaround is removed (catalog versions match upstream chart-index entries). |
| `internal/workflows/steps/step_metrics_server.go` | Catalog + PullAndVerify; same `"v"` prefix removed. |
| `internal/workflows/steps/step_prometheus_operator_crds.go` | Catalog + PullAndVerify (OCI). |
| `internal/workflows/steps/step_external_secrets.go` | Catalog + PullAndVerify; same `"v"` prefix removed. |
| `internal/workflows/steps/step_teleport.go` | Catalog + PullAndVerify with the `--version` carve-out: if operator-pinned and non-catalog, install proceeds without verification (WARN log). |
| `cmd/weaver/commands/teleport/cluster/cluster.go` | `--version` flag default comes from the catalog (or empty if the catalog fails to load). |
| `pkg/config/global.go` | Removes three `deps.TELEPORT_VERSION` references; `Teleport.Version` defaults to empty (step has a catalog fallback). Avoids the pkg/config → pkg/software → internal/proxy → pkg/config import cycle. |
| `pkg/config/global_test.go` | `TestDefaultsConfig_ReturnsDepsConstants` now asserts `Teleport.Version` is empty (catalog-driven). |
| `docs/claude/plans/00589-consume-charts-catalog-helm-steps.md` (new) | Implementation plan. |

## Code review checklist

- [ ] No file outside this PR's tests references `deps.ALLOY_VERSION`, `deps.METALLB_VERSION`, `deps.METRICS_SERVER_VERSION`, `deps.NODE_EXPORTER_VERSION`, `deps.PROMETHEUS_OPERATOR_CRDS_VERSION`, `deps.EXTERNAL_SECRETS_VERSION`, or `deps.TELEPORT_VERSION` (grep guard — see Tests).
- [ ] `pkg/deps/deps.go` contains only the `BLOCK_NODE_*` group. Every other constant for the eight catalog charts (version, chart, repo, namespace, release) is gone.
- [ ] The package doc comment in `pkg/deps/deps.go` explains both what's there (block-node) and why (RSL strategy chain), nothing else.
- [ ] Every step builder calls `chartSpec("<name>")` at the top of the function and the closures read `spec.Namespace` / `spec.Release` (no `deps.*` references for the eight catalog charts anywhere in `internal/workflows/steps/`).
- [ ] Non-step consumers (`internal/alloy/manifest.go`, `internal/reality/teleport_checker.go`, `internal/rsl/teleport_runtime.go`) use `software.MustGetClusterComponent(name)` rather than `deps.*` constants.
- [ ] `ChartMetadata` schema validation requires non-empty `namespace:` and `release:` for every cluster entry; an entry missing either fails to load.
- [ ] Every Helm `InstallChart`/`DeployChart`/`UpgradeChart` call in the six steps is preceded by `hm.PullAndVerify(...)` and receives the returned local `.tgz` path as `chartRef` (with `chartVersion == ""`).
- [ ] Step→catalog name contract: each step calls `resolveCatalogChart` with the exact name present in `cluster:` (`metallb`, `alloy`, `node-exporter`, `metrics-server`, `prometheus-operator-crds`, `external-secrets`, `teleport-cluster-agent`, `solo-operator`). `Test_resolveCatalogChart_AllStepNames` enforces this.
- [ ] `PullAndVerify` uses the Helm Go SDK (no shell-out to `helm`). Classic computes SHA256 of the pulled `.tgz`; OCI reads `PullResult.Manifest.Digest` and strips the `sha256:` prefix.
- [ ] Mismatch returns `helm.ErrChecksumMismatch` and includes both the expected and observed values in the message.
- [ ] Algorithm other than `sha256` is rejected with a clear error; empty `expectedChecksum` is rejected.
- [ ] Each step passes `chartDownloadsDir()` (`internal/workflows/steps/catalog.go`) as `destDir` so verified `.tgz` artifacts land in `<WeaverPaths.DownloadsDir>/charts`. The destination is a per-call parameter; `helm.Manager` holds no path state. Unit tests pass `t.TempDir()`.
- [ ] `cmd/weaver/commands/teleport/cluster/cluster.go` registers the `--version` flag even when the catalog fails to load (the install step surfaces the catalog error then).
- [ ] Teleport step skips verification only when the operator explicitly pins a version that differs from the catalog default; the WARN log line identifies the pinned version.
- [ ] `pkg/config/global.go` does NOT import `pkg/software` (would create an import cycle via `internal/testutil` → `internal/proxy`). The comment near the Teleport defaults explains why.
- [ ] Generated mock at `pkg/helm/mocks_generated.go` is present after `task mocks` (gitignored).
- [ ] The `"v"`-prefix workaround was removed from the metallb / metrics-server / external-secrets steps. Catalog versions match upstream chart-index entries directly (same convention as `scripts/chart-checksums`).

## Tests

Unit tests on macOS for the packages we changed:

```bash
go test -race -tags='!integration' ./pkg/helm/... ./pkg/config/... ./pkg/deps/...
```

Full unit suite (Linux-only step package needs the UTM VM):

```bash
task vm:test:unit
```

Targeted integration tests for the install path (classic + OCI) in the VM:

```bash
task vm:test:integration TEST_NAME='^Test_SetupMetalLB_Integration$|^Test_PrometheusOperatorCRDs_Integration$'
```

Full integration suite (slow):

```bash
task vm:test:integration
```

Grep guard — must return no matches:

```bash
grep -rn 'deps\.\(ALLOY\|METALLB\|METRICS_SERVER\|NODE_EXPORTER\|PROMETHEUS_OPERATOR_CRDS\|EXTERNAL_SECRETS\|TELEPORT\)_VERSION' --include='*.go'
```

Lint:

```bash
task lint
```

## Manual UAT

1. **Build the binary**

   ```bash
   task build:weaver GOOS=linux GOARCH=amd64
   ```

   Expected: `bin/solo-provisioner-linux-amd64` is produced without errors.

2. **Inspect the `--version` flag default for teleport cluster**

   ```bash
   ./bin/solo-provisioner-linux-amd64 teleport cluster install --help | grep version
   ```

   Expected:

   ```
       --version string   Teleport Helm chart version (default: 18.6.4)
   ```

   The `18.6.4` value is read from `pkg/software/infrastructure-catalog.yaml` at runtime.

3. **Fresh cluster install on the VM**

   ```bash
   task vm:start
   ssh weaver-vm sudo /usr/local/bin/solo-provisioner kube cluster install --profile local --node-type=block --non-interactive
   ```

   Expected: the install succeeds and the logs include one `Helm chart pulled and verified` line per chart with the matching algorithm and checksum, e.g.:

   ```
   INF Helm chart pulled and verified algorithm=sha256 chart=metallb/metallb checksum=4f0fc313cd97819a1c78fff0a389dfe1c9f98bc490f33f521317475df1d7b873 version=0.15.2
   ```

4. **Inspect persisted artifacts**

   ```bash
   ssh weaver-vm ls -la /opt/solo/weaver/downloads/charts
   ```

   Expected: every installed chart appears as `<name>-<version>.tgz` (e.g. `metallb-0.15.2.tgz`, `prometheus-operator-crds-24.0.1.tgz`).

5. **Negative path — tampered checksum** (optional manual check)

   Edit one byte of the `checksum:` value for a chart in `pkg/software/infrastructure-catalog.yaml`, rebuild, run install:

   ```bash
   task build:weaver GOOS=linux GOARCH=amd64
   ssh weaver-vm sudo /usr/local/bin/solo-provisioner kube cluster install --profile local
   ```

   Expected: the step aborts with `helm.checksum_mismatch ... expected sha256=<patched> got sha256=<real>` and the cluster is left in a clean state. Revert the catalog edit afterward.

6. **Teleport pinned version (legacy carve-out)**

   ```bash
   ssh weaver-vm sudo /usr/local/bin/solo-provisioner teleport cluster install --version 18.6.3 --values /etc/weaver/teleport.yaml
   ```

   Expected: the install proceeds without verification and the log contains `Skipping chart integrity verification for operator-pinned Teleport version`.

7. **Default version (catalog-driven) still works**

   ```bash
   ssh weaver-vm sudo /usr/local/bin/solo-provisioner teleport cluster install --values /mnt/solo-weaver/test/teleport/teleport-values-local.yaml --non-interactive
   ```

   Expected: same as #3 but for teleport — a `Helm chart pulled and verified` line shows `version=18.6.4` and the catalog checksum.

8. **Cluster uninstall still cleans up**

   ```bash
   ssh weaver-vm sudo /usr/local/bin/solo-provisioner kube cluster uninstall --profile local
   ```

   Expected: no errors. Uninstall paths still use `deps.X_RELEASE` / `deps.X_NAMESPACE`, which were not touched.
