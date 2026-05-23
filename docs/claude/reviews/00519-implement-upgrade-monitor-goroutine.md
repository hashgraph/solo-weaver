# Review Guide — #519 Implement UpgradeMonitor goroutine

## Problem & Solution

The solo-provisioner-daemon needed a goroutine to watch `NetworkUpgradeExecute` CRs
in Kubernetes and trigger the execute-phase workflow when one transitions to the
`ReadyForProvisionerDaemon` phase. This story wires up that watcher as a self-healing
goroutine that survives transient K8s API errors and kubeconfig rotation without a
daemon restart.

## Changed Files

| File | Description |
|------|--------------|
| `internal/daemon/consensus/upgrade_monitor.go` | New: `UpgradeMonitor` — K8s watch loop with exponential backoff, auth-error recovery, single-slot execution guard, and `handleExecute` stub |
| `internal/daemon/consensus/errors.go` | New: `ErrK8sClient`, `ErrWatchFailed` under `daemon.consensus` namespace |
| `internal/daemon/consensus/export_test.go` | New: exposes `isAuthError` and `SetOnExecute` hook for white-box tests |
| `internal/daemon/consensus/upgrade_monitor_test.go` | New: 6 unit tests via `k8s.io/client-go/dynamic/fake` |
| `internal/daemon/config.go` | New: `DaemonConfig` struct + `LoadDaemonConfig` (fail-fast if missing/invalid) |
| `internal/daemon/errors.go` | New: `ErrConfig`, `ErrConfigNotFound` (NotFound trait), `ErrConfigMalformed` under `daemon` namespace |
| `internal/daemon/daemon.go` | Modified: `New()` reads `daemon.yaml`, constructs `UpgradeMonitor` |
| `internal/daemon/export_test.go` | New: `NewWithComponents` test helper (test-only) |
| `pkg/models/weaver_paths.go` | Modified: added `DaemonConfigPath` field (`$home/config/daemon.yaml`) |
| `cmd/daemon/main.go` | Modified: load+override+validate flow; optional `--node-id`, `--kubeconfig`, `--orbit`, `--upgrade-dir` flags; calls `NewFromConfig` with resolved config |

## Review Checklist

- [ ] `Run()` returns `nil` (not an error) on clean context cancellation
- [ ] All watch errors are retried with backoff; daemon never exits on transient failure
- [ ] Clean watch channel close sleeps `backoffInitial` (2s) before reconnecting — no hot-loop on silent proxy drops
- [ ] `ListOptions.TimeoutSeconds` is set to 300 to bound the server-side watch stream
- [ ] Auth errors (401/403) rebuild the dynamic client from kubeconfig before retrying
- [ ] If kubeconfig re-read fails after an auth error, the daemon retries with the stale credential; backoff grows to 5 min and the daemon is non-functional for watch indefinitely (systemd does NOT restart — process is still running); recovery requires restoring the kubeconfig or running `provisioner daemon install` — documented in code
- [ ] If a proxy has an idle timeout shorter than `watchTimeoutSeconds` (300s), clean-close reconnects happen every `backoffInitial` (2s) indefinitely; this is a known trade-off documented in code — fix is to configure the proxy idle timeout above 300s
- [ ] `isAuthError` unwraps one errorx level to reach the typed `*StatusError`
- [ ] CR with empty `spec.operationId` is warned and dropped, not silently treated as duplicate
- [ ] **Single-slot invariant**: a duplicate event for the active operationId logs `UpgradeMonitorDuplicateEvent` and is dropped
- [ ] **Single-slot invariant**: a *different* operationId arriving while one is active logs `UpgradeMonitorBusy` (warn) and is rejected — no concurrent upgrades
- [ ] `activeOpID` is cleared in `defer` only when it still matches the goroutine's own operationId (identity-check guard)
- [ ] `daemon.Run` has a top-level `recover()` that logs the panic with `reason=DaemonPanic` and calls `os.Exit(2)` — ensuring the reason is captured before systemd restarts the process
- [ ] `buildDynamicClient` sets `restCfg.Timeout = 30s` — bounds the client-side HTTP dial so a TCP-level hang (SYN sent, no reply) does not block `Watch()` for the OS TCP timeout (~20 min)
- [ ] `handleExecute` stub carries a comment requiring implementors to use per-step timeout contexts — a hung step permanently locks `activeOpID` and silently rejects all future upgrades
- [ ] `runWatch` has a named-return `recover()` — a panic in the watch loop logs and returns an error, triggering backoff rather than crashing the daemon
- [ ] `handleExecute` goroutine has `recover()` — a panic in the workflow does not crash the daemon
- [ ] Missing `daemon.yaml` returns `ErrConfigNotFound` (carries `errorx.NotFound()` trait)
- [ ] Malformed or missing required fields (`node_id`, `kubeconfig`, `orbit`) return `ErrConfigMalformed`
- [ ] `upgrade_dir` is optional — empty value falls back to `/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current` via `DaemonConfig.upgradeDir()`
- [ ] `DaemonConfig.Validate()` is called by `LoadDaemonConfig` and again by `cmd/daemon/main.go` after flag overrides — validation is never bypassed
- [ ] CLI flag overrides (`--node-id`, `--kubeconfig`, `--orbit`, `--upgrade-dir`) each take precedence over the corresponding `daemon.yaml` field when set; unset flags leave the file value unchanged
- [ ] `NewFromConfig` is the construction path when caller holds a resolved config; `New` wraps it for the no-override production path
- [ ] `DaemonConfigPath` resolves to `$home/config/daemon.yaml`
- [ ] `cmd/daemon/main.go` returns `daemon.New` errors unwrapped — no double-wrap in `errorx.InternalError`
- [ ] `NewWithComponents` (export_test.go) is test-only and not part of the production API
- [ ] All new source files carry the SPDX Apache-2.0 header
- [ ] Upgrade event log (`/opt/solo/weaver/daemon/events/`) is **not** implemented here — deferred to the story that implements `handleExecute`; the HIP defines the directory but not the format or event names, which cannot be determined until the actual workflow steps exist

