# Command Reference

Every `solo-provisioner` command, by stack.

| Stack | Guide | What it does |
|---|---|---|
| `block node` | [block-node.md](block-node.md) | Install, upgrade, reset and remove a Hedera Block Node |
| `network` | [network/](network/) | Three scopes, one guide each: [firewall](network/firewall.md), [policy](network/policy.md), [shape](network/shape.md) |
| `alloy` / `eso` | [alloy.md](alloy.md) | Grafana Alloy metrics and logs; External Secrets Operator |
| `kube cluster` | [below](#kubernetes-cluster) | The Kubernetes stack underneath everything |
| `teleport` | [below](#teleport) | Secure SSH and kubectl access |
| `daemon service` | [below](#daemon-service) | The `solo-provisioner-daemon` systemd service |
| `consensus migration soak` | [below](#consensus-migration-soak) | Consensus-node migration soak watcher |
| `version` | [below](#utilities) | Version information |

Also useful:

- [Global flags](../reference/global-flags.md) — flags that work everywhere
- [Configuration](../reference/configuration.md) — config file, env vars, proxy
- [Deployment profiles](../reference/deployment-profiles.md) — what `--profile` selects
- [Workflows](../workflows.md) — end-to-end recipes
- [Troubleshooting](../troubleshooting.md)

---

## Quick reference card

```bash
# INSTALLATION
# Download from: https://github.com/hashgraph/solo-weaver/releases
sudo ./solo-provisioner install

# BLOCK NODE
sudo solo-provisioner block node check       --profile=<profile>
sudo solo-provisioner block node install     --profile=<profile> [--values=<file>] [--plugin-preset=<preset>]
sudo solo-provisioner block node upgrade     --profile=<profile> [--values=<file>] [--with-reset]
sudo solo-provisioner block node reconfigure --profile=<profile> [--values=<file>] [--no-restart]
sudo solo-provisioner block node reset       --profile=<profile>
sudo solo-provisioner block node uninstall   --profile=<profile> [--with-reset|--purge-storage]
sudo solo-provisioner block node reconcile-shaper --statusz-url=<url> [--check]
sudo solo-provisioner block node tc-attach   --veth=<iface> [--detach]

# KUBERNETES
sudo solo-provisioner kube cluster install
sudo solo-provisioner kube cluster uninstall

# NETWORK — HOST FIREWALL
sudo solo-provisioner network firewall create [--mgmt-cidrs=<list>] [--from-file=<yaml>]
sudo solo-provisioner network firewall create-allow-rule --name=<name> [--proto=tcp|udp] [--icmp-echo]
sudo solo-provisioner network firewall add|remove --name=<name> [--cidr=<list>] [--port=<list>]
sudo solo-provisioner network firewall set    --name=<name> [--cidrs=<list>] [--ports=<list>]
sudo solo-provisioner network firewall show   [--name=<name>] [--output=yaml|commands]
sudo solo-provisioner network firewall delete --name=<name> | --all
sudo solo-provisioner network firewall reapply

# NETWORK — WORKLOAD POLICY
sudo solo-provisioner network policy create --name=<name> --stamp=<class>|--deny [--ports=<list>] [--cidrs=<list>]
sudo solo-provisioner network policy add|remove --name=<name> --cidr=<list>
sudo solo-provisioner network policy set    --name=<name> [--cidrs=<list>]
sudo solo-provisioner network policy show   [--name=<name>]
sudo solo-provisioner network policy delete --name=<name>

# NETWORK — BANDWIDTH SHAPING
sudo solo-provisioner network shape create --device=egress|ingress --rate=<rate>|auto --default=<class>
sudo solo-provisioner network shape create --class=<name> --rate=<rate> [--ceil=<rate>] [--prio=<0-7>]
sudo solo-provisioner network shape set    --class=<name> [--rate=<rate>] [--ceil=<rate>]
sudo solo-provisioner network shape show   [--class=<name>]
sudo solo-provisioner network shape watch  --device=<dir> --iface=<iface> [--class=<name>] [--interval=<d>] [--count=<n>]
sudo solo-provisioner network shape delete --class=<name>

# TELEPORT
sudo solo-provisioner teleport node install    --token=<token> --proxy=<addr>
sudo solo-provisioner teleport node uninstall
sudo solo-provisioner teleport cluster install --values=<file>
sudo solo-provisioner teleport cluster uninstall

# EXTERNAL SECRETS OPERATOR (ESO)
sudo solo-provisioner eso operator install    [--namespace=<ns>]
sudo solo-provisioner eso operator uninstall  [--namespace=<ns>]
sudo solo-provisioner eso secret create       --store=<name> --name=<secret> --namespace=<ns> --set KEY=store/path[#field]

# ALLOY
sudo solo-provisioner alloy cluster install   [--cluster-name=<name>] [--monitor-block-node]
sudo solo-provisioner alloy cluster uninstall

# DAEMON
sudo solo-provisioner daemon service install [--components=<list>] [--cn-node-id=<id>] [--cn-orbit=<ns>]
sudo solo-provisioner daemon service install --from-config=<path>
sudo solo-provisioner daemon service uninstall
sudo solo-provisioner daemon service check          # alias: status
sudo solo-provisioner daemon service start|stop

# CONSENSUS MIGRATION SOAK
sudo solo-provisioner consensus migration soak start  --node-id=<id> --cutover-ts=<RFC-3339> --migration-plan=<path>
sudo solo-provisioner consensus migration soak stop   [--keep-state]
sudo solo-provisioner consensus migration soak status

# UTILITIES
solo-provisioner version [--output=text|json]
solo-provisioner --help
```

---

## Kubernetes cluster

`kube cluster install` sets up a complete single-node Kubernetes environment.

### What gets installed

| Component | Role |
|---|---|
| kubeadm / kubelet | Cluster initialization and node agent |
| CRI-O | Container runtime |
| Cilium | Container networking (CNI) |
| MetalLB | Load balancer for bare-metal Kubernetes |
| Helm | Package manager |
| kubectl | Kubernetes CLI |
| k9s | Terminal Kubernetes UI |
| Metrics Server | Resource metrics for pods and nodes |

```bash
sudo solo-provisioner kube cluster install

# Undo completed steps if a later one fails
sudo solo-provisioner kube cluster install --rollback-on-error
```

| Flag | What it does |
|---|---|
| `--stop-on-error` | Stop at the first failing step (default) |
| `--rollback-on-error` | Undo completed steps if a later one fails |
| `--continue-on-error` | Keep going past failures |

### Two things it deliberately does not do

- **No node-specific firewall rules.** The `inet weaver-host-firewall` table is applied by the
  block-node workflow instead — see
  [Networking switches](block-node.md#networking-two-independent-switches).
- **No workload sizing.** Cluster install validates only what Kubernetes itself needs to run,
  so it takes **no `--profile` and no `--node-type`**. Workload-sized hardware validation
  happens later, at `block node check` / `block node install`.

> `--profile` and `--node-type` are still accepted as hidden flags so old scripts do not
> break, but the values are **ignored** and a notice is printed. Remove them.

### Uninstall

Tears down the whole Kubernetes stack — kubeadm, CRI-O, Cilium, everything — while keeping the
downloads cache.

```bash
sudo solo-provisioner kube cluster uninstall
sudo solo-provisioner kube cluster uninstall --continue-on-error
```

> **Every running workload stops.** This includes the block node.

---

## Teleport

Two independent agents:

| Agent | Gives you | Command |
|---|---|---|
| Node agent | SSH access to the host | `teleport node install` |
| Cluster agent | kubectl access to the cluster | `teleport cluster install` |

### Node agent (SSH)

```bash
sudo solo-provisioner teleport node install \
  --token=<join-token> \
  --proxy=proxy.teleport.example.com:443
```

| Flag | Required | What it does |
|---|---|---|
| `--token` | yes | Join token for the agent |
| `--proxy` | yes | Teleport proxy address as `host:port` |

Remove it, stopping the systemd service and cleaning up binaries and config:

```bash
sudo solo-provisioner teleport node uninstall
```

### Cluster agent (kubectl)

```bash
sudo solo-provisioner teleport cluster install --values=/path/to/teleport-values.yaml

# Pin the chart version
sudo solo-provisioner teleport cluster install \
  --values=/path/to/teleport-values.yaml \
  --version=16.0.0
```

| Flag | Required | What it does |
|---|---|---|
| `--values` | yes | Teleport Helm values file |
| `--version` | no | Teleport Helm chart version |

Remove the Helm release:

```bash
sudo solo-provisioner teleport cluster uninstall
```

---

## Daemon service

`solo-provisioner-daemon` is a long-running systemd service. It handles consensus-node upgrade
handoffs and, for block nodes with traffic shaping on, reconciles nft sets from statusz and
attaches ingress shaping on each pod create.

### Prerequisites

| Prerequisite | Detail |
|---|---|
| Root privileges | Every daemon command needs `sudo` |
| A reachable cluster | Via the admin kubeconfig |

### Install

One step: bootstrap `daemon.yaml`, provision RBAC, generate the daemon kubeconfig, and start
the service.

```bash
# Interactive — prompts for components, cn-node-id and cn-orbit when daemon.yaml is absent
sudo solo-provisioner daemon service install

# Non-interactive / CI: consensus-node only
sudo solo-provisioner daemon service install \
  --components=consensus-node --cn-node-id=0.0.3 --cn-orbit=hedera-network

# Override the CN upgrade staging directory
sudo solo-provisioner daemon service install \
  --components=consensus-node --cn-node-id=0.0.3 --cn-orbit=hedera-network \
  --cn-upgrade-dir=/custom/path/data/upgrade/current

# Copy a pre-built daemon.yaml into place, then run the workflow
sudo solo-provisioner daemon service install --from-config=/path/to/daemon.yaml
```

| Flag | What it does | Default |
|---|---|---|
| `--components` | Comma-separated components to enable: `consensus-node`, `block-node` | prompted |
| `--cn-node-id` | Hedera node identifier for the consensus node, e.g. `0.0.3` | prompted |
| `--cn-orbit` | Namespace where consensus-node `NetworkUpgradeExecute` CRs are watched | prompted |
| `--cn-upgrade-dir` | Consensus-node upgrade staging directory | `/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current` |
| `--bn-orbit` | Namespace for the block-node component *(supported in a future release)* | prompted |
| `--from-config` | Path to an existing `daemon.yaml` to copy to `/opt/solo/weaver/config/daemon.yaml` | none |

**How config bootstrap works:**

- If `daemon.yaml` already exists, its values are used.
- Individual fields can still be overridden with the component-scoped flags above.
- In interactive mode the prompts are pre-filled with existing values, so pressing Enter keeps
  them.

**Adding or removing a component:** run `daemon service uninstall` first, then re-run
`install` with the new `--components` list. At least one component must be selected — RBAC and
kubeconfigs are only provisioned for the components you choose.

### Check, start, stop, uninstall

```bash
# Print the full /status response: per-component monitor state, connectivity errors,
# and prerequisite probe failures. 'status' is an alias for 'check'.
sudo solo-provisioner daemon service check
sudo solo-provisioner daemon service status

sudo solo-provisioner daemon service start
sudo solo-provisioner daemon service stop
sudo solo-provisioner daemon service uninstall
```

---

## Consensus migration soak

Drives the consensus-node migration **soak watcher** that runs inside
`solo-provisioner-daemon`. These commands talk to the running daemon over its Unix socket, so
the daemon must already be installed and running — see [Daemon service](#daemon-service).

Soak lifecycle lives under `consensus migration soak`, separate from the `daemon service` tree
which is scoped to daemon lifecycle only.

```bash
# Start
sudo solo-provisioner consensus migration soak start \
  --node-id=0.0.3 \
  --cutover-ts=2025-09-01T00:00:00Z \
  --migration-plan=/path/to/migration-plan.yaml

# Stop and delete state — the daemon will NOT auto-resume
sudo solo-provisioner consensus migration soak stop

# Stop but keep elapsed soak time — the daemon WILL auto-resume on its next restart
sudo solo-provisioner consensus migration soak stop --keep-state

# Status
sudo solo-provisioner consensus migration soak status
```

| Command | Flag | Required | What it does |
|---|---|---|---|
| `start` | `--node-id` | yes | Consensus node ID |
| `start` | `--cutover-ts` | yes | Cutover timestamp, RFC-3339 (`2025-09-01T00:00:00Z`) |
| `start` | `--migration-plan` | yes | Path to the migration plan file on the host |
| `stop` | `--keep-state` | no | Preserve `cutover-state.jsonl` so the daemon resumes on restart. Default `false` |

---

## Utilities

```bash
# Human-readable
solo-provisioner version

# JSON
solo-provisioner version --output=json

# Short flag
solo-provisioner -v
```

Help is available at every level of the command tree:

```bash
solo-provisioner --help
solo-provisioner block --help
solo-provisioner block node --help
solo-provisioner block node install --help
```
