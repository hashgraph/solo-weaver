# Block Node traffic-shaper: statusz poll loop & configuration

This document describes how the `solo-provisioner-daemon` block-node
traffic-shaper monitor consumes the Block Node's `statusz` REST endpoints, how
the statusz categories map to network policies, and how the monitor resolves the
statusz endpoint — discovered from the watched BN pod, with an optional
`base_url` override in `daemon.yaml`.

> **The statusz contract is provisional.** The request/response shape below
> mirrors the Block Node's `network-data.proto`. Confirming the contract with
> the Block Node team is a blocking dependency; treat this document as the
> current best understanding, not a frozen interface.

> **Implementation note.** The poll loop applies nft membership through the
> daemon's `privexec` sudo-delegation (the daemon is unprivileged and never calls
> `nft` directly), not the in-process path this doc's prose describes. The monitor
> resolves the statusz endpoint by discovering it from the watched BN pod, with an
> optional `base_url` override.

## What the poll loop does

The traffic-shaper monitor runs a poll loop that, on a fixed cadence
(default 5m):

1. fetches the BN's `statusz/inbound` and `statusz/outbound` endpoints,
2. buckets the returned endpoints by category,
3. diffs the desired membership against the live nftables sets, and
4. applies the per-policy membership deltas.

The loop reconciles **nftables set membership only** — the dynamic plane. The
tc HTB class hierarchy is static (installed once); traffic lands in the correct
class via nftables `skb->priority` marking, not via runtime tc changes.

### Startup and reboot behaviour

The poll loop reconciles **once immediately on entry** — before the first
ticker tick fires. This means:

- After a **daemon restart**, the nft set membership is rehydrated as soon as
  the daemon starts and the BN statusz endpoint is reachable, without waiting
  a full poll interval.
- After a **host reboot**, the nft set elements are not boot-persistent (the
  static table structure is replayed from `.nft` files, but membership is not).
  The entry reconcile rehydrates the sets as soon as the daemon starts and
  statusz responds. If statusz is not yet up, `superviseResponsibility` retries
  with exponential back-off — convergence is bounded by BN startup time, not
  the poll interval.

### Steady-state behaviour

A failed poll (statusz unreachable, diff error, or apply error) is logged and
retried on the next tick, leaving the **last-good** nftables state in place. A
BN outage therefore never drops existing rules; once the BN's statusz is
reachable again, membership re-converges within one poll cycle. During initial
BN bootstrap the sets simply stay empty until statusz responds.

Even when the membership digest is unchanged, the loop forces a full apply every
`statuszForceResyncInterval` (default 1 h) to self-heal any out-of-band edits
to the daemon-owned nft sets.

## statusz endpoints

Both endpoints are REST/JSON, resolved relative to the configured base URL:

| Endpoint | Purpose |
|---|---|
| `GET statusz/inbound` | Sources allowed to connect **to** the BN, by category |
| `GET statusz/outbound` | Destinations the BN connects **out** to (peer-BN backfill) |

### Response shape (`NetworkData`)

```json
{
  "activeEndpoints": [
    {
      "local":  { "address": "0.0.0.0",      "port": "40840" },
      "remote": { "address": "10.10.1.0/24", "port": "*" },
      "category": "publisher",
      "tlsRequired": true
    }
  ]
}
```

- `remote.address` is the source (inbound) or destination (outbound) host or
  CIDR. It is the value written into the nft set.
