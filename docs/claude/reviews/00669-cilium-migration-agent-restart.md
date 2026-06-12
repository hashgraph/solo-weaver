# 00669 — Cilium acceleration migration must restart the agents

## Problem

The startup migration added in #689 re-renders `cilium-config.yaml` and runs
`cilium upgrade --values <config> --wait` to flip already-provisioned clusters
from `loadBalancer.acceleration: best-effort` to `disabled` (#669/#674).

Observed on the testnet `blk` hosts after rolling the fix: the migration ran and
the `cilium-config` ConfigMap flipped to `disabled` (helm release `cilium` → rev
2, deployed), **but the XDP program stayed attached to the public NIC**
(`prog/xdp id … cil_xdp_entry` on `eno1`) and the cilium agent pod never
restarted (age 25h, 0 restarts).

Root cause: the Cilium chart's agent pod template carries **no `checksum/config`
annotation**, so a ConfigMap-only `helm upgrade` does not roll the DaemonSet. The
agent reads `bpf-lb-acceleration` only at startup, so without a restart the new
`disabled` value is staged but never applied to the datapath — XDP is not
detached and the ixgbe PHY-reset flap risk remains.

## Solution

After `cilium upgrade` succeeds, the migration now restarts the Cilium agents and
waits for the rollout, so the agents re-read the config and detach XDP:

- `kube.Client.RolloutRestart(ctx, KindDaemonSet, "kube-system", "cilium")` —
  stamps the pod template's `kubectl.kubernetes.io/restartedAt` annotation (the
  same mechanism as `kubectl rollout restart`) via the dynamic client.
- `kube.Client.WaitForResource(..., daemonSetRolledOut, 5m)` — waits until the
  DaemonSet has observed the new spec and every scheduled pod is updated and
  ready. The check tolerates transient get errors (detaching XDP can briefly blip
  the NIC the API server rides on), so a single flap during detach does not abort
  the wait.

If the restart/rollout fails, `Execute` returns the error rather than silently
leaving XDP attached.

`KindDaemonSet` (+ its `apps/v1 daemonsets` GVR) is added to the kube client.

## Changed files

| File | Change |
| --- | --- |
| `internal/workflows/migration_cilium_acceleration.go` | After `cilium upgrade`, call `restartCiliumAgents` (rollout restart + wait). New `defaultRestartCiliumAgents` + `daemonSetRolledOut` CheckFunc; `restartCiliumAgents` seam. |
| `internal/workflows/migration_cilium_acceleration_test.go` | Assert the restart runs only on the best-effort path (not when k8s/cilium absent, unreachable, or already disabled) and that a restart failure fails the migration. |
| `internal/kube/client.go` | Add `KindDaemonSet` (+ GVR) and `RolloutRestart`. |

## Review checklist

- [ ] Restart runs **only** after a successful `cilium upgrade` (best-effort
      path), never on the skip paths.
- [ ] `RolloutRestart` patches `.spec.template.metadata.annotations`
      (`restartedAt`), not the resource's own metadata.
- [ ] `daemonSetRolledOut` returns `(false, nil)` on transient/get errors so the
      wait survives a NIC blip during XDP detach.
- [ ] A failed restart/rollout fails `Execute` (no silent "staged but not
      applied").

## Tests

```bash
go test ./internal/workflows/... -run CiliumAcceleration -count=1
go test ./internal/kube/... -run RolloutRestart -count=1
```

## Manual UAT

On a `blk` host still showing staged-but-not-applied (`bpf-lb-acceleration:
disabled` in the ConfigMap but `prog/xdp` still on `eno1`), run a provisioner
command with this build and confirm:

```bash
# Agent restarts (age resets), config applied, XDP gone, NIC stable.
KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n kube-system get pods -l k8s-app=cilium
ip -d link show eno1 | grep -i xdp || echo "no xdp on eno1 (good)"
KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n kube-system \
  get cm cilium-config -o jsonpath='{.data.bpf-lb-acceleration}'; echo   # disabled
```
