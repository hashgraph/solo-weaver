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
| Workload policy | `inet weaver-workload-policy` | `internal/network/policy` | FORWARD-hook filter over forwarded pod/workload traffic: per-category ACL/quarantine **and** `meta priority` marking (classification). Marks and filters only — it does not shape bandwidth. |
| Bandwidth shaper | tc/HTB hierarchies | `internal/network/shape` | The actual bandwidth enforcement: an egress HTB hierarchy on the `$EGRESS` physical NIC and one on each pod's host-side `$VETH`. Classifies natively on the priority the policy plane stamped. |

The policy plane and the shaper are two halves of traffic shaping: the policy
plane **marks** a packet with a priority; the tc HTB hierarchy **rate-limits** it
into the matching class. The host firewall is independent, applies to every node
type, and is out of scope for the daemon.

The plane is named for the FORWARD hook it governs — *workload* traffic — not for
the block node specifically. It is only ever provisioned for block nodes today,
and its traffic categories and daemon reconciler are block-node-specific (below),
but the table itself holds whatever `network policy` writes, block-node-related
or not.

### Which plane sees which traffic

The two tables register on different hooks, so they see **disjoint traffic**. That is why
neither carries a rule for the other's ports, and why no block-node service port appears
anywhere in the host firewall's rules or templates.

| Traffic | Outcome | Decided by |
|---|---|---|
| External → node address, **non**-service port | Dropped | Host firewall `input` (`policy drop`) |
| External → node address, service port | Translated, then forwarded. Classified when the port is in a managed `<name>_ports` set, otherwise forwarded unclassified | Workload policy `forward` |
| In-cluster → pod address directly, any port | Not constrained here — forwarded under `policy accept` | Cilium |
| Either endpoint in `@bn-restricted` | Dropped, both directions and both families | Workload policy `forward` |
| Anything → pod address, block-node health port | Request leg dropped, in each family that has a pod CIDR | Workload policy `forward` (`bn-health`) |

The first row misleads, because the mechanism is not the one the rule layout suggests. A packet
addressed to a port with no service behind it gets **no load-balancer translation** — only
exposed service ports have translation entries. Untranslated, its destination is still the
node's own address, so the routing decision delivers it locally, it arrives at `input`, and the
default drop catches it. It never becomes pod-bound traffic, so the classifier never sees it.

