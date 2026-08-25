# `network policy` — workload traffic classification

Manages the `inet weaver-workload-policy` plane: named per-category rules that either tag
traffic with an HTB priority class, or drop it.

The scope is generic and category-agnostic — the CLI takes CIDRs and class names directly and
knows nothing about statusz. The examples below use the block-node policies because
`block node install` is its only caller today.

> **Flags not listed on this page.** Every command here also accepts the
> [global flags](../../reference/global-flags.md) — `--config`, `--output`, `--log-level`,
> `--force`, `--verbose`, `--non-interactive`.

## What `create` does

Each `create`:

1. Renders the rule(s) into the `inet weaver-workload-policy` forward chain.
2. Ensures the policy's nft set `@<name>` exists.
3. Writes a per-policy registry file under `/etc/solo-provisioner/policies/`.
4. Applies the full chain to the live kernel with `nft -f`.
5. Atomically rewrites `/etc/solo-provisioner/network-weaver-workload-policy.nft`.

Create-if-missing, mirroring `network firewall create`: an existing policy warns and changes
nothing, even if the flags differ from last time. With `--force` its config and membership are
**replaced** — not merged — from the flags given.

## Pick exactly one action

| Action | Flag | Effect |
|---|---|---|
| Classify | `--stamp <class>` | Tag matching packets with an HTB priority class |
| Drop | `--deny` | Drop matching packets |

There is no `--direction` flag: every class has exactly one direction, so `--stamp` determines
it.

### How `--deny` matching works

- A `--deny` matches its `@<name>` CIDR set, **unless** `--from-entity world` replaces that
  with a match on any source.
- `--ports` adds a listener-port clause on top of either. It does not remove the set match, so
  `--deny --ports` without `--from-entity world` still needs membership to match anything.
- A **membership** `--deny` drops both directions.
- A **port-scoped** `--deny` is confined to the pod CIDR and drops the **request leg only**,
  qualified with `ct direction original`. A listener port sits inside the ephemeral range, so
  an unqualified drop would also catch the reply leg of an unrelated connection that happened
  to draw that port as its source port.
- Combining `--deny --ports --from-entity world` locks the port down from every source.

## Examples

```bash
# Publisher: highest-priority ingress class on the publisher listener port
sudo solo-provisioner network policy create --name bn-publisher \
  --ports 40840 --stamp publisher

# Subscriber ingress from any source (no IP-set clause): reserve class
sudo solo-provisioner network policy create --name bn-subscriber-in \
  --ports 40980,40981 --stamp reserve-ingress --from-entity world

# Partner egress to a curated destination list
sudo solo-provisioner network policy create --name bn-partner-out \
  --ports 40980,40981 --stamp partner --cidrs 10.20.0.0/16

# Backfill egress with an asymmetric reply class
sudo solo-provisioner network policy create --name bn-backfill \
  --stamp reserve-egress --reply-stamp backfill-response \
  --cidrs 10.30.5.7:43473

# Quarantine: drop all traffic to and from a set of CIDRs
sudo solo-provisioner network policy create --name bn-restricted \
  --deny --cidrs 10.99.0.0/16

# Port lockdown: drop inbound connections to a listener port, from every source
sudo solo-provisioner network policy create --name bn-health \
  --deny --ports 40983 --from-entity world
```

| Flag | What it does | Default |
|---|---|---|
| `--name` | Policy name, and the nft set name `@<name>`. **Required** | — |
| `--stamp` | HTB class to classify into. Also fixes the direction. Mutually exclusive with `--deny` | none |
| `--deny` | Drop the `--cidrs` (both directions), the `--ports` (request leg), or their intersection | `false` |
| `--reply-stamp` | Reply class for an asymmetric conntrack reply. Needs `--stamp` to be an egress class, and must itself be the mirror ingress class | none |
| `--from-entity` | `world` — match any source/dest with no IP-set clause. Mutually exclusive with `--cidrs` | none |
| `--ports` | Listener ports for the match key. Comma-separated or repeated | none |
| `--cidrs` | Initial set membership. `ip:port` entries for `--reply-stamp` policies | none |
| `--cidrs-file` | Same, from a file: one per line or comma-separated | none |
| `--pod-cidr` | Pod CIDR to scope classification to | auto-detected |
| `--force` | Replace an existing policy's config and membership (global flag, `-y`) | `false` |

**Rule position in the chain** is determined by action type and match specificity — deny →
reply-restore → specific stamp → fallthrough stamp — never by creation order.

**Pod CIDR auto-detection** applies only to policies that reference `POD_CIDR`: every
`--stamp` policy, and a `--deny` that carries `--ports`. A membership-only `--deny` drops on
set membership alone, so detection is skipped for it. Unlike `network firewall create`, a
`--stamp` policy that cannot detect the pod CIDR is a **hard error**, not a warning. If a
`--deny` create's merged chain still includes a `--stamp` sibling that needs `POD_CIDR`, the
value is recovered from the existing `.nft` rather than being required again — it is a
deployment-wide constant, not a per-call argument.

