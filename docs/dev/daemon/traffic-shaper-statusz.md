# Block Node traffic-shaper: statusz poll loop & configuration

This document describes how the `solo-provisioner-daemon` block-node
traffic-shaper monitor consumes the Block Node's `statusz` REST endpoints, how
the statusz categories map to network policies, the local-fallback statusz
configuration schema in `daemon.yaml`, and the mock statusz server used for
daemon development and tests.

> **The statusz contract is provisional.** The request/response shape below
> mirrors the Block Node's `network-data.proto`. Confirming the contract with
> the Block Node team is a blocking dependency; treat this document as the
> current best understanding, not a frozen interface.

> **Implementation status.** This story delivers the pieces the loop *consumes* —
> the `daemon.yaml` local-fallback config schema, the install-time monitor
> enablement, and the mock statusz server. The poll loop *itself* (fetch → diff →
> apply) lands in a follow-up TS_3 story: it applies nft membership through the
> daemon's `privexec` sudo-delegation (the daemon is unprivileged and never calls
> `nft` directly), not the in-process path this doc's prose describes.

## What the poll loop does

The traffic-shaper monitor runs a poll loop that, on a fixed cadence
(default 5s):

1. fetches the BN's `inbound-clients` and `outbound-clients` statusz endpoints,
2. buckets the returned endpoints by category,
3. diffs the desired membership against the live nftables sets, and
4. applies the per-policy membership deltas.

The loop reconciles **nftables set membership only** — the dynamic plane. The
tc HTB class hierarchy is static (installed once); traffic lands in the correct
class via nftables `skb->priority` marking, not via runtime tc changes.

### Outage behaviour

A failed poll (statusz unreachable, diff error, or apply error) is logged and
retried on the next tick, leaving the **last-good** nftables state in place. A
BN outage therefore never drops existing rules; once the BN's statusz is
reachable again, membership re-converges within one poll cycle. During initial
BN bootstrap the sets simply stay empty until statusz responds.

## statusz endpoints

Both endpoints are REST/JSON, resolved relative to the configured base URL:

| Endpoint | Purpose |
|---|---|
| `GET statusz/inbound-clients` | Sources allowed to connect **to** the BN, by category |
| `GET statusz/outbound-clients` | Destinations the BN connects **out** to (peer-BN backfill) |

### Response shape (`NetworkData`)

```json
{
  "active_endpoints": [
    {
      "local":  { "address": "0.0.0.0",      "port": "40840" },
      "remote": { "address": "10.10.1.0/24", "port": "*" },
      "category": "publisher",
      "tls_required": true
    }
  ]
}
```

- `remote.address` is the source (inbound) or destination (outbound) host or
  CIDR. It is the value written into the nft set.
- `remote.port` is only meaningful for the outbound `peer_bn` category, where
  the backfill set is keyed on `address . port`. Inbound categories ignore the
  port (it is the BN's listener port, not part of the source allowlist).
- Fields the shaper does not consume (`scheme`, `protocol`, `certificate`) are
  omitted from the decode.

## Category to policy mapping

The statusz-category to policy-name mapping is **internal to the monitor** and
not operator-configurable. The policy name is also the nftables set name, in the
`inet weaver` table. These are the same names `block node install` uses when it
creates the policies, so install and the monitor agree on the namespace without
a shared config file.

| statusz endpoint | category | policy / nft set | key shape |
|---|---|---|---|
| inbound-clients | `publisher` | `bn-publisher` | ipv4 host/CIDR |
| inbound-clients | `partner` | `bn-partner` | ipv4 host/CIDR |
| inbound-clients | `restricted` | `bn-restricted` | ipv4 host/CIDR |
| outbound-clients | `peer_bn` | `bn-backfill` | compound `ip . port` |

Categories not in this table (for example `public`, or the operator-curated
management set) are **never touched** by the monitor. A `public` source is
expressed as the absence of a source-match on its rule, not as a set element, so
it is intentionally dropped during bucketing.

Each owned category is reconciled on every successful poll: an address that
drops out of statusz is removed from its set, not left stale. A poll that
reports no endpoints for an owned category clears that set.

## Local-fallback statusz configuration

The monitor needs to know **where** to poll statusz. This is configured with an
optional `statusz` block on the block-node component in `daemon.yaml`:

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
      poll_interval: 5s               # optional; defaults to 5s
```

| Field | Required | Meaning |
|---|---|---|
| `base_url` | no | Root URL the `statusz/...` paths resolve against. Must be `http(s)` with a host. |
| `poll_interval` | no | Poll cadence as a Go duration (e.g. `5s`, `10s`). Defaults to `5s`. |

When `base_url` is empty (or the `statusz` block is absent), the poll loop
**idles** — there is no source to poll. In-cluster discovery of statusz on the
BN pod is a later story; until then, `base_url` is how the monitor is pointed at
a statusz source, whether that is the mock server, a port-forward, or a
directly reachable BN.

### Enablement at install time

`block node install` writes/merges the `block_node` block above into
`daemon.yaml` (enabled, the scoped `daemon-bn.kubeconfig`, the BN orbit, and
`monitors.traffic_shaper: true`), preserving any operator-set `statusz` block
and the `consensus_node` block. It does **not** set `base_url` — the
local-fallback source is an operator/deploy concern, added separately. The
daemon binary and service are installed by `daemon service install`; the install
step only records the enablement so the monitor starts when the daemon runs.

Disable the monitor at any time with `monitors.traffic_shaper: false` and a
daemon restart.

## Mock statusz server (dev & tests)

`internal/daemon/blocknode/statuszmock` is a minimal mock of the two statusz
endpoints, served from a JSON roster file that is **re-read on every request**,
so editing the file changes what the daemon's poll loop sees on its next tick.
It is used by daemon tests and by the UTM-VM traffic-shaper demo.

Run it:

```bash
go run ./internal/daemon/blocknode/statuszmock/cmd --addr :8080 --roster roster.json
```

Roster file shape:

```json
{
  "inbound": [
    { "remote": {"address": "10.10.1.0/24", "port": "*"}, "category": "publisher" },
    { "remote": {"address": "10.20.1.0/24", "port": "*"}, "category": "partner" }
  ],
  "outbound": [
    { "remote": {"address": "10.30.5.7", "port": "43473"}, "category": "peer_bn" }
  ]
}
```

A missing roster file is served as an empty roster (the BN "no clients yet"
bootstrap state) rather than an error, so the server can be started before the
roster exists. Point `daemon.yaml`'s `block_node.statusz.base_url` at this
server's address to drive the poll loop from it.

See `docs/dev/traffic-shaper-demo.md` for the end-to-end UTM-VM walkthrough.

## Related

- `docs/dev/daemon/daemon-architecture.md` — the daemon's overall architecture
  (components, monitors, supervision, `daemon.yaml` schema and versioning).
- The Block Node QoS multi-class priority design (unified nft priority) —
  the authoritative design for the traffic-shaper's nft/tc model.
