# Plan: 00482 — Install solo-operator via Helm chart

## Context

`solo-operator` is a Kubernetes operator that manages Hedera Solo network components via CRDs
(Orbits, ConsensusCapsules, NetworkOperations, HaproxyCapsules, EnvoyProxies, HelmCapsules).
It publishes an official Helm chart to `oci://ghcr.io/hashgraph/charts/solo-operator`.
solo-weaver needs to install it as part of the `kube cluster install` workflow, after the
cluster is initialized and MetricsServer is up, using the same Helm install pattern already
used for MetalLB, MetricsServer, and Node Exporter.

**GitHub issue:** [#482](https://github.com/hashgraph/solo-weaver/issues/482)
**Branch:** `00482-install-solo-operator`

## Decisions

- **Placement:** Step added to `kubernetesSetupWorkflow` (inside `kube cluster install`),
  not as a standalone command — solo-operator is a cluster-level prerequisite.
- **Uninstall:** Rollback support included in the install step (uninstalls if the step itself
  was responsible for installing). No explicit step in `UninstallClusterWorkflow` needed —
  `kubeadm reset` in `ResetCluster()` tears down all resources, matching existing pattern.
- **No repo add:** The chart is OCI-based, same as `BLOCK_NODE_CHART` and
  `PROMETHEUS_OPERATOR_CRDS_CHART` — no `hm.AddRepo()` call required.
- **No custom values:** Chart defaults are sufficient for initial install.

## Files Changed

| File | Change |
|------|--------|
| `pkg/deps/deps.go` | Add `SOLO_OPERATOR_*` constants |
| `internal/workflows/steps/step_solo_operator.go` | New step (install + rollback) |
| `internal/workflows/cluster.go` | Wire step into `kubernetesSetupWorkflow` |

## Implementation

### 1. `pkg/deps/deps.go`

```go
// Solo Operator
SOLO_OPERATOR_NAMESPACE = "solo-operator"
SOLO_OPERATOR_RELEASE   = "solo-operator"
SOLO_OPERATOR_CHART     = "oci://ghcr.io/hashgraph/charts/solo-operator"
SOLO_OPERATOR_VERSION   = "0.3.1"
```

### 2. `internal/workflows/steps/step_solo_operator.go`

Follows the Node Exporter pattern (`step_alloy.go`):

- `InstallSoloOperatorStepId = "install-solo-operator"`
- `InstallSoloOperator() automa.Builder`
  - `WithExecute`: idempotency guard via `hm.IsInstalled()` → `hm.InstallChart()` with
    `CreateNamespace: true`, `Atomic: true`, `Wait: true`, `Timeout: helm.DefaultTimeout`
  - `WithRollback`: skip if not installed by this step; otherwise `hm.UninstallChart()`
  - `WithPrepare/WithOnFailure/WithOnCompletion`: TUI notifications

### 3. `internal/workflows/cluster.go`

```go
steps.DeployMetricsServer(nil),
steps.InstallSoloOperator(),   // added
steps.CheckClusterHealth(),
```

## Verification

```bash
# Format
task lint

# Unit tests (UTM VM for full coverage)
task vm:test:unit

# License headers
task license:check

# Manual UAT (UTM VM)
sudo solo-provisioner kube cluster install
helm list -n solo-operator   # expect: deployed solo-operator-0.3.1
kubectl get pods -n solo-operator   # expect: Running
```
