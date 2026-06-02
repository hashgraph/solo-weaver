# #619 — Block node upgrade mutates Service topology and breaks external connectivity

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/619
> **Story branch:** `00619-block-node-upgrade-mutates-service-topology`
> **PR base:** `main` (branched from `origin/main` @ `c200d30`)
> **PR closes:** #619

## Summary

`solo-provisioner block node upgrade` can leave a block-node Service unreachable from outside the
cluster: when Helm changes `Service.spec.type` during upgrade (e.g. Shape A `ClusterIP` →
Shape B `LoadBalancer`), Cilium's eBPF service reconciler misses the mutation event and never
installs the LoadBalancer DNAT rule. Symptoms look healthy (`kubectl get svc` shows the LB IP,
endpoints are populated, pod is `Ready`) but `cilium service list` has no entry and external
traffic blackholes.

Fix: snapshot the BN-namespace Services right before `helm upgrade`, diff against the post-upgrade
state, and restart the Cilium DaemonSet **only when something actually changed** so a normal
chart-version bump doesn't pay the ~30s restart cost. Always run an in-cluster reachability probe
after the upgrade so any remaining failure mode (Cilium, MetalLB, chart, firewall) surfaces as an
immediate workflow error instead of silent breakage.

## Problem

Confirmed end-to-end in a UTM VM with `block-node-server-0.33.0`, Cilium `kubeProxyReplacement`
enabled, `bpf-lb-mode: dsr`. After a successful `helm upgrade`:

- `kubectl get svc -n block-node` → LB IP present, endpoints populated, pod `Ready`.
- `cilium service list | grep <lb-ip>` → no entry.
- `nc -zvw5 <lb-ip> 40840` (in-cluster or external) → timeout.
- `kubectl rollout restart ds/cilium -n kube-system` → entry restored, traffic flows immediately.

The Cilium-reconciler bug is the root cause; restarting the DaemonSet is the documented workaround
and the only operation that reliably restores the eBPF entry.

## Decisions

