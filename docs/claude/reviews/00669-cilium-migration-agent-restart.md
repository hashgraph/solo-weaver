# 00669 — Separate v0.19.2 migration to restart Cilium agents

## Problem

The v0.19.1 migration (`migration_cilium_acceleration.go`, #689) runs
`cilium upgrade --values` to flip already-provisioned clusters from
`loadBalancer.acceleration: best-effort` to `disabled` (#669/#674).

Verified on the testnet `blk` hosts after rolling v0.19.1: the migration ran and
the `cilium-config` ConfigMap flipped to `disabled` (helm release `cilium` → rev
2), **but the XDP program stayed attached to the public NIC** (`prog/xdp …
cil_xdp_entry` on `eno1`) and the cilium agent pod never restarted (age 25h, 0
restarts).

Root cause: the Cilium chart's agent pod template has **no `checksum/config`
annotation**, so a ConfigMap-only `helm upgrade` does not roll the DaemonSet. The
agent reads `bpf-lb-acceleration` only at startup — so the new value is staged but
never applied; XDP is not detached and the ixgbe PHY-reset flap risk remains.

## Solution

Add a **separate** startup migration, `CiliumAgentRestartMigration`, gated on the
**v0.19.2** version boundary, that restarts the Cilium agents and waits for the
rollout. It is intentionally not folded into the v0.19.1 migration:

- The `blk` hosts already ran the v0.19.1 migration (`installed == 0.19.1`), so
  the v0.19.1 migration will not re-fire on them. A new 0.19.2-gated migration
  *does* fire on the 0.19.1 → 0.19.2 upgrade and restarts the staged-but-not-
  applied agents.
- Registered **after** `CiliumAccelerationMigration`, so a `0.18.x → 0.19.2`
  upgrade flips the config (0.19.1 migration) and then restarts the agents (this
  migration) in a single pass.

`Execute` only restarts when Kubernetes and Cilium are installed and the live
acceleration is already `disabled` (the acceleration migration owns flipping the
config). The restart uses the provisioner's own client:

- `kube.Client.RolloutRestart(ctx, KindDaemonSet, "kube-system", "cilium")` —
  stamps the pod template's `kubectl.kubernetes.io/restartedAt` annotation (same
  mechanism as `kubectl rollout restart`). Adds `KindDaemonSet` + GVR.
- `kube.Client.WaitForResource(..., daemonSetRolledOut, 5m)` — waits until the
  DaemonSet has observed the new spec and every scheduled pod is updated + ready.
  The check returns `(false, nil)` on transient get errors, so a single NIC blip
  during XDP detach does not abort the wait.

A failed restart fails `Execute` rather than silently leaving XDP attached.

## Changed files

| File | Change |
| --- | --- |
| `internal/workflows/migration_cilium_agent_restart.go` | New `CiliumAgentRestartMigration` (startup, minVersion 0.19.2) + `defaultRestartCiliumAgents` + `daemonSetRolledOut`. |
| `internal/workflows/migration_cilium_agent_restart_test.go` | Tests for `Applies` (0.19.2 boundary) and `Execute` (k8s/cilium absent, unreachable, not-disabled, disabled→restart, restart-failure). |
| `internal/kube/client.go` | Add `KindDaemonSet` (+ GVR) and `RolloutRestart`. |
| `cmd/cli/commands/root.go` | Register `NewCiliumAgentRestartMigration` after `NewCiliumAccelerationMigration`. |

The v0.19.1 `CiliumAccelerationMigration` is unchanged (config flip only).

## Review checklist

- [ ] `minVersion` is `0.19.2` (so it fires on 0.19.1 → 0.19.2).
- [ ] Registered **after** `CiliumAccelerationMigration`.
- [ ] Restart runs only when k8s + cilium are installed **and** acceleration is
      already `disabled`; no-op otherwise.
- [ ] `RolloutRestart` patches `.spec.template.metadata.annotations`
      (`restartedAt`), not the resource's own metadata.
- [ ] `daemonSetRolledOut` returns `(false, nil)` on transient errors so the wait
      survives a NIC blip during XDP detach.
- [ ] A failed restart/rollout fails `Execute`.

## Tests

```bash
go test ./internal/workflows/... -run 'CiliumAgentRestart|CiliumAcceleration' -count=1
go test ./internal/kube/... -run RolloutRestart -count=1
```

## Manual UAT

On a `blk` host showing staged-but-not-applied (`bpf-lb-acceleration: disabled`
in the ConfigMap but `prog/xdp` still on `eno1`), upgrade the provisioner to a
build with this change and run any provisioner command, then confirm:

```bash
KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n kube-system get pods -l k8s-app=cilium  # age reset
ip -d link show eno1 | grep -i xdp || echo "no xdp on eno1 (good)"
KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n kube-system \
  get cm cilium-config -o jsonpath='{.data.bpf-lb-acceleration}'; echo   # disabled
```
