# `network reassert` — restore state a third party removed

Checks that weaver's live network state is still in the kernel and restores whatever is
missing: the two nftables tables and the `$EGRESS` HTB root qdisc.

> **Flags not listed on this page.** Every command here also accepts the
> [global flags](../../reference/global-flags.md) — `--config`, `--output`, `--log-level`,
> `--force`, `--verbose`, `--non-interactive`.

Most operators never run this. The `solo-provisioner-daemon` execs it once a minute, so
recovery is automatic and needs no human. Run it by hand to inspect state, or to recover a
host that has no daemon.

## Why it exists

Three things outside weaver's control destroy its network state, and every one of them is
silent:

| Trigger | What is lost |
|---|---|
| `nftables.service` starts or restarts — stock `/etc/nftables.conf` opens with `flush ruleset` — or a `firewalld` reload, or an `nft -f` carrying a flush | **Both** weaver tables |
| `netplan apply` recreating the egress device, a driver reload, a stray `tc qdisc del dev <nic> root` | The `$EGRESS` HTB hierarchy |

Nothing errors and no counter moves. The node keeps serving traffic — just unfiltered, with
no default-drop on the input chain, and unshaped, with every flow in the HTB default class at
wire speed.

## Usage

```bash
# Report what is missing, change nothing
sudo solo-provisioner network reassert --check

# Restore anything missing
sudo solo-provisioner network reassert

# Machine-readable — what the daemon execs
sudo solo-provisioner network reassert --output json
```

| Flag | What it does | Default |
|---|---|---|
| `--check` | Report what is missing without restoring anything | `false` |
| `--output json` | Emit the report as one JSON line tagged `"type":"reassert"` instead of a table (global flag, `-o`) | `text` |

Under `--output json` the report is a single compact line carrying `"type":"reassert"` and
stdout carries nothing else — log events go to stderr (see
[global flags](../../reference/global-flags.md)). Select the report by its tag anyway, the
same rule the workflow summary follows, so a merged stream (`2>&1`) still parses:

```bash
sudo solo-provisioner network reassert -o json | jq -c 'select(.type=="reassert")'
```

Sample output:

```text
ARTIFACT          STATE            DETAIL
host-firewall     restored
workload-policy   present
egress-qdisc      not-provisioned  not provisioned on this host (/usr/local/sbin/solo-provisioner-bandwidth-shaper.sh absent)
```