| Question | Decision |
|---|---|
| Why not preserve Service topology (the more invasive fix proposed in the issue)? | Out of scope. Cilium restart heals the symptom regardless of what caused the mutation, and the team prefers the simpler workflow change over a value-injector that mirrors live cluster state. Trade-off noted under "Out of scope". |
| When does the Cilium restart fire? | Only when the diff of BN-namespace Service `spec` maps between pre-upgrade and post-upgrade snapshots is non-empty (Service added, removed, or `spec` mutated). The common case — chart-version bump that doesn't touch Services — skips the restart. Reconfigure inherits this via `UpgradeBlockNode`. |
| What does "spec changed" cover? | `reflect.DeepEqual` on the full `spec` sub-map per Service (covers `type`, `ports`, `loadBalancerIP`, `clusterIP`, etc.). `status` is intentionally excluded so MetalLB status updates during the upgrade don't trip the diff. |
| Where is the pre-upgrade snapshot stored? | On the shared `*blocknode.Manager` instance (the workflow uses `newBlockNodeManagerProvider` to keep one Manager across all steps). No automa step-state passing needed. |
| What if the snapshot was never taken (defensive)? | `BlockNodeServicesChanged` returns `true` — the workflow can't prove nothing changed, so the safe choice is to restart Cilium. |
| Cilium DS location? | `ds/cilium` in namespace `kube-system` — same namespace weaver installs into via `internal/templates/files/cilium/`. Verified by reading the embedded chart. |
| What if Cilium is absent? | Treat as a fatal step error. Cilium is provisioner-installed for every profile (`internal/templates/files/cilium/`), so its absence indicates a broken cluster, not a valid runtime state. No silent skip. |
| Restart cost? | ~30s on a single-node cluster. eBPF programs stay loaded, established connections survive — only the agent control plane restarts. Acceptable for a one-time-per-upgrade workflow step. |
| Wait for rollout completion? | Yes. Block on `kubectl rollout status ds/cilium -n kube-system` with a 90s timeout before continuing. A half-restarted DS could leave the probe racing the reconciler. |
| Probe target IP — `spec.loadBalancerIP` or `status.loadBalancer.ingress[0].ip`? | `status.loadBalancer.ingress[0].ip` — that's what MetalLB actually announced. `spec.loadBalancerIP` is the request; status is reality. |
| Probe port? | Hard-code `BlockNodePublicPort = 40840`. Looking the port up on the Service is fragile: chart versions have used `http`, `grpc`, and other names for it. The port number 40840 is an ecosystem-wide contract (the chart default, every SDK assumes it) so dialing it directly is more robust than introspecting Service shape. Operator port overrides via custom values aren't supported by the probe — if you change the port number, the probe will dial the wrong one. Accepted trade-off. |
| Probe implementation? | Go `net.Dialer.DialContext` from the solo-provisioner process. Retry-with-deadline loop: per-attempt 10s dial timeout, 2s backoff between failed attempts, 60s overall budget. No pod, no template, no kube apply/wait/delete overhead. Traffic still traverses the MetalLB-ARP + Cilium-DNAT path so the failure modes from #619 surface identically. |
| Why dial from the host instead of an in-cluster pod? | solo-provisioner is a node-local tool (it does mount/systemd ops on the cluster node itself); the host shares the cluster's network path. Dialing from inside a pod adds ~5-10s per probe and a busybox image dependency for zero correctness gain in this deployment topology. |
| Trade-off of host-dial vs pod-dial? | If solo-provisioner is ever run from a host without IP routing to the cluster (e.g. a bastion with kubectl access only), the dial would always fail. Considered acceptable: not the documented operator workflow, and the failure would be loud and obvious. |
| Which Service does the probe target? | Whichever LoadBalancer Service exists: `<release>-block-node-server` (Shape B, weaver default) or `<release>-block-node-server-external` (Shape A). Look up by listing Services in the BN namespace and picking the one with `spec.type == LoadBalancer`. |
| Skip the probe on `local` profile? | Yes. `local` profile keeps `loadBalancer.enabled: false` and has no MetalLB pool; `Service.status.loadBalancer.ingress` is never populated. Step no-ops when `LoadBalancerEnabled == false`. |
| Add the probe to `SetupBlockNode` (install) too? | Yes — defense in depth. Install also goes through Helm and could in principle hit the same Cilium reconciler bug on a fresh deploy if anything in the chart or the cluster state interacts unexpectedly. Same no-op guard for `local`. |
| Cilium restart on install? | No. Install creates the Service from scratch; there's no pre-existing eBPF entry for the reconciler to miss. Probe alone is enough. |

## Scope

