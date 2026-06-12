# 00669 — Disable Cilium XDP load-balancer acceleration

## Problem

`kube cluster install` rendered the embedded Cilium values
(`internal/templates/files/cilium/cilium-config.yaml`) with
`loadBalancer.acceleration: "best-effort"` and `routingMode: native` with no
explicit `devices:` list. With `best-effort`, Cilium auto-detects native
datapath devices — including the host's public/default-route NIC — and attaches
an **XDP** program to each.

On `ixgbe` NICs (e.g. Intel X550) the driver performs a PHY/link reset whenever
an XDP program is attached or the agent reconciles. This flapped the public NIC's
carrier continuously (Gained → Lost every ~10s). Two consequences observed on a
block-node host:

1. `systemd-networkd` never finished configuring the link, so the node's static
   public IP was never installed — the host lost public connectivity.
2. Sustained flapping tripped the **upstream switch's link-flap err-disable**,
   which shut the port. That state lives on the switch and **survives a host
   reboot**, so the host stayed offline until the provider re-enabled the port.

Because the reconcile daemon re-applies the embedded config, there was no durable
operator workaround — the fix has to be in the shipped template.

## Solution

Set `loadBalancer.acceleration: "disabled"` so Cilium uses **tc-eBPF** instead of
XDP. tc-eBPF performs the same DSR/maglev load balancing without attaching an XDP
program to any NIC, so the `ixgbe` PHY is never reset and the public NIC cannot
flap. A comment documents the rationale and references this issue.

This is a safe default across mixed hardware: it removes the XDP hardware-offload
fast path (a throughput optimization) but preserves full kube-proxy-replacement /
NodePort / LB functionality.

## Changed files

| File | Change |
| --- | --- |
| `internal/templates/files/cilium/cilium-config.yaml` | `loadBalancer.acceleration` changed `"best-effort"` → `"disabled"`; added a comment explaining the XDP/ixgbe PHY-reset + switch err-disable rationale (refs #669). |
| `docs/claude/reviews/00669-disable-xdp-acceleration.md` | This review guide. |

## Review checklist

- [ ] `acceleration` is exactly `"disabled"` (not removed, not `"native"`).
- [ ] No other Cilium values changed (mode `dsr`, dsrDispatch `opt`, algorithm
      `maglev`, l7 backend `disabled`, kubeProxyReplacement, etc. all unchanged).
- [ ] No Go code or tests reference the old `best-effort` value
      (`grep -rn "best-effort\|bpf-lb-acceleration" --include='*.go'`).
- [ ] Template still renders (the `{{.MachineIP}}` / `{{.SandboxDir}}` /
      `{{.SandboxLocalBinDir}}` substitutions are untouched).

## Tests

```bash
# Confirm no code/tests assert the removed value:
grep -rniE "best-effort|bpf-lb-acceleration" --include="*.go" --include="*.yaml" . | grep -v vendor/

# Cilium installer unit/integration tests (render + configure):
go test ./pkg/software/... -run Cilium -count=1
go test ./internal/workflows/steps/... -run Cilium -count=1
```

## Manual UAT

On a node with an `ixgbe` public NIC (or any kubeadm node):

```bash
# 1. Install a cluster with this build.
sudo solo-provisioner kube cluster install --profile <profile> --node-type <type> \
  --config <sandbox>/etc/weaver/solo-provisioner.yaml --non-interactive

# 2. The rendered values must show acceleration disabled.
grep -A1 acceleration <sandbox>/etc/weaver/cilium-config.yaml
#   acceleration: "disabled"

# 3. The live Cilium config must agree.
KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n kube-system \
  get cm cilium-config -o jsonpath='{.data.bpf-lb-acceleration}'; echo
#   disabled

# 4. No XDP program is attached to any host NIC.
for n in $(ls /sys/class/net | grep -E '^eno|^enp|^eth'); do
  echo -n "$n: "; ip -d link show "$n" | grep -qi xdp && echo "XDP ATTACHED (bad)" || echo "no xdp (good)"
done

# 5. The public NIC keeps carrier — no flapping.
journalctl -u systemd-networkd -b | grep -iE 'eno[0-9].*(Gained|Lost) carrier' | tail
#   expect: NOT a repeating Gained/Lost loop on the public NIC
```

Expected: cluster comes up healthy (`cilium status` OK, nodes Ready), the public
NIC stays up with its address, and no NIC carries an XDP program.