## Test Commands

```bash
# Unit tests (run inside UTM VM)
task vm:test:unit

# Targeted coverage for changed packages
task test:coverage TEST_PATHS=./internal/daemon/... TEST_REGEX="."
```

## Manual UAT

### Prerequisites

A running Kubernetes cluster (the UTM VM with `kube cluster install` already done).

### Step 1 — Build and copy the daemon binary

```bash
# On macOS host
task build:weaver GOOS=linux GOARCH=amd64

# Copy to VM (after task vm:ssh)
cd ~
cp /mnt/solo-weaver/bin/solo-provisioner-daemon-linux-amd64 . 
```

### Step 2 — Install the NetworkUpgradeExecute CRD

The CRD is not yet shipped by solo-operator in this story. Apply it manually:

```bash
kubectl apply -f - <<'EOF'
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: networkupgradeexecutes.hedera.com
spec:
  group: hedera.com
  names:
    kind: NetworkUpgradeExecute
    listKind: NetworkUpgradeExecuteList
    plural: networkupgradeexecutes
    singular: networkupgradeexecute
  scope: Namespaced
  versions:
  - name: v1alpha1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              operationId: {type: string}
              orbit:       {type: string}
          status:
            type: object
            properties:
              phase: {type: string}
    subresources:
      status: {}
EOF
```

### Step 3 — Create the namespace and RBAC

```bash
kubectl create namespace hedera-network --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: solo-provisioner-daemon
  namespace: hedera-network
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: solo-provisioner-daemon
rules:
- apiGroups: ["hedera.com"]
  resources:
    - networkupgradeexecutes
    - networkupgradeexecutes/status
  verbs: ["get", "list", "watch", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: solo-provisioner-daemon
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: solo-provisioner-daemon
subjects:
- kind: ServiceAccount
  name: solo-provisioner-daemon
  namespace: hedera-network
EOF
```

### Step 4 — Generate a scoped kubeconfig for the daemon

```bash
# Create a long-lived token for the service account
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: solo-provisioner-daemon-token
  namespace: hedera-network
  annotations:
    kubernetes.io/service-account.name: solo-provisioner-daemon
type: kubernetes.io/service-account-token
EOF

# Wait for token to be populated
kubectl wait secret/solo-provisioner-daemon-token -n hedera-network \
  --for=jsonpath='{.data.token}' --timeout=30s

# Extract token and CA cert
TOKEN=$(kubectl get secret solo-provisioner-daemon-token -n hedera-network \
  -o jsonpath='{.data.token}' | base64 -d)
CA=$(kubectl get secret solo-provisioner-daemon-token -n hedera-network \
  -o jsonpath='{.data.ca\.crt}')
SERVER=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')

# Write daemon kubeconfig
sudo mkdir -p /opt/solo/weaver/config
sudo tee /opt/solo/weaver/config/daemon.kubeconfig <<EOF
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: ${SERVER}
    certificate-authority-data: ${CA}
users:
- name: solo-provisioner-daemon
  user:
    token: ${TOKEN}
contexts:
- name: default
  context:
    cluster: local
    user: solo-provisioner-daemon
current-context: default
EOF
```

