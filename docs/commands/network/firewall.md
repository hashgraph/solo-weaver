# `network firewall` — the host firewall

Manages the `inet weaver-host-firewall` table: the host's management allowlist, ICMP policy,
in-cluster host-service ports, and any number of named allow rules.

> **Flags not listed on this page.** Every command here also accepts the
> [global flags](../../reference/global-flags.md) — `--config`, `--output`, `--log-level`,
> `--force`, `--verbose`, `--non-interactive`.

## Two kinds of record

**Three reserved blocks.** Weaver derives or defaults their content, and leaving one out is
dangerous, so they are first-class and cannot be deleted.

| Block | What it holds |
|---|---|
| `mgmt` | Management/SSH allowlist |
| `blocked` | Operator block list — dropped on `prerouting`, `input` and `output` |
| `in_cluster` | Host-service ports reachable from the pod CIDR |

**Named allow rules.** One source list x port list x protocol accept, for anything else the
host must admit — Kubernetes control-plane ports, Cilium VXLAN, an admin jump host.

Structure (which rules exist, what protocol each matches) is declared with
`create-allow-rule`, or stated for the whole table at once with `create --from-file`.
Membership (the addresses and ports inside a rule) is always editable from the CLI.

## Files on disk

| Path | Contents |
|---|---|
| `/etc/solo-provisioner/network-weaver-host-firewall.yaml` | The config currently applied |
| `/etc/solo-provisioner/network-weaver-host-firewall.yaml.prev` | The generation before it |
| `/etc/solo-provisioner/network-weaver-host-firewall.nft` | The rendered ruleset replayed at boot |
| `/etc/solo-provisioner/network-weaver-host-firewall.dns.json` | What each domain name last resolved to |

Every mutation applies to the live kernel in one atomic `nft -f` transaction and rewrites both
the `.nft` and the `.yaml` it was rendered from.

## `create` — lay down the table

Create-if-missing: an existing table is left alone unless you pass `--force`.

```bash
# Create with a management allowlist and the default in-cluster ports
sudo solo-provisioner network firewall create \
  --mgmt-cidrs 10.0.0.0/8 \
  --mgmt-ports 22 \
  --pod-cidr 10.4.0.0/24 \
  --in-cluster-ports 6443,4244,10250

# Re-render an existing table from new flags
sudo solo-provisioner network firewall create --mgmt-cidrs 10.0.0.0/8,192.168.0.0/16 --force
```

| Flag | What it does | Default |
|---|---|---|
| `--mgmt-cidrs` | Management/SSH allowlist CIDRs and/or domain names. Comma-separated or repeated | none |
| `--blocked-cidrs` | Block list CIDRs and/or domain names, dropped before any other rule | none |
| `--in-cluster-ports` | Host-service ports reachable from the pod CIDR | `6443,4244,7472,10250` |
| `--mgmt-ports` | Management TCP port(s). Comma-separated or repeated (`mgmt.ports`) | `22` |
| `--pod-cidr` | Pod CIDR allowed to reach the in-cluster ports | auto-detected |
| `--from-file` | Render the whole table from a YAML config. Mutually exclusive with the flags above | none |
| `--force` | Re-render even if the table exists (global flag, `-y`) | `false` |

> **Omitting `--mgmt-cidrs` leaves the management rule with an empty source set under a
> default-drop policy.** That locks you out of new SSH connections. Always pass it.

**Pod CIDR auto-detection.** With `--pod-cidr` omitted, weaver reads the local node's
`.spec.podCIDR` from the Kubernetes API, matching by hostname (or taking the sole node on a
single-node host). `network firewall create` is node-agnostic and may run before a cluster
exists, so detection is best-effort: with no cluster reachable it logs a warning and **omits
the in-cluster-ports rule**. Pass `--pod-cidr` explicitly to render it anyway.

**ICMP is fixed, and there are no ICMP flags.** The ruleset is:

- Full ICMP from the management allowlist.
- From everyone else, the path-health subset: `destination-unreachable` (Path MTU Discovery)
  and `time-exceeded` (traceroute) always accepted, `echo-request` (ping) rate-limited to
  10/second.

Dropping ICMP errors would silently break PMTUD for legitimate clients, so it is not an
option. The one configurable part is `icmp_echo` on an allow rule, which grants that rule's
sources unmetered `echo-request`.

