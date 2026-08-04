# Block-node traffic shaper: files, services, and boot persistence

This document describes how the block-node traffic-shaper and host-firewall
artifacts are laid down on a node, how they are loaded, and how they survive a
reboot. It is a map of *what lives where and who loads it* — for the rule/policy
semantics see the `network firewall` / `network policy` / `network shape` command
docs and `docs/dev/daemon/traffic-shaper-statusz.md`.

## The three planes

The node ends up with two nftables tables and one tc/HTB hierarchy, owned by
three packages under `internal/network/`:

| Plane | nft table / tc object | Package | Role |
|---|---|---|---|
| Host firewall | `inet weaver-host-firewall` | `internal/network/firewall` | INPUT-hook filter protecting the host: SSH/mgmt allowlist, blocked CIDRs, ICMP policy, in-cluster host-service ports. No shaping. |
| Block-node classifier | `inet weaver-blocknode-classifier` | `internal/network/policy` | FORWARD-hook filter: per-category ACL/quarantine **and** `meta priority` marking of classified pod traffic. Marks only — it does not shape bandwidth. |
| Bandwidth shaper | tc/HTB egress hierarchy | `internal/network/shape` | The actual bandwidth enforcement: HTB classes on the `$EGRESS` NIC (and the per-pod `$VETH`). Consumes the classifier's `meta priority` marks. |

The classifier and the shaper are two halves of traffic shaping: the classifier
**marks** a packet with a priority; the tc HTB hierarchy **rate-limits** it into
the matching class. The host firewall is independent and applies to every node
type.

## On-disk layout

Everything the operator-facing config needs lives under `/etc/solo-provisioner/`
(on the root filesystem, so it is available early at boot before any late
`/opt/solo` mount). The boot-replay tc script lives under `/usr/local/sbin`, and
the systemd units under `/usr/lib/systemd/system/`.

```
/etc/solo-provisioner/
  network-weaver-host-firewall.nft          # inet weaver-host-firewall table (full ruleset)
  network-weaver-blocknode-classifier.nft   # inet weaver-blocknode-classifier table (chain + set decls)
  policies/                                 # one JSON per policy; source of truth for classifier rules
  network/shape/
    devices/                                # one JSON per tc device (egress/ingress): root qdisc rate + default class
    classes/                                # one JSON per tc class: rate/ceil/prio (daemon reads this to rebuild $VETH)

/usr/local/sbin/
  solo-provisioner-bandwidth-shaper.sh      # boot-replay script for the $EGRESS HTB hierarchy

/usr/lib/systemd/system/
  solo-provisioner-network-nft.service      # loads both .nft files at boot
  solo-provisioner-bandwidth-shaper.service # runs the tc script at boot
  solo-provisioner-daemon.service           # the long-lived daemon (reconciles set membership + $VETH)
```

The `.nft` files and the tc config live under `/etc` on purpose; the tc script
lives under `/usr/local/sbin` as a root-executable tool. The two nft table
constants are intentionally duplicated by value across `firewall/paths.go`,
`policy/paths.go`, and `shape/paths.go` and must stay in sync.

## systemd units

Two oneshot units replay the persisted state at boot, both ordered **before** the
daemon so the kernel is already in its baseline shape when the daemon starts
reconciling.

### solo-provisioner-network-nft.service (nftables loader)

Shared by the firewall and classifier packages. Rendered from
`internal/templates/files/network/solo-provisioner-network-nft.service`.

```ini
[Unit]
DefaultDependencies=no
After=local-fs.target
Before=solo-provisioner-daemon.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c 'test -e /etc/solo-provisioner/network-weaver-host-firewall.nft || exit 0; exec /usr/sbin/nft -f /etc/solo-provisioner/network-weaver-host-firewall.nft'
ExecStart=/bin/sh -c 'test -e /etc/solo-provisioner/network-weaver-blocknode-classifier.nft || exit 0; exec /usr/sbin/nft -f /etc/solo-provisioner/network-weaver-blocknode-classifier.nft'

[Install]
WantedBy=multi-user.target
```