Service traffic takes the opposite path: translation happens *before* the routing decision
(Cilium's eBPF at the tc ingress hook, or `prerouting` when kube-proxy performs the DNAT), so
the packet is forwarded and bypasses `input` entirely. That is why a block node serves traffic
on its service ports while the host firewall opens none of them.

One consequence worth knowing, because it is silent: **an exposed port absent from the managed
`<name>_ports` sets is forwarded and unshaped.** It matches no classification rule, carries no
`meta priority`, and lands in the HTB default class — `reserve-ingress` inbound, a 10%
guarantee. Since those sets are reconciled from statusz, a listener the block node does not
report gets no shaping rather than an error. For what each hook does and does not enforce, see
[Coexistence with the host's existing network stack](#coexistence-with-the-hosts-existing-network-stack).

## How classify-and-shape fits together

The policy plane and the shaper are decoupled and meet through exactly one thing:
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

The policy plane stamps that priority with an nft rule ending in
`meta priority set <hexPriority> accept` (`internal/network/policy/render.go`).
Asymmetric flows are handled with conntrack: the forward rule writes
`ct mark set <mark>` plus the forward priority, and a reply-restore rule
(`ct direction reply ct mark <mark> meta priority set <replyPriority>`) re-stamps
the reply leg. Pod traffic that matches no policy is deliberately left unstamped
so it falls to the HTB default class.

The shaper installs **no tc filters**. The kernel's HTB qdisc classifies natively
on `skb->priority` whenever it decodes to a valid `major:minor` class id, so the
priority the policy plane stamped *is* the class selector. This is why the two
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
mutated only by `network firewall` commands. `network firewall reapply` re-asserts
it from the persisted config on demand, taking no arguments and recording no
enable/disable decision; every apply also retains the generation it replaces at
`network-weaver-host-firewall.yaml.prev`, which is the recovery path that keeps
named allow rules when the config is lost (the `.nft` reparse fallback does not).

The monitor runs two independently-supervised responsibilities (each retried with
5 s to 5 min exponential backoff, so one fault never kills the daemon):

1. **Pod-lifecycle watcher -> the `$VETH` per-pod HTB.** Watches block-node pods
   (`app.kubernetes.io/name=block-node-server`). When a pod reaches
   `ContainersReady` (chosen over `PodReady` to shrink the unprioritized window),
   it resolves the host-side veth — retrying while Cilium wires the pair — and
   execs `block node tc-attach --veth <veth>` via sudo to build that pod's
   ingress HTB from the persisted class configs. On pod delete it best-effort
   `tc-detach`es (the kernel removes veth qdiscs with the interface anyway).
2. **Statusz poll loop -> the policy plane's nft set membership.** Every
   `poll_interval` (default 5 m) it resolves the block node's statusz base URL
   (operator override, else discovered from the ready pod's IP + health port),
   runs an unprivileged `--check` digest probe, and only when the digest changed
   (or the URL changed, or the hourly force-resync elapses) execs `block node
   reconcile-shaper` via sudo to apply. The periodic forced apply self-heals
   out-of-band nft edits. On daemon startup the loop reconciles once immediately
   before the first tick, so membership converges as soon as statusz is
   reachable. After a reboot the sets are not empty while it waits: the oneshot
   has already replayed the last applied membership from the `.nft` file, and
   this first poll replaces it.

**statusz** is the block node's own health API — `statusz/inbound` and
`statusz/outbound` JSON endpoints served on the pod's health port (default
40983). The reconciler (`internal/blocknode/shaper/`) fetches both, buckets the
active peer endpoints into per-category desired set membership, normalizes bare
IPs to `/32`/`/128` CIDRs, derives the managed listener ports, digests the
result, and writes only the changed nft sets atomically under one lock via
`policy.Manager.ApplySets`. (This is distinct from the daemon's own
`GET /status` over `daemon.sock`, which reports daemon health, not block-node
state.)

That port is also what `bn-health` drops. Only the node's kubelet and the
provisioner itself consume it, and both dial the pod from the node, so their
packets take `output`/`postrouting` and never reach the `forward` hook this table
registers on. Everything that does reach it is off-node by construction, which is
why the rule needs no source allowlist — and why there is no set to keep
populated across a replay. The drop is keyed on a static port list, which comes
from the policy registry rather than from statusz, and so is rendered into
`network-weaver-workload-policy.nft` from the registry entry on every re-render.

The rule drops the request leg only, and carries `ct direction original`. Both
details matter: a listener port sits inside the default ephemeral range
(`net.ipv4.ip_local_port_range`, 32768-60999), so an unrelated pod can draw
40983 as the source port of an outbound connection. Without the direction
qualifier, that connection's reply — `daddr <podCIDR> tcp dport 40983` — matches
the drop and the connection dies silently, since SYN retransmits reuse the port.
An egress mirror on `tcp sport` would kill the outbound leg the same way, and no
qualifier can rescue it, so there is none: dropping the request leg is sufficient
because this chain has no conntrack accept fast-path, so an already-open
connection loses its forward leg too.

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
  network-weaver-host-firewall.yaml         # its declarative config; source of truth for the host-firewall verbs
  network-weaver-workload-policy.nft        # inet weaver-workload-policy table (chain + set decls)
  policies/                                 # one JSON per policy; source of truth for workload-policy rules
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

Two oneshot units replay the persisted state at boot. Only the nft loader is
ordered **before** the daemon, because the daemon owns nft set membership and
must not start against a missing table; the bandwidth shaper is deliberately
unordered against it — see below.

### solo-provisioner-network-nft.service (nftables loader)

Shared by the firewall and policy packages. Rendered from
`internal/templates/files/network/solo-provisioner-network-nft.service`.

```ini
[Unit]
DefaultDependencies=no
Wants=network-pre.target
After=local-fs.target nftables.service ufw.service firewalld.service
Before=network-pre.target solo-provisioner-daemon.service
StartLimitIntervalSec=0

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c 'test -e /etc/solo-provisioner/network-weaver-host-firewall.nft || exit 0; exec /usr/sbin/nft -f /etc/solo-provisioner/network-weaver-host-firewall.nft'
ExecStart=/bin/sh -c 'test -e /etc/solo-provisioner/network-weaver-workload-policy.nft || exit 0; exec /usr/sbin/nft -f /etc/solo-provisioner/network-weaver-workload-policy.nft'

[Install]
WantedBy=multi-user.target
```

Both `ExecStart` lines load a table if its `.nft` file exists — each is guarded by
a `test -e`, so the unit is a no-op for whichever table has not been provisioned.
The same unit is restarted on every live mutation (a `network firewall` /
`network policy` command, or the install/reconfigure workflow) so the on-disk
file and the kernel stay in sync. This replays the table structure *and* the
policy plane's set membership, which is rendered inline into the file (see boot
persistence below).

The `After=` list is what keeps the tables alive across a reboot: stock
`/etc/nftables.conf` opens with `flush ruleset`, so loading *before* an enabled
`nftables.service` (or `ufw`/`firewalld`) would let that manager erase the weaver
tables on every boot. `After=` is inert unless systemd is actually starting that
unit, so hosts with no firewall manager are unaffected.

**How a unit change reaches an existing host.** The unit file is written only by
a mutation (`network firewall` / `network policy`, or the install/reconfigure
workflow). An already-provisioned host that is upgraded runs no such mutation —
it only receives a new binary — so a change to the embedded unit would never
arrive. The startup migration built by `NewNetworkNftUnitMigration`
(`internal/workflows/migration_network_unit.go`, registered under
`migration.ScopeStartup`) closes that gap: before every command that runs the
global pre-run checks, and explicitly on the `solo-provisioner install` upgrade
path, it compares the installed unit against the embedded copy and rewrites it
when they differ.

The comparison is not content-only. A unit that matches the embedded copy
byte-for-byte but is **disabled** is still the #982 failure — systemd never
starts it, so the tables do not come back after a reboot — and a byte diff
cannot see that. `NetworkNftUnitNeedsConverge`
(`internal/network/firewall/unit_drift.go`) therefore also queries enablement,
and `EnsureNetworkNftUnit` re-enables on its unchanged-content fast path. Both
halves are required: the migration only runs `Execute` when the probe reports
drift, so an enable that lives only in `Execute` would never be reached on the
host that needs it. The query is filesystem-only — `unitconv.EnabledAtBoot`
lstats the `multi-user.target.wants` symlink rather than opening a DBus
connection — so it stays cheap enough to run ahead of every root command, and
`EnsureUnit`'s fast path reads the same probe, so the gate that admits a host
and the gate that clears it cannot disagree. The authoritative write
(`systemctl enable`) stays in `Execute`. Both halves live in
`internal/network/unitconv` (`NeedsConverge` / `EnsureUnit`), shared with the
shaper unit below. `version` and the shaper worker verbs the daemon delegates
(`block node tc-attach`, `reconcile-shaper`) opt out of the pre-run
(`SkipGlobalChecks`) and never reach it. The daemon's other delegated verb,
`network policy set`, does *not* opt out: it runs the pre-run as root under
`sudo -n`, so it reaches the migration inside the daemon unit's mount namespace,
where `ProtectSystem=strict` leaves `/usr/lib` read-only. The write fails there
and is warned about rather than returned (see below), so the poll loop keeps
working and the unit converges on the next privileged invocation outside that
namespace — in practice the `solo-provisioner install` upgrade run itself. It is
gated on that drift rather than on a CLI version boundary, so every future unit
change is delivered by the same migration with no new boundary to remember. It
never restarts the unit — a restart would replay the workload-policy artifact
and revert a healthy policy table's live sets to that artifact's membership
snapshot; the new ordering takes effect at the next boot.

Because that gate is host state, it never closes on its own, which makes two
guard rails load-bearing. The migration **skips entirely for a non-root caller**
(the write lands under `/usr/lib`, so an unprivileged invocation could only
fail), and a **write failure is warned about, not returned**. Without either, a
host whose `/usr/lib` write cannot succeed would fail the pre-run of *every*
command run on it, indefinitely — a version-gated migration cannot get into that
state because its boundary closes. The loud failure still exists, on the mutation
path: `EnsureNetworkNftUnit`, called by `network firewall` / `network policy`,
returns its error.

### solo-provisioner-bandwidth-shaper.service (tc HTB loader)

Rendered from
`internal/templates/files/network/solo-provisioner-bandwidth-shaper.service`.

```ini
[Unit]
Wants=network-online.target
After=network-online.target
ConditionPathExists=/usr/local/sbin/solo-provisioner-bandwidth-shaper.sh
StartLimitIntervalSec=0

[Service]
Type=oneshot
RemainAfterExit=yes
Environment=SHAPER_DEVICE_WAIT_SECS=30
TimeoutStartSec=60s
ExecStart=/usr/local/sbin/solo-provisioner-bandwidth-shaper.sh

[Install]
WantedBy=multi-user.target
```

The script (rendered from `solo-provisioner-bandwidth-shaper.sh.tmpl`) deletes the
root qdisc and rebuilds the `$EGRESS` HTB root + trunk + one leaf class/leaf-qdisc
per configured tc class, auto-detecting the link speed and falling back to 1000
Mbit/s.

**Why it runs after the network, not before it (#980).** The egress interface is
often a netplan/systemd-networkd bond, bridge or VLAN, which does not exist until
the network is configured. The previous `After=network-pre.target`
`Before=network.target` ordering ran `tc` against a device that was not there
yet, failed, and — being a `oneshot` with no `Restart=` — never tried again: the
NIC stayed unshaped for the whole boot. `Wants=` is what makes the new ordering
bite, since `After=` alone is inert unless something else pulls
`network-online.target` into the transaction. `Before=network.target` had to go
(it would now be a cycle), and `Before=solo-provisioner-daemon.service` went with
it: ordering is transitive, so keeping it would make every daemon start wait on
`systemd-networkd-wait-online` — ~120s on a host with a non-optional unplugged
link, exactly the multi-NIC netplan host this fix targets. Nothing is lost by
dropping it, because the daemon's own `tc` work is the per-pod `$VETH` hierarchy,
never the `$EGRESS` root, so the two cannot race either way.

**How a late device is covered.** `SHAPER_DEVICE_WAIT_SECS` gives the script a
bounded poll on `/sys/class/net/<nic>` before its first `tc` call. The unit sets
it, so a manual run of the script fails fast instead of blocking. A device that
appears later than the budget stays unshaped until the next converge, and the
script exits 75 (`EX_TEMPFAIL`) to say so — a diagnostic, not a retry trigger.

That poll is the *whole* retry, because no restart policy is usable here:
`RestartForceExitStatus=` would scope a retry to the late-device case but is only
acted upon for `Type=oneshot` from systemd 256
([systemd#31148](https://github.com/systemd/systemd/issues/31148)) — Ubuntu 22.04
ships 249 and Debian 12 ships 252 — and an unscoped `Restart=on-failure` would
rebuild the root qdisc every `RestartSec=` forever on a config `tc` keeps
rejecting. `TimeoutStartSec=60s` is therefore the only bound on a wait that
hangs, since `Type=oneshot` disables the start timeout by default; it sits above
the 30s budget, which is paid *synchronously* twice — `multi-user.target` waits
for the start job, and `ApplyTcEgressScript` blocks on the restart.
`StartLimitIntervalSec=0` because with no restart policy the only starts systemd
could count are an operator's.

The teardown render (`Unshape`) deliberately has **no** wait loop: a host whose
NIC is already gone needs the unshape to succeed trivially. `ConditionPathExists=`
is the same guard one level up — a unit left behind with no script stays inactive.

**How a unit change reaches an existing host.** Same gap as the nft loader, same
code: the startup migration built by `NewNetworkShaperUnitMigration`
(`internal/workflows/migration_network_unit.go`) rewrites and re-enables the unit
when it drifts, and never restarts it, so the live HTB hierarchy survives and the
new ordering applies at the next boot. `TcEgressUnitNeedsConverge`
(`internal/network/shape/unit_drift.go`) gates the missing-unit case on a
persisted boot script, so a host that never shaped traffic gets no unit. Teardown
removes that script **before** the unit, because the script is the guard:
unit-first would leave a partial teardown that the next root command reads as
"persisted hierarchy with no unit" and silently reinstalls.

### solo-provisioner-daemon.service

The long-lived daemon (`Type=notify`, `Restart=always`, `ExecStart=/opt/solo/
weaver/bin/solo-provisioner-daemon`). It is ordered `After=network.target`, and
the nft loader declares `Before=solo-provisioner-daemon.service`, so the tables
and their persisted set elements exist before the daemon's first `ApplySets`. The
bandwidth shaper declares no edge against the daemon — the daemon's `tc` work
never touches the `$EGRESS` root, so no order between them can race (see above).
Its job at runtime is to fill in the parts that are deliberately **not** persisted
(see below).

## What survives a reboot — and what the daemon rebuilds

| Artifact | Persisted at boot? | Rebuilt by |
|---|---|---|
| nft tables, chains, rules (both tables) | Yes — replayed from the `.nft` files via `nft -f` | — |
| nft **set elements** (the CIDR membership of `bn-*` sets, and the managed `<name>_ports` sets) | Yes — rendered inline as `elements = { … }` in `network-weaver-workload-policy.nft` | daemon statusz poll loop, which replaces the persisted state on its first successful poll |
| `$EGRESS` HTB hierarchy | Yes — the `solo-provisioner-bandwidth-shaper.sh` script | — |
| `$VETH` (per-pod) HTB hierarchy | **No** | daemon pod-lifecycle watcher, on the next pod-create event |

Set membership is written to the `.nft` file as part of each set's declaration,
so the oneshot replays it in the same `nft -f` transaction that creates the
table. Because that unit is ordered `Before=solo-provisioner-daemon.service`, the
sets are populated before the daemon starts — a peer the block node has
quarantined is dropped from the first forwarded packet after a reboot, not from
the first successful statusz poll.

statusz remains the source of truth. The persisted elements are a warm start, not
an authority: every owned set is fully replaced on the first successful poll, and
`bucketizeEndpoints` seeds each owned binding present-with-an-empty-slice, so a
category the block node no longer reports collapses to an empty set rather than
leaving stale peers behind. A node that has been off for a long time therefore
replays a stale list until that first poll — which for `bn-restricted` errs
toward over-blocking, the safe direction for a quarantine.

The writer is `policy.Manager.persistMembership`, which re-renders the document
from the registry plus the *live* contents of every daemon-owned set. It runs
inside the same lock acquisition as the kernel write, on every membership
mutation — the daemon's `ApplySets` as well as the hand-run `network policy
add` / `remove` / `set`. An unchanged render is skipped via a SHA-256 compare, so
a steady-state roster does not rewrite `/etc` on every forced resync.

One thing is still intentionally left out of boot persistence:

- **The `$VETH` HTB** would be meaningless to persist because the veth interface
  does not survive reboot (Cilium recreates it on pod start). The daemon
  reinstalls it from `network/shape/classes/` on each pod-create event.

Two paths deliberately render without membership, leaving the sets empty until
the next poll: `RenderWeaverNft` (the provisioning step, which has no nft runner
and so no view of live state), and `Manager.Create`'s self-heal branch for a
table that has been destroyed out of band, where there is nothing live to
snapshot.

## Coexistence with the host's existing network stack

A provisioned node already has other writers in nftables and tc — Cilium, kube-proxy,
possibly `ufw`/`firewalld`, and whatever netplan configured. None of them are displaced,
but the interaction is not symmetric and is worth spelling out.

### nftables: weaver never flushes, but it is always the strictest filter

Both `.nft` files open with the scoped replace idiom rather than a global flush:

```
add table inet weaver-host-firewall
delete table inet weaver-host-firewall
add table inet weaver-host-firewall
```

`add` before `delete` makes the delete succeed on a first-ever load; the pair then makes
re-application idempotent. Crucially the blast radius is one table — weaver never issues
`flush ruleset`, so `iptables-nft` tables (`KUBE-*`, `CILIUM_*`), `ufw`, and `firewalld`
are untouched. Multiple base chains on one hook is a supported nftables pattern; the two
weaver tables simply register alongside the others.

What that pattern does **not** give you is additive permissiveness. Within a base chain,
`accept` ends evaluation *of that chain only* — the packet still traverses every other base
chain registered on the same hook. A `drop` (or `reject`) is final for the packet across all
of them. So on any hook where a weaver chain is `policy drop`, weaver is the binding filter:
nothing Cilium or kube-proxy accepts can rescue traffic weaver does not match.

The two tables sit on opposite sides of that line, and the distinction matters:

| Hook | Table | Chain policy | Role |
|---|---|---|---|
| `prerouting` (priority `raw`, −300) | host firewall | `accept` | Drops the operator block list ahead of conntrack. Covers the forward path too, so a blocked CIDR is blocked for pod-bound traffic as well. |
| `input` (priority `filter`, 0) | host firewall | `drop` | **Enforcing.** Anything not explicitly accepted is dropped. |
| `output` (priority `filter`, 0) | host firewall | `accept` | Block-list symmetry only — drops traffic *to* a blocked CIDR. Deliberately not an egress allowlist. |
| `forward` (priority `filter`, 0) | workload policy | `accept` | **Classifying.** Stamps `meta priority` for the HTB hierarchy; the only drops are the deny tier — the `bn-restricted` quarantine and the `bn-health` port lockdown. |

The block list is spelled on three hooks because one is not enough. Dropping a peer inbound
does not stop the host from dialing it, and once the host initiates, the replies come back in
under `ct state established` — so an inbound-only block list does not block the connection at
all. The `input` copy is redundant with `prerouting` for anything arriving on a wire; it is
kept so the block list's ordering relative to the conntrack fast-path stays a property of the
`input` chain itself rather than a consequence of a chain on another hook.

On `input`, the only broad escapes are the mgmt allowlist on `mgmt_ports`, `in_cluster_ports`
from the pod CIDR, whatever named allow rules the operator declared, the ICMP path-health
subset, and `ct state established,related`. Everything else delivered to that host is dropped on
new connections. On a single-purpose block-node host that is the intent, but it is a node-wide
decision, not a block-node-scoped one — a second CNI, a docker bridge, a VPN, DHCPv6, or
cross-node kubelet/etcd/NodePort traffic all need an explicit rule or they are dropped.

Those explicit rules are what the **named allow rules** are for. Each is a source list x port
list x protocol accept, rendered per family as
`<family> saddr @<name> <proto> dport @<name>_ports accept` into `input_ipv4` / `input_ipv6`.
They cover the axes the three reserved blocks cannot: UDP (Cilium's VXLAN 8472), port ranges
(`2379-2380`, `10256-10259`), more than one management group, and per-source unmetered ICMP
echo. They exist because weaver has to be able to express a *complete* host ruleset on hardware
where no external configuration management supplies one.

A rule's addresses are one mixed-family list; `splitCIDRs` routes each entry to `@<name>`
(`ipv4_addr`) or `@<name>6` (`ipv6_addr`) and the rule is emitted only into the chains whose
family has members.

Every set in this table — addresses as well as ports — carries `flags interval` + `auto-merge`.
On a port set the interval flag is what lets a range be a single element. On an address set
auto-merge is what makes overlapping prefixes legal: without it, adding `10.0.0.5/32` to a set
already holding `10.0.0.0/24` makes nft reject the whole document with *conflicting intervals
specified*, which a plain `firewall add --cidr` can reach. The cost is that the live set reads
back merged differently from what was written, so the persisted config — not the kernel — is the
source of truth. `firewall show` dumps the kernel and will print folded prefixes;
`firewall show --output yaml` reads the config and shows what the operator authored.

Because the kernel is not authoritative, a rejected ruleset must never reach disk either. Every
mutation renders the document, dry-runs it with `nft -c -f`, and only then writes
`network-weaver-host-firewall.yaml` and `.nft` and restarts the unit. The unit has no `ExecStop`,
so a failed load leaves the live table intact and looks harmless — but the persisted artifact is
what replays at boot, and an unloadable one means the host comes up with no weaver firewall at
all. Relatedly the unit sets `StartLimitIntervalSec=0`: it is restarted on every mutation, so
systemd's default start rate limit would otherwise turn one bad apply into an opaque
`start-limit-hit` on every later command until someone ran `systemctl reset-failed`.

Two things stay structural and no rule can remove them: the IPv6 ND/MLD accepts with their
hop-limit 255 guard (IPv6 is non-functional without them), and the ICMP rate meter. An
`icmp_echo` rule renders *above* the meter, because the meter drops over-budget echo outright —
an accept placed after it would never be reached under a flood, which is exactly when an
operator needs their own ping to work.

Block-node service ports deliberately have no home here. That traffic is forwarded rather than
delivered locally, so an `input` rule for it would never match; peer access to block-node ports
is the workload policy plane's concern.

On `forward`, weaver constrains nothing. A packet matching no classification rule is accepted
carrying no `meta priority` and lands in the HTB default class. Workload isolation on that hook
rests entirely on Cilium — which also means a host whose Cilium datapath is degraded or not yet
up has no weaver-side backstop for forwarded traffic.

### tc: why the HTB hierarchies do not fight Cilium

Three interactions, each already resolved, each fragile enough to be worth naming:

- **`tc qdisc del dev <nic> root` does not disturb Cilium's datapath.** Cilium attaches its
  BPF programs to `clsact` (handle `ffff:`), which is a distinct qdisc from `root`. Both
  the boot-replay script and `ApplyIngressVeth` delete and rebuild `root` only, so
  `from-netdev`/`to-netdev` and the per-endpoint programs survive untouched.
- **Cilium's Bandwidth Manager is a hard conflict.** It installs `mq`+`fq` on native devices
  and is the only other BPF writer of `skb->priority` — it would void the classification the
  policy plane stamps. `migration_cilium_host_legacy_routing.go` fails fast when
  `enable-bandwidth-manager=true` rather than letting the two fight.
- **`bpf_redirect_peer()` would bypass the veth qdisc entirely.** With BPF host routing,
  Cilium moves packets straight into the pod netns, so an egress HTB on the host-side veth
  would never see a packet and the ingress plane would be silently dead code. This is why
  `bpf.hostLegacyRouting: true` is pinned in `cilium-config.yaml` — it is a shaper
  requirement, not a performance preference.

**netplan** does not manage qdiscs: systemd-networkd only touches them when a
traffic-control section is present, which netplan does not emit. It can therefore conflict
only indirectly, by recreating the egress device (see the gaps below).

### Known gaps

Coexistence holds while everyone leaves everyone else alone. Nothing currently re-asserts
weaver state when a third party removes it, and every such loss is silent:

| Trigger | Effect | Tracked by |
|---|---|---|
| `nftables.service` starts or restarts (stock `/etc/nftables.conf` begins with `flush ruleset`); a `firewalld` reload; an operator's `nft -f` with a flush | Both weaver tables destroyed. Host firewall gone; policy plane gone, so nothing stamps `meta priority` and every flow falls to the HTB default class at wire speed — no error, no counter, no log | #981 (re-assert), #982 (unit ordering + install preflight) |
| `netplan apply` recreating the egress device, a driver reload, a stray `tc qdisc del` | `$EGRESS` HTB hierarchy gone; no egress shaping | #981 |
| Egress interface is a netplan-created bond, bridge, or VLAN | Fixed in #980: the unit runs `After=network-online.target` and the script polls `/sys/class/net` for `SHAPER_DEVICE_WAIT_SECS` before its first `tc` call. A device later than that budget stays unshaped until the next converge | #980 (fixed) |

The daemon's hourly force-resync reconciles nft **set membership** and the per-pod `$VETH`
hierarchy; it does not verify that the tables or the `$EGRESS` root qdisc still exist.
Until that changes, `systemctl status` on the two loader units and the inspection commands
at the end of this document are the only signal.

## Operator setup and configuration

### Turning it on at install time

Both features are opt-in on `block node install` (and `block node reconfigure`),
gated by two independent flags. In the interactive flow the **host firewall is
asked first**, then traffic shaping:

- `--firewall-enabled` — install the `inet weaver-host-firewall` plane.
  Configured by `--mgmt-cidrs`, `--blocked-cidrs`, `--mgmt-ports`, `--pod-cidr`,
  `--in-cluster-ports` — i.e. the three reserved blocks only. Named allow rules
  are not part of install: they are declared afterwards with
  `network firewall create-allow-rule` (or `create --from-file` for the whole
  table), and a later `reconfigure` preserves them.
  `--mgmt-cidrs` alone also accepts FQDNs — see below.
- `--traffic-shaping-enabled` — the single switch that wires up **all three**
  shaping pieces: the workload policy plane (`inet weaver-workload-policy`), the
  tc HTB hierarchies, and the traffic-shaper daemon. Only when this is accepted
  does install prompt for the egress NIC (`--egress-interface`) and its line rate
  (`--link-rate`, accepts `auto`), take per-class overrides via repeatable
  `--shape <class>=rate=<r>,ceil=<c>,prio=<p>`, and set the daemon poll cadence
  with `--statusz-poll-interval`.

Gating precedence is flag > interactive confirm > seed default; supplying a
content flag without its gate flag is rejected. `reconfigure` seeds each gate
from the persisted decision, so a no-flag reconfigure never silently tears a
plane down.

### FQDNs in mgmt and allow rules

`--mgmt-cidrs`, and `--cidr`/`--cidrs` on a declared allow rule, accept fully-qualified domain
names alongside IPv4 CIDRs (`Rule.acceptsFQDN`); every other address flag on either plane
stays literal-only — the block list and the in-cluster pod CIDR because a resolver outage or a
DNS answer must never change what they match, `network policy` because its plane is
kernel-authoritative rather than YAML-authoritative (see below). The parse rule is that an
entry containing `/` is an address and anything else is a name, which is unambiguous because a
maskless IP is already rejected.

The mechanism is deliberately small, and rests on the fact that the YAML is this
table's source of truth:

- Names are stored verbatim in `Rule.CIDRs`, so **re-rendering is re-resolving**
  and no separate mapping has to be kept in sync.
- Resolution happens into a *copy* of the table (`Table.expandFQDNs`), which is
  then rendered; the original is what gets marshalled to YAML. That is what keeps
  the name in the config and only literals in the `.nft`, by construction rather
  than by a guard — and it matters, because `nft` resolves a bare name itself at
  ruleset-load time, which would bypass the resolver, the cache and the
  never-empty rule below.
- `splitCIDRs` errors on anything that is not a CIDR. Skipping it instead would
  render `set mgmt_addrs { … }` with no `elements` clause, which `nft -c -f`
  accepts and which drops every new SSH connection.

`solo-provisioner-network-dns-refresh.timer` runs `network firewall refresh-dns`
a minute after boot and every five minutes after, and is installed only while the
config holds at least one name. A fixed interval, not the record TTL: stdlib
`net.Resolver` does not report one.

Lookups go through the node's own `/etc/resolv.conf` — host netns, as root, not
cluster DNS. They run concurrently under a single two-second budget for the whole
pass, so a slow resolver costs one round trip rather than N; sequentially, a
handful of names behind a slow server would exhaust the budget and report
perfectly reachable names as unresolvable. `Resolver` implementations must
therefore be safe for concurrent use. Results are folded back in list order, not
completion order, so warnings and the operator-facing error name things in the
order they were written.

Note the two release binaries can differ here: `linux/amd64` is built natively
and may use libc/NSS, while `linux/arm64` is cross-compiled with cgo disabled and
uses Go's own resolver. Plain DNS and the systemd-resolved stub behave the same on
both; a name resolvable only through an NSS module (LDAP, mDNS) does not.

It is a timer rather than a loop in the daemon because this table is node-level
and exists on hosts with no block node, while the shaping monitor idles until it
discovers a BN pod — and because the daemon takes the shared apply lock
non-blocking so it always yields to an operator, which a resolver-latency-bound
apply in that loop would undermine.

`…-host-firewall.dns.json` records what each name last resolved to. It exists
purely for **per-name attribution**: "keep the last-known addresses on failure"
is only well defined when every name fails together, and when one name fails
while another rotates, a merged `mgmt_addrs` set cannot say which member came
from which name. Not a source of truth; a missing or corrupt file degrades to
"no last-known addresses".

If resolution would leave `mgmt_addrs` empty the apply is refused outright, with
no `--force` — distinct from `checkMgmtLockout`, which compares a mutation's
before and after and so cannot see this case. An allow rule left empty by
resolution is not refused the same way: `Rule.mustResolveToSomething` is what
draws that line, true only for `mgmt`, so `checkResolvedRule` treats every
other FQDN-accepting rule as a warning (`Table.IncompleteAllowRules`, evaluated
against the resolved table) rather than a hard failure — losing SSH access
invisibly is a different order of risk than losing one rule's traffic.

**Trust boundary.** Whoever controls the answer for a name in `mgmt` controls
who can reach SSH on the node; a name in an allow rule controls who can reach
whatever that rule admits. Resolution also happens on the node, so
split-horizon DNS can differ from what the operator sees. Both are called out in
`docs/commands/network/firewall.md`; prefer a literal or an `/etc/hosts` pin on
high-value nodes.

Out of scope for now: IPv6/AAAA, TTL-aware polling, and names anywhere on the
workload policy plane.

### Adjusting a live node with the `network` commands

The three `network` sub-scopes drive each plane directly; every mutation live-
applies and then persists (see below), so they are safe to run by hand on a
provisioned node.

- **`network firewall`** (`create`/`create-allow-rule`/`add`/`remove`/`set`/
  `show`/`reapply`/`refresh-dns`/`delete`) — the host firewall. `create` takes `--mgmt-cidrs`,
  `--blocked-cidrs`, `--in-cluster-ports`, `--mgmt-ports`, `--pod-cidr`, or
  `--from-file` for the whole table; `create-allow-rule` declares one named allow
  rule (`--name`, `--proto`, `--icmp-echo`);
  `add`/`remove`/`set`/`delete` take `--name` to address one rule —
  a reserved block (`mgmt`, `blocked`, `in_cluster`) or a named allow rule — with
  the per-block flags retained as shorthands. Structure (which rules exist, and
  their protocol) is declared by its own verb, so bringing a rule into existence
  is always explicit; membership is moved by `add`/`remove`/`set`, which refuse
  an unknown `--name` so a typo edits nothing. `set` and `remove` additionally
  refuse to take the `mgmt` rule from populated to empty — either its address
  list or its port list, since the rule renders unconditionally as
  `saddr @mgmt_addrs tcp dport @mgmt_ports accept` and either half empty drops
  every new SSH connection under the default-drop input chain — unless
  authorised with the root `--force` flag (#1034); a rule that is already
  unreachable stays editable. A declared rule may be empty and
  renders nothing until it has a CIDR and either a port or `icmp_echo`.
  `show --output yaml` emits the same schema `--from-file` accepts. A file is
  the whole table and inherits nothing from the host, so all three reserved
  blocks must be stated in it (as must `cidrs` inside `mgmt` and `blocked`) —
  otherwise a file that forgot `mgmt` would render an empty management
  allowlist under the default-drop policy.
  `create` and `delete --all` also record the enable/disable decision into
  `machineState.firewall.disabled`, the same field the block-node workflow
  writes, so the standalone verbs and `block node reconfigure` share one source
  of truth (issue #1003). Only the decision is mirrored: the ruleset itself stays
  in `/etc/solo-provisioner/network-weaver-host-firewall.yaml`, which
  `ResolveHostFirewallConfig` reads as a precedence tier *above* machine state —
  otherwise a reconfigure's force re-render would revert an urgent
  `add --name mgmt --cidr …` back to the allowlist captured at install time.
- **`network policy`** (`create`/`add`/`remove`/`set`/`show`/`delete`) — the
  workload policy plane. `create` takes `--name` (the nft set name), `--stamp`
  (the HTB class to classify into, which also fixes direction) or `--deny`, plus
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
  `network-weaver-host-firewall.yaml` and then `network-weaver-host-firewall.nft`
  (config first: a crash between the two leaves the operator's intent recorded
  and the kernel merely stale, which the next apply fixes), then
  `EnsureNetworkNftUnit` installs and enables
  `solo-provisioner-network-nft.service` and restarts it. The step owns only the
  reserved blocks, which come from `config.yaml` / the install flags; it carries
  any named allow rules across unchanged, so a `reconfigure` force re-render does
  not drop rules `config.yaml` has no field for.
- **Workload policy** — `NftWeaverPersist`
  (`internal/workflows/steps/step_network_nft_weaver.go`) re-renders
  `network-weaver-workload-policy.nft` from the policy registry, ensures the
  shared nft unit, and restarts it.
- **Bandwidth shaper** — `TcEgressPersist`
  (`internal/workflows/steps/step_network_tc_egress.go`) renders
  `solo-provisioner-bandwidth-shaper.sh` and `EnsureTcEgressUnit` installs and
  enables `solo-provisioner-bandwidth-shaper.service`. `TcIngressRecord` does the
  same for the `$VETH` classes, config only — no script.

Both tc steps re-provision the shape registry on every run, so the rule that
keeps a `reconfigure`/`upgrade` from undoing day-2 tuning lives one layer down,
in `shape.mergeExistingConfig`: **the registry is the source of truth for
per-class values, and only a changed trunk rate rebalances them.** When the
resolved `--link-rate` is the same bandwidth already recorded on the device
(compared in bits per second, so `1gbit` and `1000mbit` are the same trunk), each
class keeps its recorded `rate`/`ceil`/`prio` and its `created_at`; a genuinely
different trunk rate recomputes every class at the profile proportions. Per-class
`--shape` overrides supplied on the run are merged on top either way, so they
still win. This matters because both `reconfigure` and `upgrade` resolve the link
rate back from `blockNodeState.shaping`, so the rate is never empty on a
converged host and cannot signal "the operator asked for new shaping on this
run" (issue #1037). For the same reason the persisted `shapeOverrides` are *not*
re-asserted as effective inputs: that would replay an install-time `--shape` over
a later `network shape set` on the same class.

The device's `default_class` is operator-owned the same way: one set with
`network shape create --device --default` survives every re-provision, in the
registry and in the boot script's `htb default <minor>`. A recorded class the
provision no longer writes falls back to the profile value with a warning —
kept, it would name a class the script never creates and unmatched traffic
would stop being shaped. The device `rate` is not preserved like this:
`reconfigure`/`upgrade` re-assert it from `blockNodeState.shaping` as above.

Running any equivalent `network firewall` / `network policy` / `network shape`
command by hand takes the same live-apply-then-persist path, guarded by a shared
flock under `/run/solo-provisioner/network/` so a hand-run command and the daemon
poll loop never interleave nft transactions.

## Inspecting a running node

```bash
# nft: table definitions (rules persist; set elements are daemon-managed)
sudo nft list table inet weaver-host-firewall
sudo nft list table inet weaver-workload-policy

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