> There is no `--service-ports`. Block node ports live only in `network policy --ports`. That
> traffic is forwarded rather than delivered locally, so an `input` rule would never match it.

## `create-allow-rule` — declare a named rule

`create-allow-rule` declares the rule; `add` fills it in. Both lists take comma-separated
values, so one `add` finishes the rule in a single atomic apply.

```bash
sudo solo-provisioner network firewall create-allow-rule --name rudder_server --proto tcp --icmp-echo
sudo solo-provisioner network firewall add --name rudder_server \
  --cidr 200.201.203.205/32,10.1.0.0/16 --port 5309,8443,9000-9100

# Deletion needs no separate verb
sudo solo-provisioner network firewall delete --name rudder_server
```

| Flag | What it does | Default |
|---|---|---|
| `--name` | Rule name. May not be `mgmt`, `blocked` or `in_cluster` | required |
| `--proto` | L4 protocol the rule's ports match: `tcp` or `udp` | `tcp` |
| `--icmp-echo` | Grant this rule's sources unmetered ICMP echo-request, above the rate meter | `false` |
| `--force` | Replace an existing rule, **resetting all of it** (global flag, `-y`) | `false` |

Why declaring is a separate verb from `add`:

- **A typo edits nothing.** An unknown `--name` on `add`/`remove`/`set` keeps failing, rather
  than quietly creating a second rule beside the one you meant.
- **Nothing opens early.** A declared rule renders nothing until it has at least one CIDR and
  either a port or `--icmp-echo`, so splitting declare and populate never opens access
  halfway. An incomplete rule is reported as a warning on every apply.
- **Re-declaring is safe.** Without `--force` it warns and changes nothing, mirroring
  `network firewall create`.

> `--force` **replaces** the rule outright. `create-allow-rule --name x --force` on its own
> resets `proto` and `icmp_echo` and empties the address and port lists. To change one field
> of a populated rule, use `set`.

`--proto` and `--icmp-echo` are also settable on `set`, so a rule's protocol can be corrected
without deleting and re-declaring it. The reserved blocks reject both — they render a fixed
shape.

## `create --from-file` — declare the whole table

```yaml
version: 1

mgmt:                                          # required
  cidrs: ["192.168.68.0/24"]                   # required
  ports: ["22"]                                # omitted -> 22

blocked:                                       # required
  cidrs: []                                    # required; [] means block nobody

in_cluster:                                    # required
  cidrs: ["10.4.0.0/14"]                       # omitted -> auto-detected; [] -> no rule
  ports: ["6443", "4244", "7472", "10250"]     # omitted -> the defaults

allow:
  - name: k8s-node
    cidrs: ["10.0.0.0/24"]
    ports: ["6443", "2379-2380", "10250", "10256-10259"]
    proto: tcp

  - name: cilium-vxlan
    cidrs: ["10.0.0.0/24"]
    ports: ["8472"]
    proto: udp

  - name: admin
    cidrs: ["203.0.113.5/32", "2001:db8:5e5::/64"]
    ports: ["22"]
    icmp_echo: true
```

```bash
sudo solo-provisioner network firewall create --from-file rules.yaml --force
```

### Allow-rule fields

