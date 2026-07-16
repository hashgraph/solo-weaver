# Traffic-shaper MVP demo (UTM VM)

This runbook drives the block-node traffic-shaper end to end on a UTM VM: a
mock statusz server feeds a roster to the `solo-provisioner-daemon` poll loop,
and you watch the live nftables set membership follow roster edits. It then
extends to multiple VMs to observe per-category traffic shaping.

**What you will observe:** nftables set *membership* changing within one poll
interval (default 5s) as the roster changes. The tc HTB class hierarchy is
static (installed once); traffic is classified into those classes by nftables
`skb->priority` marking, so you will not see tc rules change at runtime — you
will see traffic land in different classes as set membership changes.

> **Status.** Steps 1-3 (static-plane policies, mock server, daemon.yaml config +
> daemon install) work with the current stories. The live convergence in Step 4
> and the shaping in Step 5 depend on the **daemon poll loop**, which lands in a
> follow-up TS_3 story (it applies nft via the daemon's `privexec`
> sudo-delegation). Until that lands, the mock server + config are in place but
> the sets are not yet reconciled automatically.

## Prerequisites

- A provisioned block-node VM: `sudo solo-provisioner block node install`. Note
  this lays down the **host firewall** (`inet host`), the **egress tc** HTB root,
  and the **daemon.yaml enablement** — but **not** the `inet weaver` policies.
  Those are created in Step 1 below with `network policy create` (the install-time
  orchestration that would chain them automatically is not yet implemented).
- The daemon binary and the mock statusz server built for the VM's arch.

```bash
task build:cli GOOS=linux GOARCH=arm64      # solo-provisioner + daemon
GOOS=linux GOARCH=arm64 go build -o bin/statuszmock \
  ./internal/daemon/blocknode/statuszmock/cmd
```

## Step 1 - create the BN policies (static plane)

`block node install` does not create the `inet weaver` policies yet, so create
the four the monitor drives. The names must match the monitor's category mapping
exactly, and each `--stamp` references one of the fixed class names (`publisher`,
`partner`, `reserve-egress`, `backfill-response`, ...). The first `create` also
creates the `inet weaver` table:

```bash
sudo solo-provisioner network policy create --name bn-publisher  --stamp publisher --ports 40840
sudo solo-provisioner network policy create --name bn-partner    --stamp partner   --ports 40980,40981
sudo solo-provisioner network policy create --name bn-restricted --deny
sudo solo-provisioner network policy create --name bn-backfill    --stamp reserve-egress --reply-stamp backfill-response
```

| category (statusz) | policy / nft set | create flags |
|---|---|---|
| publisher | bn-publisher | `--stamp publisher --ports 40840` |
| partner | bn-partner | `--stamp partner --ports 40980,40981` |
| restricted | bn-restricted | `--deny` |
| peer_bn | bn-backfill | `--stamp reserve-egress --reply-stamp backfill-response` (compound ip:port) |

Confirm the table and its sets exist and are empty (no roster applied yet):

```bash
sudo nft list table inet weaver
# expect sets: bn-publisher, bn-partner, bn-restricted, bn-backfill (empty)
```

## Step 2 - start the mock statusz server

Create a roster and serve it:

```bash
cat > roster.json <<'JSON'
{
  "inbound": [
    { "remote": {"address": "10.10.1.0/24", "port": "*"}, "category": "publisher" },
    { "remote": {"address": "10.20.1.0/24", "port": "*"}, "category": "partner" }
  ],
  "outbound": [
    { "remote": {"address": "10.30.5.7", "port": "43473"}, "category": "peer_bn" }
  ]
}
JSON

./bin/statuszmock --addr 127.0.0.1:8080 --roster roster.json &
```

Sanity check:

```bash
curl -s 127.0.0.1:8080/statusz/inbound-clients | jq .
curl -s 127.0.0.1:8080/statusz/outbound-clients | jq .
```

## Step 3 - install the daemon, point it at the mock, and start it

`daemon.yaml` lives at **`/opt/solo/weaver/config/daemon.yaml`**. `block node
install` already wrote its `block_node` block (enabled, kubeconfig, orbit,
`traffic_shaper: true`) — but the daemon **binary and systemd service are
installed separately**; `block node install` does not install them.

### 3.1 Install the daemon service

```bash
sudo solo-provisioner daemon service install --components block-node --bn-orbit block-node
```

> **Prerequisite — `daemon-bn.kubeconfig`.** The traffic-shaper monitor builds a
> Kubernetes client from `/opt/solo/weaver/config/daemon-bn.kubeconfig` at
> startup; if that file is missing, the daemon fails to start the block-node
> component. Provisioning it automatically at install is a known gap, so for the
> demo provide one manually — the cluster's admin kubeconfig works:
>
> ```bash
> sudo cp <cluster-admin-kubeconfig> /opt/solo/weaver/config/daemon-bn.kubeconfig
> ```
>
> The pod-lifecycle watcher may still fault without real pod access — that's
> absorbed and retried; the statusz poll loop against the local-fallback URL runs
> independently of it.