| State | Meaning |
|---|---|
| `present` | Live in the kernel; nothing to do |
| `not-provisioned` | This host never installed that plane; not an error |
| `MISSING` | Gone from the kernel. Under `--check` only — a normal run restores it |
| `restored` | Was missing, has been put back |
| `UNRECOVERED` | The repair ran but the artifact is still gone — see [when a repair does not work](#when-a-repair-does-not-work) |
| `unknown` | Presence could not be determined, so nothing was touched — see [what it will not do](#what-it-will-not-do) |
| `skipped` | An operator apply held that plane's lock, so the run neither probed nor repaired it — see [what it will not do](#what-it-will-not-do) |

## What it checks, and where

Each plane is checked only where it is provisioned, decided from that plane's own persisted
artifact rather than from any config flag or stored decision. A host that never installed a
firewall is not missing one.

| Artifact | Provisioned when this exists | Restored by restarting |
|---|---|---|
| `host-firewall` | `/etc/solo-provisioner/network-weaver-host-firewall.nft` | `solo-provisioner-network-nft.service` |
| `workload-policy` | `/etc/solo-provisioner/network-weaver-workload-policy.nft` | `solo-provisioner-network-nft.service` |
| `egress-qdisc` | `/usr/local/sbin/solo-provisioner-bandwidth-shaper.sh`, and that script builds a hierarchy | `solo-provisioner-bandwidth-shaper.service` |

Both tables are replayed by the same loader unit, so a flush that took both costs one restart,
not two. Set membership comes back with them: it is rendered inline into the `.nft` document
on every membership change, so the replay restores the roster as well as the structure.

The boot script is also the only record of **which** interface is shaped — `network shape`
stores the device direction, never an interface name. The check reads the NIC back out of the
script rather than detecting the default route, so a host where `--egress-interface` pinned a
non-default NIC has that NIC probed and not another one.

Disabling traffic shaping (`block node reconfigure` with it turned off) does not remove the
script. It leaves a teardown render that pins the NIC but adds no root qdisc, so the unit
keeps the NIC unshaped across reboots. That reads as `not-provisioned`, not `MISSING`: the
hierarchy is absent by decision, and restarting the unit would only delete it again.

## What it will not do

**It never re-renders anything.** Repair means restarting the unit that replays the artifact
already on disk. Nothing is regenerated from config, so a reassert cannot overwrite a hand
edit, and it cannot repair a host whose artifacts are themselves wrong — that is
[`network firewall reapply`](firewall.md#reapply--re-assert-the-persisted-config) or a
re-run of `block node reconfigure`.

**It never interrupts an operator.** Every mutating `network firewall`, `network policy` and
`network shape` verb holds a per-plane apply flock for the duration of its nft/tc transaction.
A reassert takes those same two locks — `/run/solo-provisioner/network/.applying` for the nft
tables and `.tc-applying` for the shaper — without blocking, *before* it probes. Taking them
before the probe rather than merely before the restart is the point: it stops a half-applied
ruleset from reading as `MISSING` and triggering a unit restart underneath the apply that is
mid-flight. A plane whose lock is held is reported `skipped` and left alone for that run; the
daemon leaves its counters untouched and picks it up on the next tick a minute later.

Because the locks are held for the whole run, the run itself is bounded: every probe and
restart shares a two-minute deadline. A `systemctl restart` that never completes, or an `nft`
that never answers, is cut off, reported as `UNRECOVERED` or `unknown` with `deadline exceeded`
in the detail column, and the locks are released. Without that bound a wedged job would hold
the flocks that every `network firewall` / `policy` / `shape` verb waits on — blocking
operators indefinitely, since the nft loader is a `oneshot` and systemd puts no start timeout
on those by default. The deadline is enforced by this command, not by the daemon: the daemon
killing its `sudo` child would leave the root-owned CLI running with the locks still held. The
daemon adds a longer three-minute backstop around the exec only so a hang outside the CLI —
`sudo` itself, say — cannot stall the monitor.

The same "never on a guess" rule shapes the delete verbs. A `network firewall delete` or
`network policy delete` that `nft` could not answer fails and leaves its `.nft` file in place,
so once `nft` answers again the next tick replays the table. Re-run the delete at that point.

`--check` is the exception. It mutates nothing, so it takes no lock and always probes — a
read-only inspection should never be refused because an apply happens to be running.

**It never repairs on a guess.** A probe has three outcomes, not two: present, absent, and
*could not determine*. Only a definite absence is repaired. If `tc` errors because the egress
device no longer exists, or `nft` cannot answer at all — no binary, a broken install, no
kernel nftables support — the artifact is reported `unknown` and left alone. Rebuilding on an unreadable probe would have the daemon tear down and rebuild
a hierarchy every minute against a device that is not there.

### When a repair does not work

Every repair is followed immediately by a second probe, which is what separates two very
different problems:

- **`restored`, over and over.** The repair works; something on the host keeps destroying the
  state. Look at what else writes nftables — an enabled `nftables.service` is the usual
  answer, and the fix is to stop it flushing at boot, not to re-assert harder. The daemon
  reports a climbing `reassert_count` for exactly this.
- **`UNRECOVERED`.** The unit restarted and the artifact still is not there. The repair itself
  is broken — a masked unit, a boot script that exits non-zero, an egress device that never
  appears. Check `systemctl status` on the unit named in the detail column.
- **`unknown` after a repair.** The unit restarted, then the re-probe could not answer; the
  detail column says why. Nothing is claimed either way, and the next run probes again.

## What the daemon does with it

`solo-provisioner-daemon` runs this once a minute. It is unprivileged, so it execs the verb
under sudo — but only after checking, without escalating, that at least one of the three
artifacts exists on disk. A host with no weaver network state costs three `stat` calls a
minute and no more.

Why a minute, and not the hourly force-resync the issue first proposed: that cadence belongs
to the block-node shaper monitor's statusz loop, which reconciles set membership and exists
only where a block node is configured, so the host firewall has no such loop to ride. And
the loss this covers leaves the host's `input` chain wide open, so the gap between loss and
repair is the exposure; an hour of it is not acceptable, a minute is. The healthy-path cost
of a tick is one `sudo` exec of the CLI running two `nft list tables` and one
`tc -j qdisc show`, well under a second of CPU, plus the one line `sudo` writes to the auth
log per escalation. The interval is `reassertInterval` in
`internal/daemon/network/reassert_monitor.go`.

The first check waits two minutes after the daemon starts (`reassertStartupGrace`): at boot the
shaper unit is still waiting for `network-online.target` and its device, and an earlier probe
would restart it under a hierarchy it has not built yet.

Every re-assertion is logged at WARN in the daemon's journal. A failure, whether a probe
that cannot answer or a repair that did not work, is logged at ERROR when it first appears
and at DEBUG on each repeat, so a host stuck that way does not emit an ERROR every minute;
an INFO line marks the artifact coming back. The running history, including
`consecutive_failures`, is reported under the `network` component of the daemon's status
endpoint:

```bash
sudo curl -s --unix-socket /opt/solo/weaver/daemon/daemon.sock http://d/status \
  | jq '.components.network.detail'
```

```json
{
  "last_check_at": "2026-08-31T12:00:00Z",
  "artifacts": [
    {
      "artifact": "host-firewall",
      "expected": true,
      "present": true,
      "reassert_count": 3,
      "last_reasserted_at": "2026-08-31T11:59:00Z"
    }
  ]
}
```

A `reassert_count` that keeps climbing means the repair is working and the cause is not fixed.

> **Privilege:** root, for both `--check` and the full run. Listing an nftables table requires
> `CAP_NET_ADMIN`, so unlike `block node reconcile-shaper` there is no unprivileged check
> path. The daemon invokes it via `sudo`.

> **One host this does not cover.** The daemon only runs where a block node or consensus node
> is configured. A host provisioned with `network firewall create` alone has no daemon, so
> nothing re-asserts its firewall automatically — run this verb from cron or by hand there.

---

## See also

- [Network commands](README.md) — the three planes this spans
- [`network firewall reapply`](firewall.md#reapply--re-assert-the-persisted-config) — re-render the host firewall from its config, a different operation
- [Traffic shaper internals](../../dev/traffic-shaper.md) — the boot units, what survives a reboot, and what the daemon rebuilds
- [Troubleshooting](../../troubleshooting.md)
