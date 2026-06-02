# PR Review Guide — fix(blocknode): restart Cilium agent and probe reachability after BN upgrade

**Issue:** [#619](https://github.com/hashgraph/solo-weaver/issues/619)
**Branch:** `00619-block-node-upgrade-mutates-service-topology`

---

## What Was Done

### Problem

When `solo-provisioner block node upgrade` changes the block-node `Service.spec.type` (e.g. an
operator-installed Shape A `ClusterIP` + `-external` LoadBalancer cluster is flipped to weaver's
Shape B combined LoadBalancer), Cilium's eBPF service reconciler **silently misses the mutation**
and never installs the new LoadBalancer DNAT rule. MetalLB still ARP-announces the IP, the
Service looks healthy in every `kubectl` view, the pod is `Ready` — but `cilium service list`
has no entry for the LB IP and external traffic black-holes. The bug is invisible until someone
actually dials the LB.

`kubectl rollout restart ds/cilium -n kube-system` immediately restores the eBPF entry, which
confirms the failure mode is Cilium's reconciler dropping the event rather than a config rejection.

### Solution

Two-layer fix bolted onto the existing upgrade workflow:

1. **Conditional Cilium DaemonSet restart.** Snapshot the BN-namespace Services right before
   `helm upgrade`, diff against the post-upgrade state, and rolling-restart `ds/cilium` only when
   any Service `spec` actually changed (added, removed, or `DeepEqual` differs). The common case
   (chart-version-only bump, no Service changes) skips the ~30s restart entirely.
2. **TCP reachability probe.** After every install and upgrade, the provisioner dials the BN
   LoadBalancer IP from its own process and fails the workflow loudly if the connection can't be
   established within 60s. Converts the silent black-hole into an actionable error regardless of
   whether the cause is Cilium, MetalLB, the chart, or a firewall.

Three packages absorb the new logic so concerns stay separated:
- `internal/network/ProbeTCP` — generic TCP-dial-with-retry helper (sits next to `CheckEndpointReachable`).
- `internal/cilium/RestartAgentDaemonSet` — wraps the kube API call + rollout-wait against `kube-system/cilium`.
- `internal/blocknode/` — keeps the BN-specific policy (snapshot/diff/conditional gate) and the LB-endpoint lookup.

Topology preservation (the more invasive injector originally proposed in the issue) is **out of
scope**; trade-off documented in the plan.

### Files Changed

| File | Change |
|------|--------|
| `internal/kube/client.go` | Added `KindDaemonSet` + GVR mapping, `RolloutRestartDaemonSet(ctx, ns, name)`, and `IsDaemonSetRolledOut` CheckFunc (observedGeneration caught up + numberReady == desired + updatedNumberScheduled == desired). |
| `internal/kube/daemonset_test.go` (new) | Unit tests for `IsDaemonSetRolledOut` (7 cases) and `KindDaemonSet` GVR round-trip. |
| `internal/network/network.go` | Added `ProbeTCP(ctx, addr, overallTimeout, dialTimeout, retryDelay) (int, error)` — generic TCP dial with retry-until-deadline. |
| `internal/network/network_test.go` | Added 3 `ProbeTCP` tests against a real local listener (success, refused-fast, parent-ctx-cancel). |
| `internal/cilium/cilium.go` (new) | New package: `AgentDaemonSetNamespace`/`AgentDaemonSetName`/`DefaultRolloutTimeout` constants + `RestartAgentDaemonSet(ctx, kubeClient, timeout) error`. |
| `internal/cilium/cilium_test.go` (new) | Guards the namespace/name constants against accidental rename and sanity-checks the default rollout timeout. |
| `internal/blocknode/manager.go` | Added `preUpgradeServiceSpecs` field to `Manager`; added `ReachabilityProbeTimeoutSec`/`ReachabilityProbeDialTimeout`/`ReachabilityProbeRetryDelay` constants. |
| `internal/blocknode/reachability.go` (new) | `SnapshotServices`, `BlockNodeServicesChanged`, `RestartCiliumDaemonSetIfServicesChanged`, `VerifyExternalReachable`, `findLoadBalancerEndpoint`, `readNamedPort`, and the pure `diffServiceSpecs` helper. |
| `internal/blocknode/reachability_test.go` (new) | Unit tests for `diffServiceSpecs` (5 cases) and `readNamedPort` (4 cases). |
| `internal/workflows/steps/step_block_node.go` | New step IDs + builders: `snapshotBlockNodeServices`, `restartCiliumIfServicesChanged`, `verifyBlockNodeReachable`. `UpgradeBlockNode` now: `EnsureHederaOwnerStep → snapshot → upgrade → wait → restart-if-changed → verify`. `SetupBlockNode` appends only `verify` after `wait`. |
| `docs/claude/plans/00619-block-node-upgrade-mutates-service-topology.md` (new) | Design plan. |

---

## Code Review Checklist

### `internal/kube/client.go`

- [x] `KindDaemonSet` added to `kindToGVR` with `{Group: "apps", Version: "v1", Resource: "daemonsets"}`.
- [x] `RolloutRestartDaemonSet` uses `types.MergePatchType` (consistent with `patchReplicas`) and patches `spec.template.metadata.annotations` with `kubectl.kubernetes.io/restartedAt = <RFC3339>` — same mechanic as `kubectl rollout restart`.
- [x] `IsDaemonSetRolledOut` returns `true` only when `observedGeneration ≥ generation` AND `numberReady == desiredNumberScheduled` AND `updatedNumberScheduled == desiredNumberScheduled` — covers both "controller still catching up" and "old pods still running" mid-rollout.
- [x] `IsDaemonSetRolledOut` short-circuits to `true` when `desiredNumberScheduled == 0` (no nodes selected — nothing to roll out).

### `internal/network/network.go`

- [x] `ProbeTCP` derives a `probeCtx` from the parent ctx with `overallTimeout` and respects parent cancellation.
- [x] Each dial uses its own `dialCtx` scoped to `dialTimeout` so a hung dial cannot eat the whole budget.
- [x] Returns the number of attempts on success (1-based) for caller logging.
- [x] On failure path, returns the last dial error — not a generic "timeout" — so caller logs are useful.

### `internal/cilium/cilium.go`

- [x] `AgentDaemonSetNamespace`/`AgentDaemonSetName` match what `internal/templates/files/cilium/` installs; tests guard against accidental rename.
- [x] `RestartAgentDaemonSet` triggers the restart then waits with `IsDaemonSetRolledOut` — never returns before the rollout settles.

### `internal/blocknode/reachability.go`

- [x] `SnapshotServices` reads `spec` only (not `status`); MetalLB status updates during the upgrade don't trip the diff.
- [x] `BlockNodeServicesChanged` returns `true` conservatively when no snapshot was taken — the workflow can't prove nothing changed.
- [x] `diffServiceSpecs` uses `reflect.DeepEqual` per Service and detects membership changes (add/remove) in addition to spec mutations.
- [x] `RestartCiliumDaemonSetIfServicesChanged` delegates to `cilium.RestartAgentDaemonSet` rather than reaching directly into the kube client — keeps Cilium knowledge out of the BN package.
- [x] `VerifyExternalReachable` no-ops when `LoadBalancerEnabled == false` (e.g. local profile) — no false failures on no-LB clusters.
- [x] `findLoadBalancerEndpoint` reads `status.loadBalancer.ingress[0].ip` (what MetalLB actually announced), not `spec.loadBalancerIP` (the request).
- [x] `readNamedPort` resolves the port by chart-defined name `port-block-node` rather than hard-coding `40840`.

### `internal/workflows/steps/step_block_node.go`

- [x] `UpgradeBlockNode` steps in correct order: snapshot **before** the upgrade, restart/probe **after** `waitForBlockNode`.
- [x] `SetupBlockNode` does **not** include the snapshot or the conditional restart — a fresh install has no pre-existing eBPF entry to miss.
- [x] All three new step builders use the shared `newBlockNodeManagerProvider` so the Manager (and its `preUpgradeServiceSpecs` field) is shared across the steps.

### `pkg/models/inputs.go`

- [x] No change — `LoadBalancerEnabled` already exists on `BlockNodeInputs` from PR #477.

---

## Running the Tests

### Unit tests (macOS safe)

Each new helper is unit-testable in isolation:

```bash
# Pure spec-diff and port-lookup helpers
go test -tags='!integration' -run 'TestDiffServiceSpecs|TestReadNamedPort' ./internal/blocknode/... -v

# DaemonSet rollout-check function
go test -tags='!integration' -run 'TestIsDaemonSetRolledOut|TestKindDaemonSet' ./internal/kube/... -v

# Generic TCP probe (uses a real local listener; takes ~6s)
go test -tags='!integration' -run 'TestProbeTCP' ./internal/network/... -v

# Cilium package sanity guards
go test -tags='!integration' ./internal/cilium/... -v
```

All four packages together:

```bash
go test -tags='!integration' ./internal/blocknode/... ./internal/cilium/... ./internal/kube/... ./internal/network/...
```

Expected: every package reports `ok`. The `network` package is slower (~6s) because `TestProbeTCP_FailsWhenTargetUnreachable` exercises a real retry loop against a closed port.

### Full unit suite

```bash
task test:unit
```

### Integration tests (must run inside the UTM VM)

The existing BN install/upgrade integration tests already exercise the new workflow shape end-to-end — they should pass unchanged with the snapshot/restart-if-changed/probe steps appended:

```bash
task vm:test:integration TEST_NAME='^TestSetupBlockNode_FreshInstall$'
task vm:test:integration TEST_NAME='^TestUpgradeBlockNode_'      # all upgrade variants
task vm:test:integration TEST_NAME='^TestReconfigureBlockNode_'  # reconfigure inherits the new steps via UpgradeBlockNode
```

If any of those fail with a "block node LoadBalancer at ... is not reachable" error, that's the new probe doing its job — investigate the cluster, not the test.

---

## Manual UAT — Step by Step

> All commands run **inside the UTM VM** unless explicitly marked `[host]`.

### 0. Pre-flight

```bash
# [host] Build the linux binary
task build:cli GOOS=linux GOARCH=amd64

# [VM] Start clean
task uat:setup     # bootstraps cluster + bn from scratch
```

You should see four new step lines in the install workflow output:

```
▶ Verifying Block Node external reachability
✓ Block-node LoadBalancer is reachable
```

### 1. Happy path — chart bump that doesn't touch Services (should *skip* Cilium restart)

```bash
# Upgrade to the next chart version
solo-provisioner block node upgrade --chart-version <next-version>
```

Watch the workflow output. You should see:

```
▶ Snapshotting Block Node Services
✓ Block Node Services snapshotted
▶ Upgrading Block Node chart
✓ Block Node chart upgraded successfully
▶ Waiting for Block Node to be ready
✓ Block Node is ready
▶ Checking whether Cilium DaemonSet needs a restart
  No block-node Service changes detected; skipping Cilium DaemonSet restart
✓ Cilium DaemonSet check complete
▶ Verifying Block Node external reachability
✓ Block-node LoadBalancer is reachable
```

The key line: **"No block-node Service changes detected; skipping Cilium DaemonSet restart"** — confirms the conditional gate works. Total upgrade time should be ~30s shorter than the unconditional approach.

Sanity-check that Cilium was *not* restarted:

```bash
kubectl get pod -n kube-system -l k8s-app=cilium -o jsonpath='{.items[*].metadata.creationTimestamp}'
# Timestamps should be older than the upgrade — same pods are still running.
```

### 2. The actual bug repro — Shape A → Shape B flip (should *fire* Cilium restart)

This is the scenario the PR fixes. You need a cluster originally installed with the split-shape values.

**Setup (one-time, before testing the upgrade):**

```bash
# Configure MetalLB to give a deterministic external IP
kubectl edit ipaddresspool public-address-pool -n metallb-system
# Set: addresses: ["192.168.68.200/32"]

# Restart the speaker so the new pool takes effect
kubectl rollout restart ds/speaker -n metallb-system

# Write a Shape A values file
cat > /tmp/shape-a.yaml <<'EOF'
service:
  type: ClusterIP
loadBalancer:
  enabled: true
  loadBalancerIP: "192.168.68.200"
  annotations:
    metallb.io/address-pool: public-address-pool
EOF

# Install BN with Shape A
solo-provisioner block node install --values /tmp/shape-a.yaml
```

Verify both Services exist and Cilium has the LB entry:

```bash
kubectl get svc -n block-node
# Expected: two services — one ClusterIP (main), one LoadBalancer (-external)

cilium service list | grep 192.168.68.200
# Expected: 192.168.68.200:40840/TCP   LoadBalancer   1 => <pod-ip>:40840/TCP (active)

nc -zvw5 192.168.68.200 40840
# Expected: succeeded
```

Capture the current Cilium pod start time to verify the restart later:

```bash
CILIUM_BEFORE=$(kubectl get pod -n kube-system -l k8s-app=cilium -o jsonpath='{.items[0].metadata.creationTimestamp}')
echo "Cilium pod before upgrade: $CILIUM_BEFORE"
```

**Run the upgrade — this is what was previously broken:**

```bash
solo-provisioner block node upgrade --chart-version 0.33.0
```

Expected workflow output (note the **detected** branch this time):

```
▶ Snapshotting Block Node Services
✓ Block Node Services snapshotted
▶ Upgrading Block Node chart
✓ Block Node chart upgraded successfully
▶ Waiting for Block Node to be ready
✓ Block Node is ready
▶ Checking whether Cilium DaemonSet needs a restart
  Block-node Service mutation detected; restarting Cilium to refresh eBPF reconciler
✓ Cilium DaemonSet check complete
▶ Verifying Block Node external reachability
✓ Block-node LoadBalancer is reachable
```

Post-upgrade assertions:

```bash
# 1. Cilium was actually restarted
CILIUM_AFTER=$(kubectl get pod -n kube-system -l k8s-app=cilium -o jsonpath='{.items[0].metadata.creationTimestamp}')
echo "Cilium pod after upgrade:  $CILIUM_AFTER"
# Expected: $CILIUM_AFTER is later than $CILIUM_BEFORE.

# 2. Cilium has the LB entry (this is the regression we're guarding against)
cilium service list | grep 192.168.68.200
# Expected: non-empty entry.

# 3. Traffic actually flows
nc -zvw5 192.168.68.200 40840
# Expected: succeeded.

# 4. From an in-cluster pod, for completeness
kubectl run debug --rm -i --restart=Never --image=busybox -- nc -zvw5 192.168.68.200 40840
# Expected: succeeded.
```

**Pre-PR behaviour (sanity check that this PR is the fix):** if you `git stash`-and-rebuild against `origin/main` and repeat step 2, you'd see steps 2/3/4 above fail (no `cilium service list` entry, `nc` times out) — exactly the issue #619 symptom.

### 3. Reachability probe failure mode (negative test)

Force the probe to fail to confirm it surfaces a clear error rather than silently passing:

```bash
# Block all traffic to MetalLB's range temporarily
sudo iptables -I OUTPUT -d 192.168.68.200 -j DROP

# Run any upgrade
solo-provisioner block node upgrade --chart-version <current>
```

Expected: the workflow fails with a loud error from the probe step:

```
✗ Block Node is not reachable from the provisioner host
  block node LoadBalancer at 192.168.68.200:40840 is not reachable from
  solo-provisioner after 6 attempts in 1m0s — traffic is likely being
  dropped by Cilium or MetalLB
```

Cleanup:

```bash
sudo iptables -D OUTPUT -d 192.168.68.200 -j DROP
```

### 4. Local profile skip (no MetalLB pool)

The probe must no-op on the `local` profile since there is no LoadBalancer to dial.

```bash
solo-provisioner block node upgrade --profile local --chart-version <current>
```

Expected: the workflow runs but the reachability step produces no output (debug-level log only). No errors.

### 5. Reconfigure inherits the new steps

`block node reconfigure` ultimately delegates to `UpgradeBlockNode`, so it should also pick up the snapshot/restart-if-changed/probe steps. Verify by running:

```bash
solo-provisioner block node reconfigure --values /tmp/shape-a.yaml
```

You should see the same step sequence as in §1 or §2 depending on whether the values actually change Service shape.

---

## Quick reference — assertions in one place

| What to verify | Command | Expected |
|---|---|---|
| Cilium DS exists and is healthy | `kubectl get ds/cilium -n kube-system` | `READY` = `DESIRED` |
| Cilium has the LB entry | `cilium service list \| grep <lb-ip>` | non-empty row |
| LB is reachable externally | `nc -zvw5 <lb-ip> 40840` | `succeeded` |
| LB is reachable in-cluster | `kubectl run x --rm -i --restart=Never --image=busybox -- nc -zvw5 <lb-ip> 40840` | `succeeded` |
| Cilium was/wasn't restarted | `kubectl get pod -n kube-system -l k8s-app=cilium -o jsonpath='{.items[*].metadata.creationTimestamp}'` | timestamps moved (or not) |
| BN Services present | `kubectl get svc -n block-node` | Shape A → 2 Services, Shape B → 1 Service |

---

## Notes for the reviewer

- **Why no in-cluster pod probe?** Earlier draft spawned a busybox pod running `nc` for the probe. Swapped to `net.DialContext` from the provisioner process — same MetalLB-ARP + Cilium-DNAT path for a node-local tool, ~10× simpler, no template/image dependency. The only deployment that would lose coverage is "solo-provisioner running from an off-cluster bastion", which isn't the documented operator workflow.
- **Why conditional restart instead of unconditional?** Most upgrades are chart-version bumps that don't touch Services. Paying ~30s for a Cilium agent rollout on every upgrade is wasteful. The snapshot + diff costs two `LIST services` calls (negligible) in exchange for skipping the restart on the common case.
- **Why not preserve Shape A topology?** Documented as explicit out-of-scope in the plan. Operators who pin a specific `loadBalancerIP` on a Shape A `-external` Service may see MetalLB reallocate from the pool on upgrade; the Cilium restart still heals the connectivity black-hole. A follow-up issue can be opened if the multi-IP-pool scenario surfaces in production.