### 3.2 Add the statusz source

Edit `/opt/solo/weaver/config/daemon.yaml` and add the `statusz` block under
`block_node` so the monitor polls the mock (the rest of the block is already
there):

```yaml
components:
  block_node:
    enabled: true
    kubeconfig: /opt/solo/weaver/config/daemon-bn.kubeconfig
    orbit: block-node
    monitors:
      traffic_shaper: true
    statusz:                        # add this
      base_url: http://127.0.0.1:8080
      poll_interval: 5s
```

### 3.3 Restart and verify it's up

```bash
sudo systemctl restart solo-provisioner-daemon
solo-provisioner daemon service check          # health + component status (alias: status)
systemctl status solo-provisioner-daemon       # or plain systemd status
journalctl -u solo-provisioner-daemon -f       # watch the poll loop log
```

You should see `statusz poll loop starting` and, once the first tick applies,
`applied membership deltas`. If the service is **not active**, `journalctl` shows
why — most commonly the missing `daemon-bn.kubeconfig` from the prerequisite above.

## Step 4 - watch membership converge

In another shell on the VM:

```bash
watch -n1 'sudo nft list table inet weaver | sed -n "/set bn-/,/}/p"'
```

Within one poll interval the sets fill from the roster:

- `bn-publisher` -> `10.10.1.0/24`
- `bn-partner`   -> `10.20.1.0/24`
- `bn-backfill`  -> `10.30.5.7 . 43473`

Now edit `roster.json` (no restart needed - the mock re-reads it, the daemon
re-polls it):

```bash
# e.g. add a publisher CIDR and drop the partner
cat > roster.json <<'JSON'
{
  "inbound": [
    { "remote": {"address": "10.10.1.0/24", "port": "*"}, "category": "publisher" },
    { "remote": {"address": "10.11.0.0/16", "port": "*"}, "category": "publisher" }
  ],
  "outbound": []
}
JSON
```

Within ~5s: `bn-publisher` gains `10.11.0.0/16`, `bn-partner` empties, and
`bn-backfill` empties. Stop the mock server (or block its port) and confirm the
sets are **left as-is** - an outage keeps the last-good state, it does not drop
rules.

## Step 5 - traffic shaping across the category VMs (optional)

Steps 1-4 prove the statusz -> nft membership loop. This step proves the payoff:
traffic from each category lands in the right HTB class at the rate the profile
budgets for it. It mirrors the v4 companion POC
(`bn-qos-multiclass-priority-poc-v4-nft-priority.md` in the traffic-shaper repo),
scaled here to a **100 Mbit** link so the ratios read as round numbers.

### Bandwidth budget at `--link-rate 100mbit`

The per-class floors and ceilings are the design's profile ratios applied to a
100 Mbit parent. Install the static plane with `block node install
--link-rate 100mbit` so these classes exist:

| Attach point | Class | Role | floor | ceil | prio |
|---|---|---|---|---|---|
| `$VETH` (ingress) | 1:10 | Publisher | 80 Mbit | 100 Mbit | 0 |
| `$VETH` (ingress) | 1:20 | Backfill response | 10 Mbit | 100 Mbit | 7 |
| `$VETH` (ingress) | 1:30 | Reserve (subscribe req, mgmt) | 10 Mbit | 100 Mbit | 1 |
| `$EGRESS` (egress) | 1:40 | Partner subscriber | 40 Mbit | 70 Mbit | 0 |
| `$EGRESS` (egress) | 1:50 | Public subscriber / status | 30 Mbit | 70 Mbit | 5 |
| `$EGRESS` (egress) | 1:60 | Reserve (backfill req, ACKs) | 30 Mbit | 100 Mbit | 1 |

Sanity-check the ratios: **each direction's floors sum to exactly the 100 Mbit
parent** (ingress 80+10+10; egress 40+30+30), so guaranteed capacity is fully
allocated — none over- or under-subscribed — and every `ceil >= floor` lets an
otherwise-idle class be borrowed against. Consequences to look for:

- Under full concurrent load in one direction, each class is pinned to its floor
  (the floors already sum to the parent, so there is nothing left to borrow).
- When a higher-priority class goes idle, a lower one borrows up to its ceil.

> The `$EGRESS` HTB is installed by `block node install` (the egress tc plane),
> so the egress checks (1:40/1:50/1:60) work with what this PR + install provide.
> The `$VETH` ingress HTB is installed by the daemon's pod-lifecycle watcher — a
> later story — so until that lands, set up the ingress classes manually per the
> POC (Stage 2) to exercise the 1:10/1:20/1:30 checks.

