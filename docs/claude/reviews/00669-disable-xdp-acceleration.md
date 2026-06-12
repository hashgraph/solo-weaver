# 00669 — Use generic (testing-only) Cilium XDP to avoid ixgbe PHY reset

## Problem

`kube cluster install` rendered the embedded Cilium values
(`internal/templates/files/cilium/cilium-config.yaml`) with
`loadBalancer.acceleration: "best-effort"` and `routingMode: native` with no
explicit `devices:` list. With `best-effort`, Cilium auto-detects native
datapath devices — including the host's public/default-route NIC — and attaches
a **native (driver-mode) XDP** program to each.

On `ixgbe` NICs (e.g. Intel X550) the driver performs a PHY/link reset whenever
a native XDP program is attached or the agent reconciles. This flapped the public
NIC's carrier continuously (Gained → Lost every ~10s). Two consequences observed
on a block-node host:

1. `systemd-networkd` never finished configuring the link, so the node's static
   public IP was never installed — the host lost public connectivity.
2. Sustained flapping tripped the **upstream switch's link-flap err-disable**,
   which shut the port. That state lives on the switch and **survives a host
   reboot**, so the host stayed offline until the provider re-enabled the port.

Because the reconcile daemon re-applies the embedded config, there was no durable
operator workaround — the fix has to be in the shipped template.

## Solution

Set `loadBalancer.acceleration: "testing-only"` so Cilium attaches XDP in
**generic (SKB) mode** rather than native driver mode. Generic XDP runs the
program in the kernel network stack instead of the NIC driver's receive path, so
the `ixgbe` PHY is never reset and the public NIC cannot flap — while still
keeping an XDP-based load-balancer datapath (DSR/maglev unchanged).

`"testing-only"` is Cilium's own value for generic XDP (`XDPModeGeneric` in
`pkg/option`). The Cilium v1.18 Helm chart passes `loadBalancer.acceleration`
verbatim into the `bpf-lb-acceleration` ConfigMap key and its `values.schema.json`
types the field as a plain string (no enum), so the value installs cleanly.

Trade-off: generic XDP is Cilium's CI/testing path — it does not bypass GRO and
cannot linearize every skb, so it forgoes the native-XDP hardware-offload
throughput gain. It is chosen here deliberately for `ixgbe` link stability over
raw XDP offload performance. (If offload is not needed at all,
`acceleration: "disabled"` / tc-eBPF is the alternative — see history of #669.)

## Changed files

| File | Change |
| --- | --- |
| `internal/templates/files/cilium/cilium-config.yaml` | `loadBalancer.acceleration` changed `"best-effort"` → `"testing-only"` (generic/SKB XDP); comment rewritten to explain the native-XDP/ixgbe PHY-reset + switch err-disable rationale and the generic-XDP mitigation (refs #669). |
| `docs/claude/reviews/00669-disable-xdp-acceleration.md` | This review guide. |

## Review checklist

- [ ] `acceleration` is exactly `"testing-only"` (not `"native"`, `"best-effort"`,
      or removed).
- [ ] No other Cilium values changed (mode `dsr`, dsrDispatch `opt`, algorithm
      `maglev`, l7 backend `disabled`, kubeProxyReplacement, etc. all unchanged).
- [ ] No Go code or tests assert the old `best-effort` value
      (`grep -rn "best-effort\|bpf-lb-acceleration" --include='*.go'`).
- [ ] Template still renders (the `{{.MachineIP}}` / `{{.SandboxDir}}`
      substitutions are untouched).

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

# 2. The rendered values must show acceleration testing-only.
grep -A1 acceleration <sandbox>/etc/weaver/cilium-config.yaml
#   acceleration: "testing-only"

# 3. The live Cilium config must agree (helm accepted the value — no schema reject).
KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n kube-system \
  get cm cilium-config -o jsonpath='{.data.bpf-lb-acceleration}'; echo
#   testing-only

# 4. XDP is attached in GENERIC/SKB mode (not native driver mode).
for n in $(ls /sys/class/net | grep -E '^eno|^enp|^eth'); do
  echo -n "$n: "; ip -d link show "$n" | grep -oiE 'xdp(generic|drv|offload)?' | head -1 || echo "no xdp"
done
#   expect: xdpgeneric on the LB devices, NOT xdpdrv (native)

# 5. The public NIC keeps carrier — no flapping.
journalctl -u systemd-networkd -b | grep -iE 'eno[0-9].*(Gained|Lost) carrier' | tail
#   expect: NOT a repeating Gained/Lost loop on the public NIC
```

Expected: cluster comes up healthy (`cilium status` OK, nodes Ready), the public
NIC stays up with its address, and XDP runs in generic mode (no native-driver
attach, so no PHY reset).