- `remote.port` is only meaningful for the outbound `partner` category, where
  the backfill set is keyed on `address . port`. Inbound categories ignore the
  port (it is the BN's listener port, not part of the source allowlist).
- Fields the shaper does not consume (`scheme`, `protocol`, `certificate`) are
  omitted from the decode.

## Category to policy mapping

The statusz-category to policy-name mapping is **internal to the monitor** and
not operator-configurable. The policy name is also the nftables set name, in the
`inet weaver-workload-policy` table. These are the same names `block node install` uses when it
creates the policies, so install and the monitor agree on the namespace without
a shared config file.

| statusz endpoint | category | policy / nft set | key shape |
|---|---|---|---|
| inbound | `publisher` | `bn-publisher` | ipv4 host/CIDR |
| inbound | `partner` | `bn-partner-out` | ipv4 host/CIDR |
| inbound | `restricted` | `bn-restricted` | ipv4 host/CIDR |
| outbound | `partner` | `bn-backfill` | compound `ip . port` |

The mapping is keyed on **(direction, category)**, not category alone: the same
`partner` string maps to `bn-partner-out` inbound and to the compound
`bn-backfill` outbound. A peer block node this BN backfills from is reported as
an outbound `partner` connection — there is no distinct `peer_bn` category.

The `public` category is **recognized but deliberately unmapped**: a `public`
source is expressed as the absence of a source-match on its rule (the
`bn-public-out` port-match set), not as a statusz-reconciled set element, so it
is intentionally dropped during bucketing rather than treated as an unknown
category. The operator-curated management sets are likewise never touched by the
monitor.

Each owned category is reconciled on every successful poll: an address that
drops out of statusz is removed from its set, not left stale. A poll that
reports no endpoints for an owned category clears that set.

## Statusz endpoint: discovery and override

The monitor needs to know **where** to poll statusz. By default it **discovers**
the endpoint from the watched BN pod — the pod's IP joined with its
`health`-named containerPort (the BN `/healthz` + statusz port) — and re-resolves
it as the pod restarts or reschedules. An optional `statusz` block on the
block-node component in `daemon.yaml` overrides discovery with a fixed endpoint:

```yaml
components:
  block_node:
    enabled: true
    kubeconfig: /opt/solo/weaver/config/daemon-bn.kubeconfig
    orbit: block-node
    monitors:
      traffic_shaper: true
    statusz:                          # optional
      base_url: http://127.0.0.1:8080 # where the poll loop fetches statusz
      poll_interval: 5m               # optional; defaults to 5m
```

| Field | Required | Meaning |
|---|---|---|
| `base_url` | no | Root URL the `statusz/...` paths resolve against. Must be `http(s)` with a host. When set, **overrides** pod discovery. |
| `poll_interval` | no | Poll cadence as a Go duration (e.g. `5m`, `30s`). Defaults to `5m`. |

When `base_url` is set it takes precedence over discovery — an explicit override
pointing at a directly reachable BN or a port-forward. When it is empty (or the
`statusz` block is absent), the monitor discovers the endpoint from the BN pod;
until a ready BN pod is observed the poll loop idles quietly and starts polling
as soon as one appears.

**Using pod discovery without `base_url`:** after a daemon restart or host
reboot, the entry reconcile fires immediately and skips if no URL has been
discovered yet — the pod watcher runs concurrently, so on a cold start it has
usually not recorded an endpoint yet. Convergence is **not** deferred to the next
tick in that case: the watcher signals the poll loop as soon as it records an
endpoint, and the loop wakes on that signal as well as on the ticker. Both paths
therefore converge as fast as the BN statusz endpoint responds — bounded by BN
startup time, not by `poll_interval`.

The same signal fires when a rescheduled pod changes the endpoint, and when the
owning pod is deleted (the loop wakes, observes an empty URL, and logs the loss
promptly instead of up to one interval later).

> Before #1000 convergence on this path was bounded by *pod discovery plus up to
> one poll interval*, because the ticker was the loop's only wake-up source.
> Setting `base_url` was the documented workaround; it is no longer needed to get
> prompt convergence.

### Enablement at install time

`block node install` writes/merges the `block_node` block above into
`daemon.yaml` (enabled, the scoped `daemon-bn.kubeconfig`, the BN orbit, and
`monitors.traffic_shaper: true`), preserving any operator-set `statusz` block
and the `consensus_node` block. It does **not** set `base_url` — discovery is the
default, and the override is an operator/deploy concern added separately. The
daemon binary and service are installed by `daemon service install`; the install
step only records the enablement so the monitor starts when the daemon runs.

Disable the monitor at any time with `monitors.traffic_shaper: false` and a
daemon restart.

## Related

- `docs/dev/daemon/daemon-architecture.md` — the daemon's overall architecture
  (components, monitors, supervision, `daemon.yaml` schema and versioning).
- The Block Node QoS multi-class priority design (unified nft priority) —
  the authoritative design for the traffic-shaper's nft/tc model.