> **Membership is never persisted to `network-weaver-workload-policy.nft`.** Statusz is the
> source of truth and the daemon reconciles it. `--cidrs` seeds the live set only, and only on
> a brand-new policy or a `--force` re-create (which replaces membership with exactly what you
> pass — not a merge).

## What an empty statusz response does

Worth knowing before you read too much into an empty set: **this plane fails open, not
closed.**

The forward chain's policy is `accept`. A packet that no rule matches carries no
`meta priority` and lands in the HTB default class — it is not dropped. Only the deny tier
drops, and a deny rule whose set is empty matches nothing.

So when statusz returns `200` with an empty list, the daemon clears the owned sets and:

| Policy | Set cleared means |
|---|---|
| `bn-publisher`, `bn-partner-out` (`--stamp`) | The rule stops matching. Traffic falls through to the default class |
| `bn-subscriber-in`, `bn-public-out` (managed `_ports`) | Same — the port clause matches nothing |
| `bn-restricted` (`--deny`) | **Nothing is dropped.** The quarantine list is empty |

Traffic keeps flowing. What you lose is prioritization: everything competes in one class
instead of publisher traffic holding its reserved share.

Three things do **not** change, which is why this can never lock a node out:

- **`bn-health` is not reconciled.** Its match key is a static port list, not membership, so
  the health/statusz port stays dropped from off-node either way.
- **The host firewall is a different table.** `inet weaver-host-firewall` keeps enforcing the
  management allowlist and block list regardless of what statusz says.
- **The tc hierarchy stays installed.** The classes keep their rates; they just stop receiving
  marked packets.

> **This is easy to miss in testing.** The default class ceils at 100% of the trunk, so while
> the other classes sit idle it borrows the whole link. A throughput test during an
> empty-statusz window measures full line rate and looks healthy. The loss only shows up once
> the other classes have traffic again and the default class is squeezed back to its
> guarantee. Confirm classification with
> [`network shape watch`](shape.md#watch--live-counters-read-only) rather than with a
> throughput number.

## `add` / `remove` / `set` — change live membership

**None of these re-render the `.nft`.** Only the live kernel set changes.

```bash
# Add (repeatable or comma-separated)
sudo solo-provisioner network policy add --name bn-publisher --cidr 10.1.0.1/32
sudo solo-provisioner network policy add --name bn-publisher --cidr 10.1.0.2/32,10.1.0.3/32

# Remove
sudo solo-provisioner network policy remove --name bn-publisher --cidr 10.1.0.1/32

# Replace the whole list atomically (flush + re-add in one kernel transaction)
sudo solo-provisioner network policy set --name bn-publisher --cidrs 10.2.0.0/16

# Clear the set (omit --cidrs)
sudo solo-provisioner network policy set --name bn-publisher
```

| Verb | Flag | Required |
|---|---|---|
| `add`/`remove` | `--name` | yes |
| `add`/`remove` | `--cidr` — CIDR to add or remove, repeatable or comma-separated | yes |
| `set` | `--name` | yes |
| `set` | `--cidrs` — replacement membership; omit to clear | no |
| `set` | `--cidrs-file` — same, from a file | no |

For `--reply-stamp` policies the entries must be `ip:port` pairs on all three verbs, same as
`create --cidrs`.

## `show` — inspect policies

Without `--name`, lists every configured policy sorted by name. With `--name`, prints one.

```bash
sudo solo-provisioner network policy show
sudo solo-provisioner network policy show --name bn-publisher
```

```
policy: bn-publisher
  direction: ingress
  action:  stamp
  class:   publisher
  ports:   40840
  created: 2026-01-01T00:00:00Z
  live set @bn-publisher:
    10.1.0.1/32
    10.1.0.2/32
```

With nothing configured, a bare `show` prints `no policies configured` rather than failing.

## `delete` — remove a policy

```bash
sudo solo-provisioner network policy delete --name bn-restricted
```

It re-renders the full chain without the removed policy, snapshots and restores the remaining
policies' live membership (so the destructive `delete table; add table` does not wipe their
sets), removes the registry file, and atomically overwrites the `.nft`.

If this was the last policy, the table is torn down entirely, live and on disk. The boot
oneshot stays enabled.

---

---

## See also

- [Network commands](README.md) — the other two planes
- [`network shape`](shape.md) — what the classes a policy stamps actually get
- [`block node reconcile-shaper`](../block-node.md#reconcile-shaper--sync-policy-sets-from-statusz) — how set membership is filled in at runtime
- [Traffic shaper internals](../../dev/traffic-shaper.md)