### Step 5 — Write daemon.yaml

```bash
sudo mkdir -p /opt/solo/weaver/config
sudo tee /opt/solo/weaver/config/daemon.yaml <<'EOF'
node_id:     0.0.3
kubeconfig:  /opt/solo/weaver/sandbox/etc/weaver/kubeconfig
orbit:       hedera-network
upgrade_dir: /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
EOF
```

### Step 6 — Run the daemon

```bash
sudo ./solo-provisioner-daemon-linux-amd64 --log-level debug
```

Expected log output (JSON):

```json
{"level":"info","reason":"UpgradeMonitorStarted","namespace":"hedera-network","message":"Upgrade monitor started"}
{"level":"debug","reason":"UpgradeMonitorWatchEstablished","namespace":"hedera-network","message":"Watch established on NetworkUpgradeExecute CRs"}
```

### Step 7 — Create a NetworkUpgradeExecute CR in ReadyForProvisionerDaemon phase

In a second terminal:

```bash
kubectl apply -f - <<'EOF'
apiVersion: hedera.com/v1alpha1
kind: NetworkUpgradeExecute
metadata:
  name: upgrade-20260522T120000Z-execute
  namespace: hedera-network
spec:
  operationId: upgrade-20260522T120000-v0.75.0
  orbit: hedera-network
status:
  phase: ReadyForProvisionerDaemon
EOF

# Status must be patched via the status subresource (apply sets spec only)
kubectl patch networkupgradeexecute upgrade-20260522T120000Z-execute \
  -n hedera-network \
  --subresource=status \
  --type=merge \
  -p '{"status":{"phase":"ReadyForProvisionerDaemon"}}'
```

Expected daemon log output:

```json
{"level":"info","reason":"ReadyForProvisionerDaemon","operation_id":"upgrade-20260522T120000-v0.75.0","orbit":"hedera-network","cr_name":"upgrade-20260522T120000Z-execute","message":"NetworkUpgradeExecute entered ReadyForProvisionerDaemon — triggering execute workflow"}
{"level":"info","reason":"ExecuteWorkflowStarted","operation_id":"upgrade-20260522T120000-v0.75.0","message":"Execute workflow stub — full implementation in subsequent stories"}
```

### Step 8 — Verify deduplication

Apply the same CR again (or trigger a Modified event via another patch):

```bash
kubectl patch networkupgradeexecute upgrade-20260522T120000Z-execute \
  -n hedera-network \
  --subresource=status \
  --type=merge \
  -p '{"status":{"phase":"ReadyForProvisionerDaemon"}}'
```

Expected: daemon logs `UpgradeMonitorDuplicateEvent` at debug level; `ExecuteWorkflowStarted` is NOT logged again.

### Step 9 — Verify fail-fast on missing daemon.yaml

```bash
sudo mv /opt/solo/weaver/config/daemon.yaml /tmp/daemon.yaml.bak
sudo /tmp/solo-provisioner-daemon-linux-amd64
# Expected: non-zero exit, error type daemon.config mentioning the missing path
sudo mv /tmp/daemon.yaml.bak /opt/solo/weaver/config/daemon.yaml
```

## Notes

- `handleExecute` is a stub; full implementation lands in subsequent stories.
- Steps 2–5 (CRD, RBAC, kubeconfig, daemon.yaml) are manual test scaffolding. In production
  these will be owned by the planned `solo-provisioner provisioner daemon install` command,
  which will create the ServiceAccount, ClusterRole, kubeconfig at
  `/opt/solo/weaver/config/daemon.kubeconfig`, and write `daemon.yaml`. The full lifecycle:
  ```
  solo-provisioner provisioner daemon install    # provision credentials + start daemon
  solo-provisioner provisioner daemon check      # query /health on daemon socket
  solo-provisioner provisioner daemon uninstall  # stop daemon + remove credentials
  solo-provisioner provisioner self   upgrade    # existing self-upgrade command
  ```
- In production the NetworkUpgradeExecute CR (step 7) is created by the NUD sidecar, not
  manually. The manual `kubectl apply` + status patch is for triggering the watch without NUD.
- The scoped kubeconfig uses a static service-account token for simplicity. Production will
  use a short-lived token rotated by the Kubernetes token controller; the daemon's
  auth-error recovery path handles rotation transparently.
