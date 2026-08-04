# Block-node traffic shaper and host firewall

This document describes the design of the block-node host-firewall and
traffic-shaper: what the planes are, how egress and "ingress" shaping actually
work (and where inbound shaping hits a hard limit), what the daemon does at
runtime, how the artifacts are laid down and survive a reboot, and how a node
operator sets it all up. For the exact rule/policy semantics of each nft table
see the `network firewall` / `network policy` / `network shape` command help and
`docs/dev/daemon/traffic-shaper-statusz.md`.

## The three planes

A block node ends up with two nftables tables and one tc/HTB hierarchy, owned by
three packages under `internal/network/`:

| Plane | nft table / tc object | Package | Role |
|---|---|---|---|
| Host firewall | `inet weaver-host-firewall` | `internal/network/firewall` | INPUT-hook filter protecting the host: SSH/mgmt allowlist, blocked CIDRs, ICMP policy, in-cluster host-service ports. No shaping. Static — never reconciled by the daemon. |
| Block-node classifier | `inet weaver-blocknode-classifier` | `internal/network/policy` | FORWARD-hook filter: per-category ACL/quarantine **and** `meta priority` marking of classified pod traffic. Marks only — it does not shape bandwidth. |
| Bandwidth shaper | tc/HTB hierarchies | `internal/network/shape` | The actual bandwidth enforcement: an egress HTB hierarchy on the `$EGRESS` physical NIC and one on each pod's host-side `$VETH`. Classifies natively on the priority the classifier stamped. |

The classifier and the shaper are two halves of traffic shaping: the classifier
**marks** a packet with a priority; the tc HTB hierarchy **rate-limits** it into
the matching class. The host firewall is independent, applies to every node type,
and is out of scope for the daemon.

## How classify-and-shape fits together

The classifier and the shaper are decoupled and meet through exactly one thing:
the skb priority, encoded as a tc class id.

Each traffic category has a fixed mark, skb priority, and direction
(`internal/network/policy/class.go`):

| Category | Mark | skb priority | Direction (device) |
|---|---|---|---|
| `publisher` | `0x10` | `0x10010` | ingress (`$VETH`) |
| `backfill-response` | `0x20` | `0x10020` | ingress (`$VETH`) |
| `reserve-ingress` | `0x30` | `0x10030` | ingress (`$VETH`) |
| `partner` | `0x40` | `0x10040` | egress (`$EGRESS`) |
| `public` | `0x50` | `0x10050` | egress (`$EGRESS`) |
| `reserve-egress` | `0x60` | `0x10060` | egress (`$EGRESS`) |

The priority is just the class id encoded into an skb priority:
`priority = mark | 0x10000`, so `0x10010` is the HTB class id `1:10`, `0x10040`
is `1:40`, and so on (major `1` in the high 16 bits, the mark as the minor in the
low 16). Those minors are exactly the HTB leaf classes the shaper installs.

The classifier stamps that priority with an nft rule ending in
`meta priority set <hexPriority> accept` (`internal/network/policy/render.go`).
Asymmetric flows are handled with conntrack: the forward rule writes
`ct mark set <mark>` plus the forward priority, and a reply-restore rule
(`ct direction reply ct mark <mark> meta priority set <replyPriority>`) re-stamps
the reply leg. Pod traffic that matches no policy is deliberately left unstamped
so it falls to the HTB default class.

The shaper installs **no tc filters**. The kernel's HTB qdisc classifies natively
on `skb->priority` whenever it decodes to a valid `major:minor` class id, so the
priority the classifier stamped *is* the class selector. This is why the two
packages need no shared runtime state: `policy` owns the fixed name-to-priority
map, `shape` owns each class's bandwidth (rate/ceil/prio), and they meet only
through the class-id encoding.

## Egress vs "ingress" shaping (and the inbound limit)

In this package `ingress` and `egress` name two **devices**, not two tc
directions. Both are shaped with an ordinary **egress HTB root qdisc**; there is
no tc `ingress` qdisc, no IFB/ifb redirect, and no policing anywhere in the
codebase (`internal/network/shape/class.go`, `veth.go`, `tc_linux.go`).

