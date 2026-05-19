# Review guide — #588: rename `artifact.yaml` to `infrastructure-catalog.yaml` and add `cluster:` section

## Summary

Phase 2 of the artifact catalog refactor. The host-only `pkg/software/artifact.yaml`
file is renamed to `pkg/software/infrastructure-catalog.yaml`. The top-level key
`artifact:` is renamed to `host:`, and a new `cluster:` section is introduced for
Helm charts installed into Kubernetes. Seven infrastructure charts are added with
integrity values (SHA256 of `.tgz` for classic charts, OCI manifest digest for
OCI charts).

The `-catalog` suffix is deliberate: the CN release package ships a
separate `manifests/infrastructure-versions.yaml` audit manifest with a
minimal (`name`+`version`) schema that the provisioner cross-checks
against this catalog at apply time. A future hidden CLI flag
(`--override-component <name>=<version>` + `--confirm-untested-combination`)
will additionally allow overriding a single component's version, but
only among the versions this catalog already declares. The intentional
filename split (`-catalog` vs `-versions`) avoids any suggestion that
the two files are interchangeable. See the "Future overrides" section
in `docs/dev/chart-checksums.md` for the planned design.

The Go side gains an `InfrastructureCatalog` type containing `Host` and `Cluster`
slices, plus a `ChartMetadata` struct. `LoadArtifactConfig()` is replaced by
`LoadInfrastructureCatalog()`, which validates the schema at load time. No workflow
step is migrated to read from `cluster:` in this PR — that is Phase 3 (#589).

## Changed files

| File | Description |
|------|-------------|
| `pkg/software/artifact.yaml` → `pkg/software/infrastructure-catalog.yaml` | File rename; top-level key `artifact:` becomes `host:`; new `cluster:` section with seven charts and their integrity records. |
| `pkg/software/config.go` | Embed directive updated; introduces `InfrastructureCatalog`, `ChartMetadata`, `ChartType` (`classic`/`oci`); replaces `LoadArtifactConfig()` with `LoadInfrastructureCatalog()`; adds `GetHostArtifact`, `GetClusterComponent`, `HostNames`, `ClusterNames`; adds schema validation and `ChartMetadata.GetDefaultVersion`. Reuses the existing `Checksum` struct for per-version chart integrity. |
| `pkg/software/base_installer.go` | Switches from `LoadArtifactConfig()`/`GetArtifactByName()` to `LoadInfrastructureCatalog()`/`GetHostArtifact()`. |
| `pkg/software/config_test.go` | Renames the collection test to use `InfrastructureCatalog`; adds tests for `GetClusterComponent`, `ChartMetadata.GetDefaultVersion`, chart-level `validate()`, catalog-level `validate()`, and the load path. |
| `pkg/software/config_it_test.go` | Migrates all integration tests to `LoadInfrastructureCatalog` and `Host`; adds `Test_Config_ClusterSection_Integration` covering all seven charts. |
| `scripts/chart-checksums/main.go` + `Taskfile.yaml` (`chart-checksums` target) | New Go helper that loads the catalog via `software.LoadInfrastructureCatalog()`, runs `helm pull` for every (chart, version) pair, and prints the SHA256 of each `.tgz` plus the OCI manifest digest. Sandboxes `HELM_CONFIG_HOME`/`HELM_CACHE_HOME`/`HELM_DATA_HOME` so the developer's global `repositories.yaml` is untouched, uses `helm repo add --force-update`, surfaces helm errors, and rejects catalog entries that share an alias but disagree on the repo URL. Exposed as `task chart-checksums`. |
| `docs/dev/chart-checksums.md` | New doc describing the integrity model, how to recompute values, and the schema invariants the loader enforces. |

## Code review checklist

- [ ] `pkg/software/artifact.yaml` is **deleted** (no transitional alias).
- [ ] `pkg/software/infrastructure-catalog.yaml` exists with top-level keys `host:` and `cluster:` only.
- [ ] All seven infra charts are present under `cluster:`: `alloy` (1.4.0), `teleport-cluster-agent` (18.6.4), `metallb` (0.15.2), `metrics-server` (3.13.0), `external-secrets` (0.20.2), `node-exporter` (4.5.19), `prometheus-operator-crds` (24.0.1).
- [ ] Each `cluster:` entry declares `type:` (`classic` or `oci`), `chart:`, `default:`, and at least one `versions:` record.
- [ ] Classic charts declare `repo:`; OCI charts do not.
- [ ] The Go `//go:embed` directive points to the new file name.
- [ ] `LoadInfrastructureCatalog()` returns `(*InfrastructureCatalog, error)` and validates the catalog before returning.
- [ ] Validation errors out on: missing `default:` on either section; `default:` pointing to an unknown version; missing/unknown `type:`; `classic` without `repo:`; `oci` with `repo:`; missing `chart:`; empty `versions:`; empty `algorithm:` or `checksum:` for any version.
- [ ] Validation checks run from most fundamental to most derived (type/repo → chart → versions presence → per-version integrity → default resolution), so a chart missing both `chart:` and `default:` reports the chart error first.
- [ ] Per-version iteration sorts version keys alphabetically before checking, so when multiple versions are malformed the same one is named on every run (Go map iteration is randomized).
- [ ] `ChartMetadata.GetDefaultVersion()` mirrors `ArtifactMetadata.GetDefaultVersion()` (explicit default, errors on missing/unknown).
- [ ] No workflow step under `internal/workflows/steps/` reads from `cluster:` yet — Phase 3 (#589) does that wiring.
- [ ] No version constants are deleted from `pkg/deps/deps.go` yet — also Phase 3.
- [ ] All previous `LoadArtifactConfig()` callers are migrated to `LoadInfrastructureCatalog()`.

### `task chart-checksums` helper

- [ ] Loads the catalog via `software.LoadInfrastructureCatalog()` so the chart/version list always matches the YAML (no hardcoded duplicates).
- [ ] Sandboxes Helm: sets `HELM_CONFIG_HOME`, `HELM_CACHE_HOME`, `HELM_DATA_HOME`, `HELM_REPOSITORY_CONFIG`, `HELM_REPOSITORY_CACHE` to subdirectories under a per-run temp dir. The developer's global `repositories.yaml` is left byte-identical after a run.
- [ ] Calls `helm repo add --force-update` and surfaces `helm` exit codes (combined output is included in the returned error) — no silent failures.
- [ ] Detects catalog-level alias conflicts (same first-segment alias with different `repo:` URLs) and fails before any network call.
- [ ] Derives the pulled `.tgz` filename via `path.Base(chartRef)` so chart references of any depth produce the right path.

## Tests

Unit tests for the loader and catalog APIs (macOS or VM):

```bash
go test -race -tags='!integration' ./pkg/software/... -run '^Test_Config_'
```

Integration tests that exercise the embedded YAML (no cluster required):

```bash
go test -tags='integration' ./pkg/software/... -run '^Test_Config_'
```

Full unit suite (CLAUDE.md notes that the full suite needs the UTM VM for Linux-only paths):

```bash
task vm:test:unit
```

Full integration suite in the VM:

```bash
task vm:test:integration
```

## Manual UAT

These steps verify that the catalog loads, the schema validator runs, and that
no behaviour has changed at install time (workflow steps still consume the
constants in `pkg/deps/deps.go`).

1. **Build the binary**

   ```bash
   task build:weaver GOOS=linux GOARCH=amd64
   ```

   Expect: `bin/solo-provisioner-linux-amd64` is produced without errors.

2. **Smoke-test the catalog loader** from a one-off Go program (run from the repo root):

   ```bash
   cat > /tmp/load_catalog.go <<'EOF'
   package main

   import (
       "fmt"

       "github.com/hashgraph/solo-weaver/pkg/software"
   )

   func main() {
       cat, err := software.LoadInfrastructureCatalog()
       if err != nil {
           fmt.Println("ERROR:", err)
           return
       }
       fmt.Println("host:   ", cat.HostNames())
       fmt.Println("cluster:", cat.ClusterNames())
   }
   EOF
   go run /tmp/load_catalog.go
   ```

   Expected output (order may vary):

   ```
   host:    [cri-o kubelet kubeadm kubectl helm k9s cilium teleport]
   cluster: [alloy teleport-cluster-agent metallb metrics-server external-secrets node-exporter prometheus-operator-crds]
   ```

3. **Verify the chart checksum helper reproduces committed values**:

   ```bash
   task chart-checksums
   ```

   Expected: every printed digest matches the `checksum:` value committed
   under the corresponding chart's `versions:` entry in
   `pkg/software/infrastructure-catalog.yaml`.

4. **No-op behaviour check** — run a clean cluster install on the VM and
   confirm that the seven infra charts still install with the same versions as
   before:

   ```bash
   task vm:test:integration TEST_NAME='^Test_StepKubeadm_Fresh_Integration$'
   ```

   Expected: integration tests pass with no version drift. Because Phase 3
   has not yet rewired the Helm install steps to read from `cluster:`, the
   installer continues to consume `pkg/deps/deps.go` constants and produces
   the same result as before this PR.