| Field | Required | Notes |
|---|---|---|
| `name` | yes | Also the nft set name. `mgmt`, `blocked`, `in_cluster` are reserved |
| `cidrs` | yes | IPv4, IPv6, and domain names in one list; each entry routes to `@<name>` or `@<name>6` by family — see [Domain names in address lists](#domain-names-in-address-lists) |
| `ports` | yes\* | Single ports and inclusive ranges (`2379-2380`). \*Optional when `icmp_echo` is set |
| `proto` | no | `tcp` (default) or `udp`. nft has no combined match, so a service on both is two rules |
| `icmp_echo` | no | Unmetered `echo-request`, rendered above the rate meter |

### Top-level keys

| Key | Required | Omitted means |
|---|---|---|
| `version` | no | The current schema version (`1`) |
| `mgmt` | **yes** | rejected |
| `blocked` | **yes** | rejected |
| `in_cluster` | **yes** | rejected |
| `mgmt.cidrs` | **yes** | rejected — no safe default exists |
| `mgmt.ports` | no | `22` |
| `blocked.cidrs` | **yes** | rejected — write `[]` to block nobody |
| `in_cluster.cidrs` | no | auto-detect this node's pod CIDR (`[]` renders no rule) |
| `in_cluster.ports` | no | `6443,4244,7472,10250` |
| `allow` | no | no named allow rules — **and any that exist are deleted** |

### The file is the whole table

Nothing is inherited from the host's current firewall. Only `add`/`remove`/`set` merge with
what is already there. Two consequences:

- **`allow:` is declarative.** A rule absent from the file is **deleted**.
- **All three reserved blocks are required**, as is `cidrs` inside `mgmt` and `blocked`. An
  omitted block would fall back to a default the file never stated — and for `mgmt` that
  default is an empty allowlist under default-drop, a lockout nobody wrote down. To render no
  rule for a block, state it with an empty list (`in_cluster: {cidrs: []}`). The block itself
  still cannot be removed.

`in_cluster.cidrs` is the one address list weaver can legitimately derive on its own, which is
why it stays optional. Its absence costs a rule, not access to the host.

The same strictness applies to the persisted config: a truncated or hand-edited
`network-weaver-host-firewall.yaml` is refused rather than loaded with a defaulted management
allowlist. To repair one, see [Recovering a corrupt config](#recovering-a-corrupt-config).

## `add` / `remove` / `set` — change a rule's addresses and ports

- `add` and `remove` **merge** with what is there.
- `set` **replaces** the full list atomically.
- `--name` selects a reserved block or an allow rule.

```bash
sudo solo-provisioner network firewall add    --name mgmt     --cidr 10.1.0.0/16
sudo solo-provisioner network firewall add    --name blocked  --cidr 203.0.113.9/32
sudo solo-provisioner network firewall add    --name k8s-node --cidr 10.0.0.5/32 --port 9345
sudo solo-provisioner network firewall remove --name k8s-node --port 9345
sudo solo-provisioner network firewall set    --name mgmt     --cidrs 10.0.0.0/8,192.168.0.0/16
sudo solo-provisioner network firewall set    --name mgmt     --cidrs-file /etc/mgmt-cidrs.txt

# One invocation is one atomic apply, so a rule can be populated in full at once
sudo solo-provisioner network firewall add --name k8s-node \
  --cidr 10.0.0.5/32,10.0.0.6/32 --port 6443,2379-2380,10250

# --proto and --icmp-echo change what a rule matches, not who is in it
sudo solo-provisioner network firewall set --name cilium-vxlan --proto udp
sudo solo-provisioner network firewall set --name admin --icmp-echo
sudo solo-provisioner network firewall set --name admin --icmp-echo=false
```

| Verb | Flag | What it does |
|---|---|---|
| `add`/`remove`/`set` | `--name` | Rule to modify: `mgmt`, `blocked`, `in_cluster`, or an allow rule |
| `add`/`remove` | `--cidr` | CIDR(s) to add or remove. Comma-separated or repeated |
| `add`/`remove` | `--port` | Port(s) to add or remove. Single ports or ranges |
| `set` | `--cidrs` | Full replacement CIDR list. An empty value clears it — except on `mgmt`, [see below](#emptying-the-mgmt-rule-is-guarded) |
| `set` | `--cidrs-file` | Same, from a flat file: one per line or comma-separated, `#` comments allowed |
| `set` | `--ports` | Full replacement port list — except on `mgmt`, [see below](#emptying-the-mgmt-rule-is-guarded) |
| `set` | `--proto` | `tcp` or `udp`. Allow rules only; empty restores `tcp` |
| `set` | `--icmp-echo` | Grant or revoke unmetered ICMP echo-request. Allow rules only |

### Per-block shorthands

The older per-block flags still work — they just name their reserved block implicitly:

```bash
sudo solo-provisioner network firewall add    --mgmt-cidr 10.1.0.0/16      # = --name mgmt --cidr
sudo solo-provisioner network firewall remove --blocked-cidr 203.0.113.9/32
sudo solo-provisioner network firewall add    --in-cluster-port 9100
sudo solo-provisioner network firewall set    --mgmt-cidrs 10.0.0.0/8 --in-cluster-ports 6443,4244
```

### Emptying the `mgmt` rule is guarded

Clearing the management rule's addresses or ports leaves `@mgmt_addrs` matching no source under
the default-drop input chain, so the host drops **every new SSH connection**. Your current
session survives on the established-connection accept and shows no sign of it.

So `set` and `remove` refuse the change unless you pass `--force` (`-y`):

```bash
# Refused — nothing is rendered, dry-run or written
sudo solo-provisioner network firewall set --name mgmt --cidrs ""

# Allowed
sudo solo-provisioner network firewall set --name mgmt --cidrs "" --force
```

Three things this guard is careful about:

- **It guards the transition, not the state.** It fires only when the rule was reachable before
  and is not after. A rule that is *already* unreachable — either half empty — stays fully
  editable, so repairing one is never blocked.
- **Only `mgmt` is guarded.** Clearing `blocked` or `in_cluster` is the supported way to turn
  them off.
- **Only the mutation verbs.** `set` and `remove` go through the check; `create` and
  `create-allow-rule` keep their older warn-only behaviour.

### Three behaviours to know

- **`add`/`remove` touch membership only.** To change an allow rule's `--proto` or
  `--icmp-echo` after declaring it, use `set` — `create-allow-rule --force` would reset the
  rest of the rule. The reserved blocks reject both flags outright, **including `--proto tcp`**:
  they render a fixed shape, so accepting the value that happens to match would report a
  change the renderer ignores.
- **Ports are removed by exact spec.** Removing `2379` from a rule holding `2379-2380` does
  nothing — an nft range is a single set element. Replace the range with `set --ports`.
- **Overlapping CIDRs are accepted.** Adding `10.0.0.5/32` to a rule holding `10.0.0.0/24`
  keeps both entries in the config, so removing the wider prefix later leaves the narrower one
  in force. The kernel folds them into one interval, so `show` prints the folded form and
  `show --output yaml` prints what you authored.

> A ruleset the kernel would refuse is rejected before anything is written. The CLI errors and
> both on-disk files are left exactly as they were, so the ruleset that replays at boot is
> always one that loads.

## `show` / `delete`

```bash
# The live table
sudo solo-provisioner network firewall show

# The config it was rendered from
sudo solo-provisioner network firewall show --output yaml

# One rule
sudo solo-provisioner network firewall show --name k8s-node

# Delete one named allow rule
sudo solo-provisioner network firewall delete --name k8s-node

# Remove the whole table and its on-disk artifacts
sudo solo-provisioner network firewall delete --all
```

### `--output yaml` round-trips

It prints exactly the schema `create --from-file` accepts:

```bash
sudo solo-provisioner network firewall show --output yaml > rules.yaml
sudo solo-provisioner network firewall create --from-file rules.yaml --force   # a no-op
```

### `--output commands` copies one rule to another host

Requires `--name`, and works on named allow rules only — the reserved blocks are configured by
`create`/`set`, not declared.

```bash
sudo solo-provisioner network firewall show --name rudder_server --output commands
```

```
solo-provisioner network firewall create-allow-rule --name rudder_server --proto tcp --icmp-echo
solo-provisioner network firewall add --name rudder_server --cidr 200.201.203.205/32 --port 5309,8443
```

Unlike `show --name <rule> --output yaml`, which is an inspection view rather than a config,
this sequence is **safe to replay against a host that already has a firewall**.
`create-allow-rule` and `add` are additive: they bring the one rule into existence and leave
every other rule alone.

```bash
# on the source host
sudo solo-provisioner network firewall show --name rudder_server --output commands > rudder.sh
# on the target host
sudo sh rudder.sh
```

The emitted lines carry no `sudo` of their own, so it is a script you run once with privilege
rather than a list of individually-escalating commands. Addresses come out in stored (sorted)
order — what the host actually has, not the order you typed.

### `delete --all` removes everything

`--all` is the default when `--name` is omitted. It removes the table and both on-disk files,
leaving the host with no weaver-managed firewall — **including no management allowlist**. It
asks for confirmation in an interactive session; `--force` skips the prompt.

It does not disable `solo-provisioner-network-nft.service`, which is shared with the workload
policy plane. Disable that by hand if you need it off.

Reserved blocks cannot be deleted individually. Clear their addresses instead — `mgmt` needs
`--force`, [see above](#emptying-the-mgmt-rule-is-guarded):

```bash
sudo solo-provisioner network firewall set --name mgmt --cidrs "" --force
```

### `create` and `delete --all` record a decision

Both write the enable decision into the host's runtime state
(`machineState.firewall.disabled`), so `block node reconfigure` agrees with what you did here:

- A firewall you created by hand survives a later reconfigure instead of being torn down.
- One you deleted here is not re-created by it.
- A **live table always wins** over the recorded decision, so removing an active host firewall
  through `block node reconfigure` needs an explicit `--firewall-enabled=false`.
- The membership verbs (`add`, `remove`, `set`) record no decision. `reconfigure` reads their
  result straight out of the persisted YAML, so an urgent `add --name mgmt --cidr …` is not
  reverted by the next reconfigure.

## Domain names in address lists

`--mgmt-cidrs`, `--blocked-cidrs`, and `--cidr`/`--cidrs` on any of those or on a declared
allow rule, take fully-qualified domain names as well as addresses, mixed freely:

```bash
sudo solo-provisioner network firewall create \
  --mgmt-cidrs 10.0.0.0/8,jump.corp.example.com \
  --mgmt-ports 22 \
  --blocked-cidrs 203.0.113.0/24,bad.corp.example.com

sudo solo-provisioner network firewall create-allow-rule --name monitoring
sudo solo-provisioner network firewall add --name monitoring \
  --cidr probe.corp.example.com --port 9100
```

Each name is resolved to its A records and expanded to one `/32` per address before the
ruleset is rendered, so the rule's address set only ever holds literals. The config keeps the
name:

```bash
sudo solo-provisioner network firewall show --output yaml   # jump.corp.example.com, bad.corp.example.com
sudo nft list set inet weaver-host-firewall mgmt_addrs      # 192.0.2.7
sudo nft list set inet weaver-host-firewall blocked_addrs   # 203.0.113.0/24, 198.51.100.4
sudo nft list set inet weaver-host-firewall monitoring      # 198.51.100.9
```

**Every rule but `in_cluster` takes names.** `--pod-cidr` and `--in-cluster-*` stay
address-only — that list is auto-detected from the node's `.spec.podCIDR` rather than typed,
so there is no name to accept. Every `network policy` flag stays address-only too: that plane
reads its membership back out of the kernel, so a name written there would be overwritten by
resolved addresses on the next apply.

The block list takes names on the same terms as the rest, but **fails differently** — see
[The block list fails the other way](#the-block-list-fails-the-other-way).

### What counts as a name

| Input | Read as |
|---|---|
| `10.0.0.0/8` | Address — anything containing `/` |
| `10.0.0.1` | Address, and **rejected**: supply the mask, `10.0.0.1/32` |
| `jump.corp.example.com` | Name |
| `jump.corp.example.com.` | Name — the trailing root dot is accepted and dropped |
| `localhost` | **Rejected** — see below |

Names are case-insensitive, so `Jump.Example.COM.` and `jump.example.com` are one entry, and
`remove` matches across spellings.

A bare hostname with no dot is rejected on purpose. It would resolve through the host's search
domains, so the same config would admit different sources on different machines. IPv6 / AAAA
records are not supported yet.

### Keeping the addresses current

Installing a name also installs `solo-provisioner-network-dns-refresh.timer`, which runs
`refresh-dns` a minute after boot and every five minutes after that. Removing the last name
removes the timer, and so does `delete --all`.

```bash
systemctl list-timers solo-provisioner-network-dns-refresh.timer
sudo solo-provisioner network firewall refresh-dns   # or force it now
```

`refresh-dns` re-resolves every name and rewrites the ruleset **only when an address actually
changed**, so a routine run costs one DNS query and no reload. That is the difference from
`reapply`, which always reloads because its job is to re-assert a table something else
disturbed — at which point the files on disk are already correct and only the kernel diverged.

The interval is fixed rather than driven by the record's TTL. Five minutes sits inside the
30–300s band these records normally use.

### Where resolution happens

Names are resolved **by this node**, through its own `/etc/resolv.conf`, by the CLI running
as root in the host network namespace. No extra configuration: if `getent hosts <name>` works
on the node, so does the allowlist entry.

```bash
getent hosts jump.corp.example.com    # if this resolves, the firewall entry will too
```

Two consequences worth knowing:

- **This is not cluster DNS.** The lookup does not go through CoreDNS, so a
  `*.svc.cluster.local` name will not resolve here. That is the right behaviour for a
  management allowlist — the hosts in it are outside the cluster by definition.
- **An `/etc/hosts` entry is honoured**, and pinning a name there is the recommended
  mitigation for a high-value node, since it takes the answer out of a remote party's hands.

All names are looked up at once rather than one after another, and the whole pass shares a
two-second budget. A slow resolver therefore costs one round trip regardless of how many
names are in the list.

> **`arm64` caveat.** The `linux/amd64` build is compiled natively and can use the system
> resolver (libc/NSS); the `linux/arm64` build is cross-compiled with cgo disabled and so
> uses Go's own resolver, which reads `/etc/hosts` and `/etc/resolv.conf` directly. Ordinary
> DNS and `systemd-resolved` behave identically on both — Debian and Ubuntu point
> `resolv.conf` at the `127.0.0.53` stub, which speaks DNS. The difference only shows up for
> a name resolvable *solely* through an NSS module, such as LDAP-backed hosts or mDNS
> `.local`: those work on `amd64` and may not on `arm64`. Use an address or an `/etc/hosts`
> entry for such a name.

### When resolution fails

Per name, never all-or-nothing — one unreachable host does not freeze the others:

| Situation | Behaviour |
|---|---|
| Name resolves | Addresses updated, cached in `…dns.json` |
| Name stops resolving, was cached | **Last-known addresses kept**, warning logged |
| Name has never resolved, on `create`/`create-allow-rule`/`add`/`set` | **Command fails.** Almost always a typo — this applies to every rule |
| Name has never resolved, on `refresh-dns` | Contributes nothing, warning logged — **except in `blocked`**, which fails instead |
| `mgmt` resolves to no addresses at all | **Refused.** An empty `@mgmt_addrs` under the default-drop input chain drops every new SSH connection, and there is no `--force` past it |
| An allow rule resolves to no addresses at all | Renders no accept rule, warning logged — unlike `mgmt`, this costs one rule's traffic, not administrative access to the host |
| Any `blocked` name resolves to nothing, on any verb | **Refused**, even if the rest of the block list is fine — see below |

The cache is a fallback, never the source of truth. Deleting it costs the last-known addresses
and nothing else.

> **DNS is part of your trust boundary now.** Whoever controls the answer for a name in `mgmt`
> controls who can reach SSH on this host; a name in an allow rule controls who can reach
> whatever that rule admits; and whoever can make a name in `blocked` *stop* resolving decides
> when this host stops dropping their traffic. On a high-value node, prefer an address, or
> pin the name in `/etc/hosts` so the answer cannot be changed remotely. Note also that
> resolution happens **on this host** — under split-horizon DNS it may see a different answer
> than your workstation does.
>
> A reboot replays the last rendered `.nft`, so the host comes up admitting the addresses from
> the last successful resolution until the timer first fires. That is deliberate: a boot that
> depends on DNS is a boot that can lock you out.

### The block list fails the other way

Losing a name from `mgmt` or an allow rule takes access away: something stops working, and
you find out. Losing a name from `blocked` hands access back — the host it named is reachable
again, the ruleset looks healthy, and nothing you would think to check has changed.

So the block list carries a stricter rule than everything else in this table, aimed squarely
at the case where an entry would quietly contribute nothing:

- **A `blocked` name must resolve once, at the moment you add it.** That is the same
  create/add/set gate every other name faces.
- **After that, its addresses are cached and kept indefinitely.** A resolver outage changes
  nothing — the entry keeps denying what it last denied. That is what stops a blip breaking
  the rule; it is also what leaves a withdrawn record pinned, see below.
- **A `blocked` name with no answer and no cached one fails every verb**, including
  `refresh-dns` and `reapply`, and even when it is one entry out of fifty. Nothing is written,
  so the live ruleset keeps enforcing the addresses it already has.

In practice you reach the third case only by removing `…dns.json` while a name is
unresolvable, or by hand-editing a never-resolved name into the config. When you do,
`refresh-dns` fails and its unit shows up in `systemctl --failed`:

```
$ sudo solo-provisioner network firewall refresh-dns
Error: [bad.corp.example.com] in "blocked" resolved to no address and none is on record, so
rendering would drop them from @blocked_addrs and silently stop blocking the hosts they name.
Nothing was changed
```

Fix the resolution, or drop the entry:

```bash
sudo solo-provisioner network firewall remove --name blocked --cidr bad.corp.example.com
```

The timer keeps firing either way, so the failure clears itself on the next tick once the name
resolves again. There is no `--force`: forcing here would mean rendering a block list that
blocks less than you wrote, which is the failure this is guarding against.

One consequence worth knowing: because resolution covers the whole table, an unresolvable
`blocked` name also blocks *unrelated* edits — an `add --name mgmt` will refuse until you fix
or remove it. The error names the offending entry.

### What a name here does and does not cover

An nft set holds addresses, so a name in this list means *the addresses it resolved to at the
last successful refresh* — never the name itself. Two consequences, and they are different
from each other:

- **Ordinary rotation is followed.** While the name still resolves, every refresh replaces
  that entry's addresses. A host that moves is blocked at its new address within five minutes,
  or one minute after a boot, and stops being blocked at the old one in the same write.
- **A withdrawn record is not.** If the name stops resolving at all — NXDOMAIN, or the
  resolver is unreachable — the cached addresses are held until it resolves again or you
  remove the entry, with a warning on each refresh. A host that both moves *and* drops its
  record is reachable again.

The refusal described above does not change the second case, and no failure policy could:
refusing writes nothing, so the kernel keeps exactly the same cached addresses it would have
kept anyway. What the refusal buys is that an entry can never silently *vanish* — not that it
stays accurate.

So treat a name here as a convenience for hosts you know by name and expect to be stable. It
is not a control against someone who runs their own DNS and would rather not be blocked; for
that, block by address, or block at the DNS layer where the name is what gets matched.

## `reapply` — re-assert the persisted config

Re-renders and re-applies what is on disk without changing it. It takes no arguments — it
states no intent, so there is nothing to supply.

```bash
sudo solo-provisioner network firewall reapply
```

Use it after something else on the host disturbed the table, or after recovering the config.

- It **records no enable/disable decision**, unlike `create` and `delete --all`. A later
  `block node reconfigure` behaves exactly as if the reapply had not run.
- It replaces only the `inet weaver-host-firewall` table. The rendered ruleset scopes its
  flush to that table, so third-party nftables tables are left alone.
- With no config persisted it **fails** rather than applying a default table — default-drop
  with an empty allowlist would lock the host out. Run `create` first.

There is deliberately no way to point `reapply` at a file. To apply a file, that is
`create --from-file <path> --force`.

To pick up a changed DNS answer, that is `refresh-dns` — `reapply` re-resolves too, but it
reloads the kernel whether anything changed or not.

### Recovering a corrupt config

Every apply retains the generation it replaces, so repair is two steps and **keeps the named
allow rules**:

```bash
sudo cp /etc/solo-provisioner/network-weaver-host-firewall.yaml.prev \
        /etc/solo-provisioner/network-weaver-host-firewall.yaml
sudo solo-provisioner network firewall reapply
```

What that retained copy is and is not:

- **One generation deep.** It is a recovery artifact, not version history. Keep history in
  your own repository, holding the output of `show --output yaml`.
- **Always loadable.** It is only written when the config it replaces parses, so it is never
  itself corrupt. Recovering from it does not consume it — the bad config is not promoted over
  the good one.
- **Not the last line of defence.** Without it, a lost config falls through to re-parsing the
  rendered `.nft`, which recovers the three reserved blocks but **loses every named allow
  rule**. That fallback still exists; the retained copy is what keeps you from needing it.

`delete --all` removes the retained copy along with the config and the `.nft`.

---

---

## See also

- [Network commands](README.md) — the other two planes
- [`network policy`](policy.md) — workload traffic classification
- [Block node commands](../block-node.md#host-firewall---firewall-enabled) — the `--firewall-enabled` switch that creates this table
- [Troubleshooting](../../troubleshooting.md)
