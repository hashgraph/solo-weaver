# Review guide — #644 simplify block node upgrade by pre-deleting Service

> Branch: `00644-simplify-bn-upgrade-pre-delete-service`
> Base: `main` (branched from `origin/main` @ `f07505c`, the merge of #632)
> Issue: https://github.com/hashgraph/solo-weaver/issues/644

## Summary

PR #632 fixed the Cilium reconciler-miss on block-node upgrade by snapshotting Services
pre-upgrade, diffing post-upgrade, and rolling-restarting the cluster-wide `ds/cilium`
when anything changed. This PR replaces that machinery with a single pre-upgrade step
that deletes the helm-owned Services so that Helm itself recreates them as part of the
upgrade. Cilium's eBPF reconciler handles CREATE events correctly — the bug from #619
is specifically that it drops `Service.spec.type` UPDATE transitions — so the topology
flip heals itself without any kube-system-wide restart.

Net diff: **+131 / −535 LOC**. The `internal/cilium` package, the `KindDaemonSet` GVR
plumbing and `RolloutRestartDaemonSet`/`IsDaemonSetRolledOut` helpers, the
`SnapshotServices` / `BlockNodeServicesChanged` / `diffServiceSpecs` /
`RestartCiliumDaemonSetIfServicesChanged` family, the `preUpgradeServiceSpecs` field
on `Manager`, and two saga step builders all disappear. `VerifyExternalReachable` +
`ProbeTCP` are retained as the post-upgrade safety net.

## Files changed

| File | Change |
|---|---|
| `internal/blocknode/chart.go` | Added `Manager.DeleteHelmOwnedServices(ctx)` and `HelmOwnedServiceDeleteTimeout = 30s`. List-filter-delete-wait sequence using existing `kube.Client.List` and `WaitForResourcesDeletion`. |
| `internal/blocknode/manager.go` | Removed `preUpgradeServiceSpecs` field; updated struct doc to point at the new chart.go responsibility and the reachability.go probe responsibility. |
| `internal/blocknode/reachability.go` | Removed `SnapshotServices`, `BlockNodeServicesChanged`, `diffServiceSpecs`, `RestartCiliumDaemonSetIfServicesChanged`, plus the `internal/cilium` import and one `reflect` import. Kept `BlockNodePublicPort`, `VerifyExternalReachable`, `findLoadBalancerEndpoint`. |
| `internal/blocknode/reachability_test.go` | Removed `TestDiffServiceSpecs`. Kept `TestBlockNodePublicPort_IsWellKnownValue`. |
| `internal/cilium/cilium.go`, `internal/cilium/cilium_test.go` | Package deleted — sole consumer was the BN restart-if-changed step. |
| `internal/kube/client.go` | Removed `KindDaemonSet` ResourceKind, its GVR map entry, `IsDaemonSetRolledOut` (CheckFunc), and `RolloutRestartDaemonSet`. |
| `internal/kube/daemonset_test.go` | File deleted — every test covered removed surface. |
| `internal/workflows/steps/step_block_node.go` | Removed `SnapshotBlockNodeServicesStepId` + `RestartCiliumIfServicesChangedStepId` constants and their step builders. Moved the `DeleteHelmOwnedServices` call **inside** `upgradeBlockNode`'s Execute — after preflight (migration discovery + values-file computation) and immediately before the helm operation — so preflight failures leave Services intact. The saga is now `ensure-hedera-owner → upgrade-block-node → wait-for-block-node → verify-block-node-reachable`. Updated `UpgradeBlockNode` doc comment. |
| `docs/claude/plans/00644-simplify-bn-upgrade-pre-delete-service.md` | New plan file with decisions table and scope checklist. |

## Review checklist

- [ ] `DeleteHelmOwnedServices` scopes by `app.kubernetes.io/managed-by=Helm` AND `app.kubernetes.io/instance=<release>` — not just `managed-by=Helm` (would catch other releases) and not just `instance=<release>` (would catch operator-managed Services that happen to share an instance label). The conjunction is the correct fence.
- [ ] No-match (empty list) is a success no-op — a prior failed upgrade may have left the Services already deleted.
- [ ] `WaitForResourcesDeletion` is called after submitting all deletes — not interleaved per-Service — so propagation overlaps. 30s timeout via `HelmOwnedServiceDeleteTimeout`.
- [ ] The `DeleteHelmOwnedServices` call lives **inside** `upgradeBlockNode`'s Execute, positioned *after* `BuildMigrationWorkflow` / `ComputeValuesFile` succeed and *immediately before* the helm action. Preflight failures leave Services intact (no outage); only failures from `UpgradeChart` onward could leave the Service set empty, and helm's `atomic: true` covers those by rolling back to the previous release (which includes the Services). SetupBlockNode (install) does NOT call the delete — there's nothing to delete on first install.
- [ ] Reconfigure (`internal/bll/blocknode/reconfigure_handler.go`) inherits the new step transitively via its `steps.UpgradeBlockNode(ins)` calls — no separate wiring needed.
- [ ] `upgradeBlockNode` has no `WithRollback` hook. Justification: helm's `atomic: true` rolls back partial helm operations to the previous release; the only state a rollback hook could undo is "Services deleted but helm never ran", which doesn't happen because the delete is the last thing before the helm call. Recovery for any half-failed upgrade is "re-run the upgrade" — same workflow re-applies cleanly.
- [ ] `VerifyExternalReachable` is unchanged and still runs as the final saga step on both `SetupBlockNode` and `UpgradeBlockNode`. It's the safety net regardless of root cause.
- [ ] `BlockNodePublicPort` is intentionally kept at 40840 (ecosystem contract). The test asserting this is retained.
- [ ] No production caller of the removed `RolloutRestartDaemonSet` / `IsDaemonSetRolledOut` / `KindDaemonSet` exists post-PR. Verify with `git grep` after pulling.
- [ ] No production caller of `SnapshotServices` / `BlockNodeServicesChanged` / `diffServiceSpecs` / `RestartCiliumDaemonSetIfServicesChanged` exists post-PR. Verify with `git grep`.

## How to test

### Unit (macOS-safe subset)

```bash
go test -tags='!integration' -count=1 ./internal/blocknode/... ./internal/kube/...
```

### Full unit + integration (UTM VM, per project CLAUDE.md)

```bash
task vm:test:unit
task vm:test:integration TEST_NAME='^TestUpgradeBlockNode'
```

### Lint

```bash
task lint:check
```

(`lint:errorx` should report `0 issues.`)

## Manual UAT — Shape A → Shape B repro from #619

Run inside the UTM VM. Same recipe as the #619 plan, but the success criterion is now
"`cilium service list` shows the new entry **without** any Cilium DaemonSet restart."

1. **Set up the MetalLB pool to a single IP** (so the BN keeps the same LB IP across
   the upgrade and we can verify the cilium-service entry tracks the topology flip
   rather than an IP churn):

    ```bash
    kubectl edit ipaddresspool public-address-pool -n metallb-system
    # set: addresses: ["192.168.68.200/32"]
    kubectl rollout restart deployment/speaker -n metallb-system
    ```

2. **Install Shape A** (split `ClusterIP` + `-external` `LoadBalancer`):

    ```yaml
    # shape-a.yaml
    service:
      type: ClusterIP
    loadBalancer:
      enabled: true
      loadBalancerIP: "192.168.68.200"
      annotations:
        metallb.io/address-pool: public-address-pool
    ```

    ```bash
    solo-provisioner block node install --values shape-a.yaml
    ```

   **Expected:** `cilium service list | grep 192.168.68.200` shows one
   `LoadBalancer 1 => <pod-ip>:40840/TCP (active)` entry.

3. **Run the upgrade with no values file** (lets weaver re-render to Shape B):

    ```bash
    solo-provisioner block node upgrade --chart-version 0.33.0
    ```

   **Expected workflow output:**

    ```
    [upgrade-block-node]          Upgrading Block Node chart
    [upgrade-block-node]          (in-step log: "Deleting helm-owned Services before upgrade so Cilium reconciles a fresh CREATE")
    [wait-for-block-node]         Waiting for Block Node to be ready
    [verify-block-node-reachable] Block Node external reachability verified
    ```

4. **Verify post-upgrade datapath:**

    ```bash
    kubectl get svc -n block-node
    # expect: 1 Service of type LoadBalancer with IP 192.168.68.200 (Shape B)

    cilium service list | grep 192.168.68.200
    # expect: a fresh LoadBalancer entry with the new pod IP

    kubectl get pods -n kube-system -l k8s-app=cilium
    # expect: cilium pod AGE is the same as it was pre-upgrade — no DS rollout

    nc -zvw5 192.168.68.200 40840
    # expect: open
    ```

5. **Benign-upgrade case** — re-run the same `solo-provisioner block node upgrade
   --chart-version 0.33.0` without any topology change:

    ```bash
    solo-provisioner block node upgrade --chart-version 0.33.0
    ```

   **Expected:** the `upgrade-block-node` step logs the in-step delete, helm
   recreates the Services, same LB IP returns, the reachability probe passes.
   There is a brief outage window (seconds) where the LB IP isn't dispatched —
   confirm by tightening `nc` to a tight loop before/during/after.

## Risks / rollbacks

- **Brief external outage on every upgrade.** Bounded by `HelmOwnedServiceDeleteTimeout`
  (30s) plus helm's recreate + MetalLB re-ARP + Cilium reconcile (typically <10s).
- **LB IP churn** when MetalLB pool has multiple unpinned IPs. Documented in the issue
  body; operators pin `loadBalancerIP` when stability matters.
- **Rollback:** revert this PR; the merged state from #632 (snapshot + Cilium restart)
  is the previous known-good behavior.
