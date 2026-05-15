# Review Guide: 00482 — Install solo-operator via Helm chart

## Summary

**Problem:** `solo-operator` (a Kubernetes operator for Hedera Solo network components)
was not installed as part of the `kube cluster install` workflow in solo-weaver.

**Solution:** Added `solo-operator` as a Helm-based installation step in the
`kubernetesSetupWorkflow`, after MetricsServer and before the cluster health check,
following the same pattern used for MetalLB, MetricsServer, and Node Exporter.
The chart is pulled from the official OCI registry (`oci://ghcr.io/hashgraph/charts/solo-operator`).

The feature is **opt-in** via `soloOperator.enabled: true` in the config file (defaults to
`false`) so existing production systems are unaffected until explicitly enabled.

## Changed Files

| File | Description |
|------|-------------|
| `pkg/deps/deps.go` | Added `SOLO_OPERATOR_*` constants (namespace, release, chart URL, version) |
| `pkg/models/config.go` | Added `SoloOperatorConfig{Enabled bool}` and `SoloOperator` field on `Config` |
| `internal/workflows/steps/step_solo_operator.go` | New step implementing idempotent install and rollback |
| `internal/workflows/cluster.go` | Conditionally wires `InstallSoloOperator()` into `kubernetesSetupWorkflow` when `SoloOperator.Enabled` |
| `internal/workflows/steps/step_health.go` | Conditionally appends solo-operator namespace, pod, and CRDs to health check lists when `SoloOperator.Enabled` |
| `test/config/config_with_solo_operator.yaml` | Test config with `soloOperator.enabled: true` |
| `taskfiles/uat.yaml` | Added `uat:solo-operator` task; included it in `uat:all` |

## Code Review Checklist

- [ ] `SOLO_OPERATOR_CHART` points to the correct OCI registry (`oci://ghcr.io/hashgraph/charts/solo-operator`)
- [ ] `SOLO_OPERATOR_VERSION` matches the latest published chart version (`0.3.1`)
- [ ] `InstallSoloOperator` checks `IsInstalled` before installing (idempotent)
- [ ] Rollback only runs if `InstalledByThisStep` is true (does not uninstall pre-existing releases)
- [ ] Step is conditionally added — not present in workflow when `SoloOperator.Enabled = false`
- [ ] Health check lists only include solo-operator entries when `SoloOperator.Enabled = true`
- [ ] Default value of `SoloOperator.Enabled` is `false` (zero value of bool)
- [ ] No `AddRepo` call — OCI charts do not require repo registration
- [ ] SPDX license header present in new file

## Local Dev Testing

To test solo-operator installation in a local UTM VM without running the full UAT suite:

1. Build the binary:
   ```bash
   task build:weaver GOOS=linux GOARCH=arm64  # or amd64
   ```

2. Copy to VM and self-install:
   ```bash
   scp bin/solo-provisioner-linux-arm64 vm:/tmp/
   ssh vm 'sudo /tmp/solo-provisioner-linux-arm64 install'
   ```

3. Create a config file with `soloOperator.enabled: true` on the VM:
   ```yaml
   # /mnt/solo-weaver/test/config/config_with_solo_operator.yaml
   log:
     level: debug
     consoleLogging: true
   soloOperator:
     enabled: true
   ```

4. Run cluster install:
   ```bash
   ssh vm 'sudo solo-provisioner kube cluster install -p local -c /mnt/solo-weaver/test/config/config_with_solo_operator.yaml --node-type=block' 
   ```

5. Verify:
   ```bash
   ssh vm 'helm list -n solo-operator'
   # Expected: solo-operator release with STATUS=deployed

   ssh vm 'kubectl get pods -n solo-operator'
   # Expected: solo-operator-controller-manager-* with STATUS=Running

   ssh vm 'kubectl get crds | grep hedera'
   # Expected: six CRDs (consensuscapsules, envoyproxies, haproxycapsules, helmcapsules, networkoperations, orbits)
   ```

6. Re-run to verify idempotency:
   ```bash
   ssh vm 'sudo solo-provisioner kube cluster install -p local -c /mnt/solo-weaver/test/config/config_with_solo_operator.yaml --node-type=block' 
   # Expected: "Solo Operator is already installed, skipping installation" in logs
   ```

7. Verify feature is off by default — run install **without** `soloOperator.enabled`:
   ```bash
   ssh vm 'sudo solo-provisioner kube cluster install -p local'
   # Expected: InstallSoloOperator step absent; health check does not include solo-operator entries
   ```

## Test Commands

```bash
# Unit tests (macOS — packages without Linux-only deps)
task test:unit

# Full unit tests (inside UTM VM)
task vm:test:unit

# License header check
task license:check
```

## UAT Instructions

### Standard lifecycle (solo-operator disabled)

Run inside the UTM VM — solo-operator is off by default, so the standard lifecycle
test is unaffected:

```bash
task uat:lifecycle
```

### solo-operator lifecycle test

Requires a cluster already set up by `uat:setup`. Run:

```bash
task uat:solo-operator
```

This test:
1. Installs the cluster using `test/config/config_with_solo_operator.yaml` (which sets `soloOperator.enabled: true`)
2. Verifies the Helm release status is `deployed`
3. Verifies the controller-manager pod reaches `Ready`
4. Verifies all six CRDs are present
5. Re-runs install to confirm idempotency
6. Verifies the `solo-operator` namespace is present (confirming health check passes)

### Full suite (includes solo-operator)

```bash
task uat:all
```