### Category VMs and roster

| VM | Roster category | Exercises |
|---|---|---|
| `solo-weaver-cn` | `publisher` | ingress publisher class 1:10 |
| `solo-weaver-partner` | `partner` | egress partner class 1:40 |
| `solo-weaver-public` | (unmapped `public`) | egress public class 1:50 |
| `solo-weaver-peer` | `peer_bn` | backfill: egress 1:60 req / ingress 1:20 resp |

Put each VM's address in `roster.json` under its category (publisher/partner/
peer_bn); the public VM stays **unmapped** (a `public` source is any-source, not
a set member, so it falls through to the public class). Reload is automatic — the
mock re-reads the file, the daemon re-polls it.

### iperf3 setup

```bash
# on each category VM
sudo apt-get install -y iperf3

# four listeners in the BN pod (one per BN port), plus the peer backfill target
for p in 40840 40980 40981 40982; do kubectl exec deploy/bn -- iperf3 -s -p $p -D; done
# on the peer VM:
iperf3 -s -p 43473 -D
```

On the BN host resolve the attach points (`$EGRESS` from the default route;
`$VETH` per the POC "discover the pod's host-side veth" step). Per-class Mbit/s is
computed by diffing each class's `Sent` bytes over time — use the streaming
`tc_rates` helper from the v4 POC, or diff by hand:

```bash
export EGRESS=$(ip route | awk '/default/{print $5; exit}')
sudo tc -s class show dev $EGRESS classid 1:40   # note "Sent N bytes", wait 5s, repeat
# Mbit/s = (N2 - N1) * 8 / (5 * 1e6)
```

### Per-category baseline (no contention)

Run one category at a time for ~20s; ≥95% of the bytes should land in that
category's class, the others idle.

| Driver VM | Command | Bytes land in |
|---|---|---|
| cn (publisher) | `iperf3 -c $BN_LB_IP -p 40840 -t 20` | `$VETH` 1:10 |
| partner | `iperf3 -c $BN_LB_IP -p 40980 -t 20 -R` | `$EGRESS` 1:40 |
| public | `iperf3 -c $BN_LB_IP -p 40981 -t 20 -R` | `$EGRESS` 1:50 |

Check after each: `sudo tc -s class show dev <DEV> classid <CLASS>`.

### Contention (floors hold, priority preempts)

Start all categories at once for 90s and watch the rates converge to the floors:

```bash
# cn:      iperf3 -c $BN_LB_IP -p 40840 -t 90 &                              # publisher -> 1:10
# partner: iperf3 -c $BN_LB_IP -p 40980 -t 90 -R &                           # partner   -> 1:40
# public:  iperf3 -c $BN_LB_IP -p 40981 -t 90 -R &                           # public    -> 1:50
# BN pod:  kubectl exec deploy/bn -- iperf3 -c $PEER_VM_IP -p 43473 -t 90 -R & # backfill

# BN host, two terminals (tc_rates from the v4 POC):
tc_rates $VETH   1:10,1:20,1:30 2
tc_rates $EGRESS 1:40,1:50,1:60 2
```

Expected steady-state during the overlap window (100 Mbit link):

| Class | Expected | Why |
|---|---|---|
| `$VETH` 1:10 | ~80 Mbit/s | publisher floor, prio 0 |
| `$VETH` 1:20 | ~10 Mbit/s | backfill response, prio 7 yields |
| `$VETH` 1:30 | ~10 Mbit/s | reserve |
| `$EGRESS` 1:40 | ~40 Mbit/s | partner floor, prio 0 |
| `$EGRESS` 1:50 | ~30 Mbit/s | public floor, prio 5 |
| `$EGRESS` 1:60 | ~30 Mbit/s | reserve |

Each device's classes sum to ~100 Mbit/s and no class exceeds its ceil. Now kill
the publisher (`kill %1` on the cn VM): 1:20 should ramp toward ~100 Mbit/s within
~5s as it borrows the freed prio-0 capacity up to its ceil — the borrowing proof.

There are **no tc filters** anywhere (`tc filter show dev $VETH` / `$EGRESS` are
empty); classification is entirely by nft `skb->priority`. Finally, move a source
between categories in `roster.json` and confirm new connections reclassify on the
next poll interval (existing flows keep their class until they close).

## Cleanup

```bash
kill %1                                   # stop the mock server
sudo systemctl restart solo-provisioner-daemon   # or set traffic_shaper: false
```

## See also

- `docs/dev/daemon/traffic-shaper-statusz.md` - statusz endpoints, category
  mapping, config schema, and the mock server reference.