Each `ExecStart` is guarded by a `test -e`, so the unit is a no-op for whichever
table has not been provisioned. The same unit is restarted on every live
mutation (a `network firewall`/`network policy` command, or the install/
reconfigure workflow) so the on-disk file and the kernel stay in sync.

### solo-provisioner-bandwidth-shaper.service (tc HTB loader)

Rendered from
`internal/templates/files/network/solo-provisioner-bandwidth-shaper.service`.

```ini
[Unit]
DefaultDependencies=no
After=network-pre.target
Before=network.target solo-provisioner-daemon.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/sbin/solo-provisioner-bandwidth-shaper.sh

[Install]
WantedBy=multi-user.target
```

The script (rendered from `solo-provisioner-bandwidth-shaper.sh.tmpl`) deletes the
root qdisc and rebuilds the `$EGRESS` HTB root + one class/leaf-qdisc per
configured tc class, auto-detecting the link speed and falling back to 1000
Mbit/s.

### solo-provisioner-daemon.service

The long-lived daemon (`Type=notify`, `Restart=always`, `ExecStart=/opt/solo/
weaver/bin/solo-provisioner-daemon`). It is ordered `After=network.target`, so it
starts after both oneshots have replayed the baseline. Its job at runtime is to
fill in the parts that are deliberately **not** persisted (see below).

## What survives a reboot — and what the daemon rebuilds

| Artifact | Persisted at boot? | Rebuilt by |
|---|---|---|
| nft tables, chains, rules (both tables) | Yes — replayed from the `.nft` files via `nft -f` | — |
| nft **set elements** (the CIDR membership of `bn-*` sets) | **No** | daemon statusz poll loop, ~5 s after boot |
| `$EGRESS` HTB hierarchy | Yes — the `solo-provisioner-bandwidth-shaper.sh` script | — |
| `$VETH` (per-pod) HTB hierarchy | **No** | daemon pod-lifecycle watcher, on the next pod-create event |

Two things are intentionally left out of boot persistence:

- **Set membership** (which CIDRs belong to each `bn-*` category) is never written
  to the `.nft` file — statusz is the source of truth and the daemon reconciles
  it. On a fresh boot the classifier chain is present but its sets are empty
  until the daemon's first poll rehydrates them.
- **The `$VETH` HTB** would be meaningless to persist because the veth interface
  does not survive reboot (Cilium recreates it on pod start). The daemon
  reinstalls it from `network/shape/classes/` on each pod-create event.

## When the files/units are written and enabled

During `block node install` / `reconfigure`, the workflow lays these down:

- **Host firewall** — the firewall-create step renders
  `network-weaver-host-firewall.nft`, then `EnsureNetworkNftUnit` installs and
  enables `solo-provisioner-network-nft.service` and restarts it.
- **Classifier** — `NftWeaverPersist`
  (`internal/workflows/steps/step_network_nft_weaver.go`) re-renders
  `network-weaver-blocknode-classifier.nft` from the policy registry, ensures the
  shared nft unit, and restarts it.
- **Bandwidth shaper** — `TcEgressPersist`
  (`internal/workflows/steps/step_network_tc_egress.go`) renders
  `solo-provisioner-bandwidth-shaper.sh` and `EnsureTcEgressUnit` installs and
  enables `solo-provisioner-bandwidth-shaper.service`.

Running any equivalent `network firewall` / `network policy` / `network shape`
command by hand takes the same live-apply-then-persist path, guarded by a shared
flock under `/run/solo-provisioner/network/` so a hand-run command and the daemon
poll loop never interleave nft transactions.

## Inspecting a running node

```bash
# nft: table definitions (rules persist; set elements are daemon-managed)
sudo nft list table inet weaver-host-firewall
sudo nft list table inet weaver-blocknode-classifier

# tc: the live egress HTB hierarchy
tc -s class show dev "$EGRESS_NIC"

# systemd: the loaders and the daemon
systemctl status solo-provisioner-network-nft.service \
                 solo-provisioner-bandwidth-shaper.service \
                 solo-provisioner-daemon.service

# on-disk artifacts
ls -l /etc/solo-provisioner/ /etc/solo-provisioner/network/shape/{devices,classes}
cat /usr/local/sbin/solo-provisioner-bandwidth-shaper.sh
```
