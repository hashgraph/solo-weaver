# `network shape` — bandwidth classes

Manages the tc HTB hierarchy. `block node install` drives this automatically from
`--link-rate`; `network shape` lets you inspect or adjust individual classes afterwards.

Every `create`/`set`/`delete` re-renders
`/usr/local/sbin/solo-provisioner-bandwidth-shaper.sh` and restarts
`solo-provisioner-bandwidth-shaper.service`, so the live kernel and the boot script stay in
sync.

> **Flags not listed on this page.** Every command here also accepts the
> [global flags](../../reference/global-flags.md) — `--config`, `--output`, `--log-level`,
> `--force`, `--verbose`, `--non-interactive`.

## The six classes

Class names are fixed. Each belongs to exactly one direction.

| Class | Direction | tc classid | Default rate | Default ceil | Prio |
|---|---|---|---|---|---|
| `publisher` | ingress | `1:10` | 80% of trunk | 100% | 0 |
| `backfill-response` | ingress | `1:20` | 10% | 100% | 7 |
| `reserve-ingress` | ingress (**default**) | `1:30` | 10% | 100% | 1 |
| `partner` | egress | `1:40` | 40% of trunk | 70% | 0 |
| `public` | egress | `1:50` | 30% | 70% | 5 |
| `reserve-egress` | egress (**default**) | `1:60` | 30% | 100% | 1 |

Prio 0 is highest. Percentages are of that device's trunk rate. The **default** class is where
unmatched traffic lands.

- **Egress** is the physical NIC. Its hierarchy is persisted for reboot replay.
- **Ingress** is the per-pod host-side veth. It is ephemeral, so it is recorded as config only
  and re-attached per-pod by the daemon (see [`tc-attach`](../block-node.md#tc-attach--attach-ingress-shaping-to-a-pod-veth)).

## `create --device` — the device root

```bash
# Explicit trunk rate, written into the boot script as concrete tc values
sudo solo-provisioner network shape create --device egress --rate 1gbit --default reserve-egress

# Detect the trunk rate now from sysfs and store the resolved value
sudo solo-provisioner network shape create --device egress --rate auto --default reserve-egress

# Replace an existing device config
sudo solo-provisioner network shape create --device egress --rate 1gbit --default reserve-egress --force
```

### What `--rate auto` does

- Reads the NIC's link speed from `/sys/class/net/<NIC>/speed` **at create time**, while the
  link is up and stable.
- Stores the resolved value (e.g. `1gbit`) as an ordinary explicit rate.
- If the speed is not readable (a virtual NIC reporting `-1`), falls back to a concrete
  `1gbit`.

Either way you get a concrete stored rate: `network shape show` reports a real number, and the
boot script carries explicit values with no `SPEED` variable and no sysfs read at boot.

> The sysfs-at-boot form only appears when no shape device is configured at all — for example
> `block node install` run without `--link-rate` in non-interactive mode.

Until you add the first `--class`, the device root renders a placeholder hierarchy using the
default proportions from the table above, at the resolved trunk rate. Adding explicit
`--class` configs replaces the placeholder.

## `create --class` — leaf classes

```bash
sudo solo-provisioner network shape create --class partner        --rate 400mbit --ceil 700mbit  --prio 0
sudo solo-provisioner network shape create --class public         --rate 300mbit --ceil 700mbit  --prio 5
sudo solo-provisioner network shape create --class reserve-egress --rate 300mbit --ceil 1000mbit --prio 1
```

Once all three classes for a direction are present, the boot script switches to fully explicit
rates with no `SPEED` variable at all.

## `set` — live update, no qdisc teardown

```bash
sudo solo-provisioner network shape set --class partner --rate 500mbit
sudo solo-provisioner network shape set --class public  --ceil 600mbit
```

`set` runs `tc class change` on the live kernel and re-renders the boot script immediately.
Tuning done this way survives a bare `block node reconfigure` or `upgrade`.

## `show` — stored configuration

```bash
sudo solo-provisioner network shape show                  # all devices and classes
sudo solo-provisioner network shape show --class partner  # one class
```

`show` reports the **stored** rate/ceil/prio. For live traffic, use `watch`.

## `watch` — live counters, read-only

```bash
# Watch the egress NIC every 2s. Runs until Ctrl-C.
sudo solo-provisioner network shape watch --device egress --iface enp0s1

# One class, faster sampling, stop after 5 samples
sudo solo-provisioner network shape watch --device egress --iface enp0s1 \
  --class partner --interval 1s --count 5

# Watch a block node's ingress veth
sudo solo-provisioner network shape watch --device ingress --iface lxc1a2b3c
```

It samples `tc -s class show dev <iface>` at `--interval` and prints, per class:

- Throughput, from the byte delta
- Change in overlimits and drops since the previous sample

Use it to confirm traffic really is being classified and shaped — for example partner traffic
landing in `1:40` with a non-zero rate and climbing overlimits.

**Both `--device` and `--iface` are required.** The command does no environment probing: no
NIC detection, no veth detection. That keeps it independent of any running block node.

- For `egress`, `--iface` is the physical NIC (`enp0s1`).
- For `ingress`, it is the per-pod host veth (`lxc1a2b3c`). Find it with `ip link` or
  `tc qdisc show`.

`watch` never mutates tc or the shape registry. It complements the Prometheus counters, which
target dashboards rather than an operator at a terminal.

## `delete`

```bash
sudo solo-provisioner network shape delete --class reserve-egress
```

Fails if the class is the device default, or if a policy `--stamp` references it.

## All `network shape` flags

| Flag | What it does | Required |
|---|---|---|
| `--device` | Direction: `egress` or `ingress` | one of `--device` / `--class` |
| `--class` | Class name from the table above | one of `--device` / `--class` |
| `--rate` | Bandwidth (`100mbit`, `1gbit`) or `auto` (sysfs; `--device` form only) | yes on create/set |
| `--ceil` | Burst ceiling, must be >= `--rate`. Defaults to `--rate` | no |
| `--prio` | HTB priority `0`–`7`; 0 is highest | no (default `0`) |
| `--default` | Default class for unmatched traffic (`--device` form only) | yes with `--device` |
| `--force` | Replace an existing device or class config | no |
| `--iface` | Interface to sample. No auto-detection | yes for `watch` |
| `--interval` | Sampling interval for `watch` (`1s`, `500ms`) | no (default `2s`) |
| `--count` | Number of `watch` samples then exit. `0` runs until interrupted | no |

---

---

## See also

- [Network commands](README.md) — the other two planes
- [`network policy`](policy.md) — what decides which class traffic lands in
- [`block node tc-attach`](../block-node.md#tc-attach--attach-ingress-shaping-to-a-pod-veth) — how the ingress hierarchy reaches a pod
- [Traffic shaper internals](../../dev/traffic-shaper.md)