- **`egress` device = the physical NIC (`$EGRESS`).** Shapes traffic the node
  sends out: `partner`, `public`, `reserve-egress`. The boot script
  (`solo-provisioner-bandwidth-shaper.sh`) builds `root handle 1: htb default
  <minor>`, a trunk class `1:1` at link rate, then one leaf HTB class plus an
  `fq_codel` leaf qdisc per configured class. Persisted and replayed at boot.
- **`ingress` device = the pod's host-side veth (`$VETH`).** Shapes traffic the
  host forwards *toward* the pod — the block node's inbound flows: `publisher`,
  `backfill-response`, `reserve-ingress`. Because Cilium's veth pair is crossed,
  putting an **egress** HTB on the host end of the veth is what rate-limits the
  pod's *inbound* traffic (`ApplyIngressVeth` in `veth.go` builds the same
  root/trunk/leaf + `fq_codel` structure). When the ingress root rate is left as
  `auto` it mirrors the egress link rate.

**The inbound limitation.** "Ingress shaping" here can only queue and drop packets
at the host veth on their way into the pod; it cannot make a remote sender slow
down. There is no true inbound qdisc or IFB path, so the only backpressure to the
origin is whatever TCP infers from the drops the HTB causes — UDP and
non-responsive senders get no backpressure at all. And because HTB **shapes**
(queues, then drops only when a class is saturated) rather than **polices** (drop
first), inbound enforcement is best-effort smoothing, not a hard inbound rate
guarantee. Read-back counters (`drops`, `overlimits` in `tc_linux.go`) exist for
observability of exactly this.

Default profiles (`internal/network/shape/defaults.go`): egress = `partner` 40%
(ceil 70), `public` 30% (ceil 70), `reserve-egress` 30% (ceil 100); ingress =
`publisher` 80%, `backfill-response` 10%, `reserve-ingress` 10% (all ceil 100).

## The daemon's job

`solo-provisioner-daemon` runs unprivileged (`User=weaver`) and reaches for
privilege only by exec-ing narrow `block node` worker subcommands through sudo.
For the block node its **only** monitor is the `TrafficShaperMonitor`
(`internal/daemon/blocknode/`), gated on the traffic-shaper being enabled. The
host firewall is **not** part of this — it is static, applied at install time and
mutated only by `network firewall` commands.

The monitor runs two independently-supervised responsibilities (each retried with
5 s to 5 min exponential backoff, so one fault never kills the daemon):

1. **Pod-lifecycle watcher -> the `$VETH` per-pod HTB.** Watches block-node pods
   (`app.kubernetes.io/name=block-node-server`). When a pod reaches
   `ContainersReady` (chosen over `PodReady` to shrink the unprioritized window),
   it resolves the host-side veth — retrying while Cilium wires the pair — and
   execs `block node tc-attach --veth <veth>` via sudo to build that pod's
   ingress HTB from the persisted class configs. On pod delete it best-effort
   `tc-detach`es (the kernel removes veth qdiscs with the interface anyway).
2. **Statusz poll loop -> the classifier's nft set membership.** Every
   `poll_interval` (default 5 s) it resolves the block node's statusz base URL
   (operator override, else discovered from the ready pod's IP + health port),
   runs an unprivileged `--check` digest probe, and only when the digest changed
   (or the URL changed, or a periodic force-resync elapses) execs `block node
   reconcile-shaper` via sudo to apply. The periodic forced apply self-heals
   out-of-band nft edits.

**statusz** is the block node's own health API — `statusz/inbound` and
`statusz/outbound` JSON endpoints served on the pod's health port (default
40983). The reconciler (`internal/blocknode/shaper/`) fetches both, buckets the
active peer endpoints into per-category desired set membership, normalizes bare
IPs to `/32`/`/128` CIDRs, derives the managed listener ports, digests the
result, and writes only the changed nft sets atomically under one lock via
`policy.Manager.ApplySets`. (This is distinct from the daemon's own
`GET /status` over `daemon.sock`, which reports daemon health, not block-node
state.)

