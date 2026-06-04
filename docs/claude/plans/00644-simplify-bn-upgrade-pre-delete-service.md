# #644 — Simplify block node upgrade by pre-deleting Service instead of restarting Cilium

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/644
> **Story branch:** `00644-simplify-bn-upgrade-pre-delete-service`
> **PR base:** `main` (branched from `origin/main` @ `f07505c`)
> **PR closes:** #644

## Summary

Replace the post-upgrade snapshot/diff/Cilium-restart machinery from PR #632 with a single pre-upgrade
step that deletes the block-node Helm-owned Services before invoking `helm upgrade`. Helm then
recreates them as part of the upgrade, Cilium sees clean CREATE events (not the dropped UPDATE
transition that caused #619), and the heal logic disappears. Blast radius drops from "every node's
cilium-agent across the cluster" to "one Service per BN release in the BN namespace." The
post-upgrade reachability probe is retained as the safety net.

## Problem

The Cilium-restart approach merged in #632 works, but it touches kube-system and bounces every
cilium-agent in the cluster on a node-by-node rolling basis. While the data plane is mostly
preserved (eBPF maps persist across agent restart), the control plane on each node briefly stops
reconciling — meaning new pod creation, policy updates, and identity allocation are interrupted
across the entire cluster for the ~30s rollout window.

That's a wide blast radius for a Service-scoped bug. Cilium drops the `Service.spec.type` *Update*
event specifically; CREATE events are reconciled correctly. So removing the Service before `helm
upgrade` and letting Helm recreate it puts Cilium on the known-good path with no broader impact.

Pre-existing post-#632 layout:

- `internal/cilium/` package — DS restart helper (`cilium.go:30-52`).
- `internal/kube/client.go:51,66,985,1227` — `KindDaemonSet`, GVR mapping, `IsDaemonSetRolledOut`,
  `RolloutRestartDaemonSet` — only added in #632 to support the Cilium restart.
- `internal/blocknode/reachability.go:27-122` — `SnapshotServices`,
  `BlockNodeServicesChanged`, `RestartCiliumDaemonSetIfServicesChanged`, `diffServiceSpecs`.
- `internal/blocknode/manager.go:60-65` — `preUpgradeServiceSpecs` field on `Manager`.
- `internal/workflows/steps/step_block_node.go:34-35,332-335,738-787` — step IDs, step builders,
  `UpgradeBlockNode` wiring.

`VerifyExternalReachable` + `ProbeTCP` (`internal/blocknode/reachability.go:124-167`,
`internal/network/network.go`) are retained.

## Decisions

| Question | Decision |
|---|---|
| Which Services do we delete? | Helm-owned Services in the BN namespace, scoped by label selector `app.kubernetes.io/managed-by=Helm,app.kubernetes.io/instance=<release>`. Avoids touching anything that wasn't provisioned by the BN Helm release (operator-managed Services, etc.). |
| Headless Service too? | Yes — it's helm-owned and matches the selector. Recreated by helm immediately; DNS via the headless service is briefly unresolvable but pod IPs still work, and the single-replica BN topology doesn't depend on it for gossip. |
| What about Service `clusterIP`? | `clusterIP` is dynamically allocated when omitted from the spec and re-allocated on recreate. In-cluster clients dialing by Service DNS resolve to the new IP transparently. External clients use the LoadBalancer IP, which is preserved when pinned (see next row). |
| What about LoadBalancer IP? | Preserved when pinned via `spec.loadBalancerIP` in chart values. Without a pin, MetalLB re-allocates from its pool — typically the same IP returns immediately if the pool has only one IP (the common single-host UTM-VM case). Operators who need cross-upgrade stability should pin. Documented in the issue body and the umbrella PR description. |
| Outage window on benign upgrades? | Yes — brief, on every upgrade, even when the spec didn't change. Order of 5-15s for delete → helm create → MetalLB re-ARP → Cilium reconcile. Accepted: BN upgrade already rolls the StatefulSet pod, so the Service-side gap overlaps with existing disruption. |
| Where in the workflow does the delete go? | Inside `upgradeBlockNode`'s Execute function, AFTER `BuildMigrationWorkflow` / `ComputeValuesFile` succeed and IMMEDIATELY BEFORE the helm operation (both migration and normal paths). Not a standalone saga step — collapsing it into `upgradeBlockNode` shrinks the failure window between delete and helm to a function call and keeps preflight failures non-destructive. Surfaced as Copilot-review feedback on the initial draft (#645) and consolidated into the same commit. |
| Does this run on first install? | No. `SetupBlockNode` doesn't call the new step — there's nothing to delete. Probe alone is enough on install (same posture #632 took for the Cilium-restart-on-install question). |
| Does this run on `reconfigure`? | Yes, transitively. `reconfigure_handler.go` calls `UpgradeBlockNode(ins)` (`reconfigure_handler.go:71,89,107,112`), so the new pre-delete step rides along automatically. Same for `upgrade_handler.go:95,98`. |
| What about migrations (breaking chart changes)? | `upgradeBlockNode` step builds a migration workflow first (`step_block_node.go:362-383`), then falls through to `UpgradeChart`. The pre-delete step runs *before* `upgradeBlockNode` in the saga, so migrations and normal upgrades both get the same pre-delete treatment. Migrations that already delete the StatefulSet for `volumeClaimTemplates` changes (`chart.go:101-116`) are unaffected. |
| How do we delete? | Add `Manager.DeleteHelmOwnedServices(ctx) error` in `internal/blocknode/chart.go`. Lists Services in the namespace via `kubeClient.List(...KindService...)`, filters by `metadata.labels[app.kubernetes.io/managed-by]==Helm && metadata.labels[app.kubernetes.io/instance]==<release>`, deletes each via the dynamic client. No new generic kube-client helper — keeps the surface area small. |
| Wait for deletion to complete before `helm upgrade`? | Yes — use `kubeClient.WaitForResourcesDeletion(ctx, KindService, ns, timeout, opts)` with the same label selector. Avoids a race where helm's create sees the still-deleting Service and either errors or skips. 30s timeout. |
| What if no Services match (fresh state, or already-deleted by a prior failed run)? | No-op success. The wait-for-deletion call against an already-empty list returns immediately. |
| Do we need to remove the cilium-restart annotation `kubectl.kubernetes.io/restartedAt` we added? | No — the Cilium DS lives in kube-system and isn't part of any helm release we manage. Once the call site is removed, the function and the annotation it would have applied stop existing. |
| Removed surface: keep `internal/cilium/` for any future use? | Delete it. No other caller. If a future story needs DS-restart helpers, re-introduce then with current requirements. |
| Removed surface: keep `kube.RolloutRestartDaemonSet` / `kube.IsDaemonSetRolledOut` / `kube.KindDaemonSet`? | Delete them — they were added specifically for the Cilium restart in PR #632 (confirmed by `git log`). `KindDaemonSet` is referenced only by the cilium package and the daemonset tests. If a future story needs DS rollout primitives, restore the same way. |
| Keep `VerifyExternalReachable` + `ProbeTCP`? | Yes — they catch silent failures regardless of root cause and are independent of the heal mechanism. Their value is "loud error when traffic is broken" not "Cilium-specific recovery." |
| Should the umbrella `BlockNodePublicPort` constant move? | No — it stays in `reachability.go` since it's still used by `findLoadBalancerEndpoint`. |
| What about the `ReachabilityProbe*` constants and `preUpgradeServiceSpecs` field? | Constants stay (the probe still uses them). `preUpgradeServiceSpecs` field removed from `Manager`. |
| Logging at the new step? | Info-level: "Deleting helm-owned BN Services before upgrade (N services)" with the names. Debug-level: per-service delete confirmation. |

## Scope

### `internal/blocknode/chart.go`
- [ ] Add `Manager.DeleteHelmOwnedServices(ctx) error` — list-filter-delete-wait sequence. List BN-namespace Services, filter by `managed-by=Helm` + `instance=<release>`, dynamically delete each, then `WaitForResourcesDeletion` with the label selector.

### `internal/blocknode/manager.go`
- [ ] Remove `preUpgradeServiceSpecs` field from `Manager`. Update the struct comment block accordingly.

### `internal/blocknode/reachability.go`
- [ ] Remove `SnapshotServices`, `BlockNodeServicesChanged`, `diffServiceSpecs`, `RestartCiliumDaemonSetIfServicesChanged`. Keep `BlockNodePublicPort`, `VerifyExternalReachable`, `findLoadBalancerEndpoint`. Update file-level doc comment if any.
- [ ] Remove the `internal/cilium` import.

### `internal/blocknode/reachability_test.go`
- [ ] Remove `TestDiffServiceSpecs` (and any other tests touching the removed surface). Keep `findLoadBalancerEndpoint` / probe tests.

### `internal/cilium/` (package deleted)
- [ ] Delete `internal/cilium/cilium.go` and `internal/cilium/cilium_test.go`.

### `internal/kube/client.go`
- [ ] Remove `KindDaemonSet` from the `ResourceKind` enum and from the GVR mapping table (lines 51, 66).
- [ ] Remove `IsDaemonSetRolledOut` (lines 978-985).
- [ ] Remove `RolloutRestartDaemonSet` (line 1227).

### `internal/kube/daemonset_test.go` (file deleted)
- [ ] Delete the whole file — all of its tests cover the removed helpers.

### `internal/workflows/steps/step_block_node.go`
- [ ] Remove `SnapshotBlockNodeServicesStepId` and `RestartCiliumIfServicesChangedStepId` constants and their step builders.
- [ ] Inside `upgradeBlockNode`'s Execute, call `manager.DeleteHelmOwnedServices(ctx)` *after* `BuildMigrationWorkflow`/`ComputeValuesFile` succeed and *immediately before* the helm operation. Mirror the call on both branches (migration sub-workflow and normal `UpgradeChart`).
- [ ] Update `UpgradeBlockNode` step list to `EnsureHederaOwnerStep, upgradeBlockNode, waitForBlockNode, verifyBlockNodeReachable`.
- [ ] Update the `UpgradeBlockNode` and `upgradeBlockNode` doc comments to describe the in-step delete-then-helm sequencing and reference #644.

### `internal/workflows/steps/step_block_node_test.go`
- [ ] Update any step-ordering assertions that name the removed step IDs. Add coverage for the new step ID where the snapshot/restart steps were previously asserted.

### `internal/bll/blocknode/reconfigure_handler_test.go`
- [ ] Update step-ordering assertions at lines 117, 166, 173, 187 to reflect the new shape (delete-before-upgrade replaces snapshot, no restart-if-changed step).

## Out of scope

- Predictive topology detection (extract Services from `helm template` to decide whether to skip the pre-delete). Mentioned in the issue body as the "smarter" variant; deferred. The unconditional pre-delete is the chosen simplification.
- Mitigating the LoadBalancer-IP-may-change-on-recreate scenario for deployments without a pinned `loadBalancerIP`. Documented as an operator concern; no auto-pin logic added.
- Cilium upstream fix coordination — that's external work, not blocked by this PR.

## Test plan

- [ ] Unit: `go test -race -cover -tags='!integration' ./internal/blocknode/... ./internal/kube/... ./internal/workflows/steps/... ./internal/cilium/...` (the cilium directory shouldn't exist anymore; the test command will just skip a missing package).
- [ ] Specifically:
  - `internal/blocknode/chart_test.go` (new) — `DeleteHelmOwnedServices` with fake client: matches selector, skips unmatched, handles empty result, propagates wait-for-deletion timeout.
  - `internal/workflows/steps/step_block_node_test.go` — `UpgradeBlockNode` step list matches the new order.
  - `internal/bll/blocknode/reconfigure_handler_test.go` — step-ordering assertions updated.
- [ ] Integration (UTM VM): re-run the #619 reproduction recipe (`docs/claude/reviews/00619-block-node-upgrade-mutates-service-topology.md` UAT section). Expected: post-upgrade `cilium service list` shows the new LB entry, TCP probe passes — without any Cilium DS restart.
- [ ] Manual UAT in VM:
  1. Install BN with Shape A values (split `ClusterIP` + `-external` LoadBalancer with pinned IP).
  2. `solo-provisioner block node upgrade --chart-version <next>`.
  3. Observe in workflow output: `delete-block-node-services-before-upgrade` step runs and reports N deleted; `verify-block-node-reachable` passes.
  4. `cilium service list | grep <lb-ip>` → entry present; no Cilium DS pod restart visible in `kubectl get pods -n kube-system`.
  5. `nc -zvw5 <lb-ip> 40840` → open.

## Risks / rollbacks

- **Brief external outage on every upgrade.** Mitigation: post-upgrade probe fails loudly within 60s if anything goes wrong, and BN upgrade is already a disruptive operation.
- **MetalLB IP churn on pools with >1 IP.** Mitigation: documented, operators can pin `loadBalancerIP`.
- **Helm upgrade fails after we already deleted Services.** State recovery: the next `block node upgrade` invocation either succeeds (helm picks up where it left off and recreates the missing Services) or fails the same way again with a loud probe failure. No silent data loss — BN data lives in PVs untouched by this change.
- **Rollback path:** revert this PR; the merged-state from #632 (snapshot + Cilium restart) is the previous known-good behavior.