### `internal/kube/client.go`
- [ ] Add `RolloutRestartDaemonSet(ctx, namespace, name) error` if no equivalent exists. (`AnnotateResource` is already there for Services; the existing `ScaleStatefulSet` + `patchReplicas` give a precedent for the patch pattern.) Implementation: patch the DS with a `kubectl.kubernetes.io/restartedAt` annotation on `spec.template.metadata.annotations`, same mechanic as `kubectl rollout restart`.
- [ ] Add `WaitForDaemonSetRollout(ctx, namespace, name, timeout) error` — polls `status.observedGeneration >= generation` AND `status.numberReady == status.desiredNumberScheduled`.
- [ ] Add `ListServices(ctx, namespace) ([]unstructured.Unstructured, error)` if not already present — needed by the probe to find the LB Service without hard-coding the Shape A/B suffix. (`List(ctx, KindService, ns, opts)` exists at `client.go:606` — verify it's usable for this; if so, no new method needed.)

### `internal/network/network.go`
- [x] Add `ProbeTCP(ctx, addr, overallTimeout, dialTimeout, retryDelay) (int, error)` — generic retry-with-deadline TCP dial helper. Sits next to the existing `CheckEndpointReachable` (HTTP). Returns attempts + last error. Reusable beyond block-node.

### `internal/cilium/cilium.go` (new package)
- [x] `AgentDaemonSetNamespace = "kube-system"`, `AgentDaemonSetName = "cilium"`, `DefaultRolloutTimeout = 90s` — constants pinning the kube-API location of weaver's Cilium install.
- [x] `RestartAgentDaemonSet(ctx, kubeClient, timeout) error` — triggers the rolling restart and waits for completion. Keeps Cilium-specific knowledge out of the BN package.

### `internal/blocknode/reachability.go` (new file)
- [x] `SnapshotServices(ctx)` — lists Services in the BN namespace and stashes a `name → spec` map on the Manager.
- [x] `BlockNodeServicesChanged(ctx)` — re-lists Services post-upgrade and runs `diffServiceSpecs` against the snapshot.
- [x] `RestartCiliumDaemonSetIfServicesChanged(ctx)` — BN policy layer: checks the diff and delegates to `cilium.RestartAgentDaemonSet` when needed.
- [x] `VerifyExternalReachable(ctx)` — calls `network.ProbeTCP` against the LB endpoint. No pod creation, no busybox.
- [x] `findLoadBalancerEndpoint(ctx)` + `readNamedPort(svc, name)` helpers (BN-specific).
- [x] `diffServiceSpecs(before, after)` — pure helper used by `BlockNodeServicesChanged`. Lives here for test reach.
- [x] `findLoadBalancerEndpoint(ctx)` + `readNamedPort(svc, name)` helpers.
- [x] `diffServiceSpecs(before, after)` — pure helper used by `BlockNodeServicesChanged`. Lives here for test reach.

### `internal/workflows/steps/step_block_node.go`
- [x] Add `SnapshotBlockNodeServicesStepId` + `snapshotBlockNodeServices` step builder.
- [x] Add `RestartCiliumIfServicesChangedStepId` + `restartCiliumIfServicesChanged` step builder.
- [x] Add `VerifyBlockNodeReachableStepId` + `verifyBlockNodeReachable` step builder.
- [x] Wire `UpgradeBlockNode` as: `EnsureHederaOwnerStep → snapshotBlockNodeServices → upgradeBlockNode → waitForBlockNode → restartCiliumIfServicesChanged → verifyBlockNodeReachable`.
- [x] Wire `SetupBlockNode` to append `verifyBlockNodeReachable` after `waitForBlockNode`. **No** snapshot or Cilium restart on install (fresh deploy has no pre-existing eBPF entry to miss).

### `internal/bll/blocknode/reconfigure_handler.go`
- [ ] No direct changes needed — every reconfigure branch already terminates in `UpgradeBlockNode` or its variants, so the new steps are inherited automatically.

### Tests

#### Unit
- [x] `internal/kube/daemonset_test.go` — `IsDaemonSetRolledOut` (7 cases) + `KindDaemonSet` GVR round-trip.
- [x] `internal/network/network_test.go` — `ProbeTCP` against a real local TCP listener: success-first-attempt, repeated-failure-respects-budget, parent-context-cancel-exits-promptly.
- [x] `internal/cilium/cilium_test.go` — guards `AgentDaemonSetNamespace`/`AgentDaemonSetName` constants and the `DefaultRolloutTimeout` sanity range.
- [x] `internal/blocknode/reachability_test.go` — `diffServiceSpecs` (added, removed, spec-flipped, identical, both-empty) + `readNamedPort` (found, absent, malformed, missing `spec.ports`).

#### Integration (UTM VM)
- [ ] Existing BN upgrade integration test continues to pass with the two new steps in the workflow.
- [ ] New: `Test_BlockNode_Upgrade_NoServiceChange_SkipsCilium_Integration` — chart-version-only bump asserts `ds/cilium` `restartedAt` annotation is **unchanged** from before the upgrade.
- [ ] New: `Test_BlockNode_Upgrade_ServiceShapeFlip_RestartsCilium_Integration` — Shape A → Shape B upgrade asserts a fresh `restartedAt` annotation on `ds/cilium` and a populated `cilium service list` entry for the LB IP.

## Out of scope

- **Service topology preservation.** Clusters originally installed in Shape A (split `ClusterIP` +
  `-external` LoadBalancer with pinned `loadBalancerIP`) will continue to be flipped to Shape B on
  upgrade. The Cilium restart heals the connectivity blackhole symptom; if the operator pinned a
  specific `loadBalancerIP` on the `-external` Service, MetalLB may reallocate a different IP from
  the pool. Operators who need pinned-IP semantics must encode that in a custom `--values-file`
  that produces Shape B with the IP pinned in `loadBalancer.loadBalancerIP`. This is an acceptable
  trade for the simpler implementation; a follow-up issue can be opened if the multi-IP-pool
  scenario surfaces in production.
- Upstream Cilium fix for the eBPF reconciler-miss. Worth filing against `cilium/cilium` separately
  but this PR does not depend on an upstream change.
- CLI flag to opt out of the probe or Cilium restart. The probe is fast; the restart is bounded at
  ~30s. No escape hatch unless a real deployment surfaces a false-positive.

## Test plan

- [ ] Unit: `task test:unit:verbose` — `./internal/workflows/steps/...` covering the new step builders.
- [ ] Integration (UTM VM): `task vm:test:integration TEST_NAME='^Test_BlockNode_Upgrade_Restarts_Cilium_Integration$'`.
- [ ] Manual UAT (UTM VM), per the issue's reproduction recipe:
  1. Configure MetalLB pool, restart speaker.
  2. `solo-provisioner block node install --values shape-a.yaml`.
  3. `cilium service list` shows the LB entry.
  4. `solo-provisioner block node upgrade --chart-version 0.33.0`.
  5. Post-upgrade: `cilium service list | grep <lb-ip>` returns the entry; `nc -zvw5 <lb-ip> 40840` from an in-cluster pod succeeds; the workflow output shows the new "Restarting Cilium DaemonSet" and "Verifying Block Node reachability" steps.
- [ ] Confirm the existing `uat:core` flow (Shape B clusters) is unaffected end-to-end — the two new steps run and pass without changing any pre-existing expectations.

## Risks / rollbacks

- **Risk:** Cilium DS restart briefly drops the control plane for the agent. Existing connections survive because eBPF programs stay loaded, but a service-creation race during the restart could be flaky. **Mitigation:** we wait for the rollout to complete before the probe runs, and the conditional gating means the restart only happens on upgrades that actually changed a Service.
- **Risk:** `diffServiceSpecs` is `reflect.DeepEqual` over `spec`. A semantically-identical re-marshal (e.g. map ordering, integer typing) could theoretically register as a diff and trigger an unnecessary restart. **Mitigation:** the snapshot reads through the dynamic client (same code path as the post-upgrade read), so any normalization is symmetric. Worst case is a spurious restart, not silent breakage.
- **Risk:** The probe could fail on slow MetalLB ARP convergence and break upgrades that would otherwise succeed eventually. **Mitigation:** 60s overall deadline + the Cilium restart finishes before the probe starts. Bump the deadline as a constant if CI flake appears.
- **Risk:** Restarting Cilium on a cluster where Cilium is unhealthy (e.g. mid-bootstrap) could destabilize traffic during the rollout window. **Mitigation:** the rollout-wait surfaces unhealthy-DS as a step failure rather than masking it.
- **Risk:** Hard-coded `kube-system / cilium` may diverge from a future install where Cilium is installed under a different name. **Mitigation:** weaver itself installs Cilium with those names (`internal/templates/files/cilium/`); update both together if that ever changes.
- **Rollback:** Revert the PR. No persistent state changes, no migrations, no schema changes. Behaviour reverts cleanly to the pre-fix workflow on next upgrade.