The two privileged worker subcommands the daemon execs — both superuser-gated and
skipping the global preflight checks — are `block node tc-attach --veth <veth>
[--detach]` and `block node reconcile-shaper --statusz-url <url> [--check]`.

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
lives under `/usr/local/sbin` as a root-executable tool. The two nft table-name
constants live in `firewall/paths.go` and `policy/paths.go` (each package owns
its own table name, duplicated by value rather than shared). `shape/paths.go`
names no table — it mirrors the policy registry dir (`policyRegistryDir`) by
value instead. All such by-value mirrors must stay in sync.

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

Both `ExecStart` lines load a table if its `.nft` file exists — each is guarded by
a `test -e`, so the unit is a no-op for whichever table has not been provisioned.
The same unit is restarted on every live mutation (a `network firewall` /
`network policy` command, or the install/reconfigure workflow) so the on-disk
file and the kernel stay in sync. Note this replays the table structure only —
the classifier's set *membership* is not in the file (see boot persistence
below).

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
root qdisc and rebuilds the `$EGRESS` HTB root + trunk + one leaf class/leaf-qdisc
per configured tc class, auto-detecting the link speed and falling back to 1000
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

## Operator setup and configuration

### Turning it on at install time

Both features are opt-in on `block node install` (and `block node reconfigure`),
gated by two independent flags. In the interactive flow the **host firewall is
asked first**, then traffic shaping:

- `--firewall-enabled` — install the `inet weaver-host-firewall` plane.
  Configured by `--mgmt-cidrs`, `--blocked-cidrs`, `--ssh-port`, `--pod-cidr`,
  `--in-cluster-ports`.
- `--traffic-shaping-enabled` — the single switch that wires up **all three**
  shaping pieces: the classifier (`inet weaver-blocknode-classifier`), the tc HTB
  hierarchies, and the traffic-shaper daemon. Only when this is accepted does
  install prompt for the egress NIC (`--egress-interface`) and its line rate
  (`--link-rate`, accepts `auto`), take per-class overrides via repeatable
  `--shape <class>=rate=<r>,ceil=<c>,prio=<p>`, and set the daemon poll cadence
  with `--statusz-poll-interval`.

Gating precedence is flag > interactive confirm > seed default; supplying a
content flag without its gate flag is rejected. `reconfigure` seeds each gate
from the persisted decision, so a no-flag reconfigure never silently tears a
plane down.

### Adjusting a live node with the `network` commands

The three `network` sub-scopes drive each plane directly; every mutation live-
applies and then persists (see below), so they are safe to run by hand on a
provisioned node.

- **`network firewall`** (`create`/`add`/`remove`/`set`/`show`/`delete`) — the
  host firewall. `create` takes `--mgmt-cidrs`, `--blocked-cidrs`,
  `--in-cluster-ports`, `--ssh-port`, `--pod-cidr`; `add`/`remove` take the
  singular forms; `set` atomically replaces a full list.
- **`network policy`** (`create`/`add`/`remove`/`set`/`show`/`delete`) — the
  classifier. `create` takes `--name` (the nft set name), `--stamp` (the HTB
  class to classify into, which also fixes direction) or `--deny`, plus
  `--reply-stamp`, `--from-entity world`, `--ports`, `--cidrs`/`--cidrs-file`,
  `--pod-cidr`.
- **`network shape`** (`create`/`set`/`show`/`delete`/`watch`) — the tc HTB
  plane. `create --device ingress|egress` sets a device's `--rate` (accepts
  `auto`) and `--default` class; `create --class <name>` sets a class's `--rate`,
  `--ceil`, `--prio [0,7]`. `set` does a live `tc class change` without qdisc
  churn; `watch` samples live counters read-only.

### How a hand-run command reaches the kernel

During `block node install` / `reconfigure` the workflow lays these down:

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

# tc: the live egress HTB hierarchy (physical NIC) and a pod's ingress veth
tc -s class show dev "$EGRESS_NIC"
tc -s class show dev "$POD_VETH"

# systemd: the loaders and the daemon
systemctl status solo-provisioner-network-nft.service \
                 solo-provisioner-bandwidth-shaper.service \
                 solo-provisioner-daemon.service

# on-disk artifacts
ls -l /etc/solo-provisioner/ /etc/solo-provisioner/network/shape/{devices,classes}
cat /usr/local/sbin/solo-provisioner-bandwidth-shaper.sh
```
