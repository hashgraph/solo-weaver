# Quickstart Guide

Solo Weaver is a Kubernetes-native deployment automation platform for Hedera network components. It enables node
operators to migrate from traditional deployment models to modern, containerized infrastructure with automated lifecycle
management.

Below is a quickstart guide to get you up and running with Solo Weaver.

## Prerequisites

- Unix operating system (Tested on: Debian 13.1.0, Ubuntu 22.04)
- `curl` installed

> **Note:** No system users need to be pre-created. The `weaver:2500` service account is
> created automatically during `solo-provisioner install`. The `hedera:2000` user and group
> (used for node storage ownership — block node, consensus node, and similar workloads) are
> created automatically when the relevant `node install` command is first run.

## Install

- Run the single-line installer:

```bash
curl -sSL https://raw.githubusercontent.com/hashgraph/solo-weaver/main/install.sh | bash
```

- Verify installation:

```
solo-provisioner --help
```

### Uninstall

```bash
sudo solo-provisioner uninstall --yes
```

`--yes` is required — the command refuses to run without it. It removes, irreversibly:

- the `solo-provisioner` CLI and its `/usr/local/bin` symlink
- the `solo-provisioner-daemon` binary and its systemd service
- the `solo-provisioner-network-nft` and `solo-provisioner-bandwidth-shaper` boot units
- the configuration tree under `/etc/solo-provisioner` (rendered `.nft` files, the policy registry, tc device/class configs)

Tear down the workloads first — the command fails while a Kubernetes cluster is still provisioned, since every teardown command lives in the binary it is about to delete:

```bash
sudo solo-provisioner block node uninstall
sudo solo-provisioner kube cluster uninstall
sudo solo-provisioner uninstall --yes
```

Loaded nft tables and tc qdiscs are left alone; they do not survive a reboot once the boot-replay units and their inputs are gone.

**Additional Flags**

| Flag | Description |
|------|-------------|
| `--yes` | Confirm removal of the CLI, the daemon and its service, the network boot units, and `/etc/solo-provisioner` |

---

## Global Flags

These flags are available for all commands:

| Flag                | Short | Description                                              | Default                    |
|---------------------|-------|----------------------------------------------------------|----------------------------|
| `--config`          | `-c`  | Path to configuration file                               | None                       |
| `--profile`         | `-p`  | Deployment profile                                       | Required for most commands |
| `--output`          | `-o`  | Output format (text, json)                               | `text`                     |
| `--non-interactive` | —     | Disable TUI and output raw logs; useful for CI/pipelines | `false`                    |
| `--version`         | `-v`  | Show version                                             | -                          |
| `--help`            | `-h`  | Show help                                                | -                          |

> **About `--output`:** selects the **stdout format**.
> - **`text`** (default) — human-readable output: the interactive TUI on a
>   terminal, or plain console log lines when piped / `--non-interactive`.
> - **`json`** — machine-readable output for automation (Ansible, `jq`, CI).
>   Emits one JSON object per log event (NDJSON) on stdout, followed by a final
>   tagged summary object `{"type":"summary","status":…,"report_path":…,"report":{…}}`.
>   Select it with `jq 'select(.type=="summary")'` (a trailing log line may
>   follow, so filter by the tag rather than assuming it is the last line).
>   Passing `-o json` **forces non-interactive mode** (the TUI never renders),
>   matching common tooling (`kubectl -o json`, `terraform -json`). Human error
>   panels still go to **stderr**, so the stdout JSON stream stays clean.
>
> The YAML workflow report file (`setup_report_<timestamp>.yaml`) is written in
> both modes regardless of `-o`; its path is reported in the `report_path=…`
> field (and in the JSON summary object).

### Error Handling Flags

Most installation commands support these execution control flags:

| Flag                  | Description                                | Default |
|-----------------------|--------------------------------------------|---------|
| `--stop-on-error`     | Stop execution on first error              | `true`  |
| `--rollback-on-error` | Rollback executed steps on error           | `false` |
| `--continue-on-error` | Continue executing steps even if some fail | `false` |

---

## Deployment Profiles

Solo Provisioner supports five deployment profiles that configure behavior and defaults:

| Profile      | Description                   | Use Case                |
|--------------|-------------------------------|-------------------------|
| `local`      | Local development and testing | Development, CI/CD      |
| `perfnet`    | Performance testing network   | Load testing            |
| `testnet`    | Hedera Testnet                | Integration testing     |
| `previewnet` | Hedera Previewnet             | Preview/staging testing |
| `mainnet`    | Hedera Mainnet                | Production deployment   |

> **Important**: Always use `--profile` to specify your target environment.

---

## Command Reference

### Block Node Commands

The primary commands for managing Hedera Block Nodes.

#### Check System Readiness

Run preflight checks to validate the system is ready for Block Node deployment:

```bash
# Basic preflight check
sudo solo-provisioner block node check --profile=mainnet

# With custom config file
sudo solo-provisioner block node check --profile=testnet --config=/path/to/config.yaml

# Check hardware requirements for a specific plugin preset
sudo solo-provisioner block node check --profile=mainnet --plugin-preset=tier1-lfh

# Check with explicit plugin list
sudo solo-provisioner block node check --profile=mainnet --plugins=com.hedera.block.suites.BlockStreamPublishing,com.hedera.block.suites.LocalFileSystemRecorder
```

**What it checks**:

- System requirements (CPU, memory, disk)
- Required dependencies
- Network connectivity
- Storage availability

**Additional Flags**:

| Flag              | Description                                                                                | Default |
|-------------------|--------------------------------------------------------------------------------------------|---------|
| `--plugin-preset` | Plugin preset to deploy (`tier1-lfh`, `tier1-rfh`, or `custom`); used for hardware sizing | `""`    |
| `--plugins`       | Comma-separated plugin list; overrides `--plugin-preset` when set                         | `""`    |

#### Install Block Node

Deploy a complete Hedera Block Node with Kubernetes cluster:

```bash
# Basic installation with defaults
sudo solo-provisioner block node install --profile=local

# Production installation with custom values
sudo solo-provisioner block node install \
  --profile=mainnet \
  --config=/path/to/config.yaml \
  --values=/path/to/custom-values.yaml

# With custom storage configuration
# Note: --verification-size applies to chart versions below 0.37.0;
# --application-state-size applies to chart versions 0.37.0 and above
# (verification retires and application-state appears in the same chart cutover,
# hiero-ledger/hiero-block-node#3025). The flag for the inactive storage is
# silently ignored outside its applicable range.
sudo solo-provisioner block node install \
  --profile=mainnet \
  --base-path=/mnt/nvme \
  --live-size=50Gi \
  --archive-size=500Gi \
  --verification-size=50Gi \
  --log-size=10Gi \
  --application-state-size=500Mi

# With specific chart version
sudo solo-provisioner block node install \
  --profile=testnet \
  --chart-version=0.22.1 \
  --namespace=hedera-block
```

**Available Flags**:

| Flag                      | Description                                                                                                                           |
|---------------------------|---------------------------------------------------------------------------------------------------------------------------------------|
| `--values`, `-f`          | Custom Helm values file                                                                                                               |
| `--chart-repo`            | Helm chart repository URL                                                                                                             |
| `--chart-version`         | Specific chart version                                                                                                                |
| `--timeout`               | Timeout for the block node Helm install/upgrade, as a Go duration (e.g. `10m`, `600s`, `1h`). The operation is rolled back (`--atomic`) if it exceeds this budget (default: `5m0s`) |
| `--namespace`             | Kubernetes namespace                                                                                                                  |
| `--release-name`          | Helm release name                                                                                                                     |
| `--base-path`             | Base path for all storage                                                                                                             |
| `--archive-path`          | Archive storage path                                                                                                                  |
| `--live-path`             | Live storage path                                                                                                                     |
| `--verification-path`     | Verification storage path (chart versions below 0.37.0)                                                                               |
| `--log-path`              | Log storage path                                                                                                                      |
| `--application-state-path` | Application-state storage path (chart versions 0.37.0 and above; introduced by hiero-ledger/hiero-block-node#3025)                   |
| `--live-size`             | Live storage size (e.g., 10Gi)                                                                                                        |
| `--archive-size`          | Archive storage size                                                                                                                  |
| `--verification-size`     | Verification storage size (chart versions below 0.37.0)                                                                               |
| `--log-size`              | Log storage size                                                                                                                      |
| `--application-state-size` | PV/PVC size for application-state storage (e.g., `500Mi`, `1Gi`); chart versions 0.37.0 and above                                    |
| `--plugin-preset`         | Plugin preset to deploy (`tier1-lfh`, `tier1-rfh`, `custom`, or `none` for no override — use `--values`/chart default); prompts interactively when omitted |
| `--plugins`               | Comma-separated plugin list; overrides `--plugin-preset` when set                                                                     |
| `--plugins-size`          | PV/PVC size for plugins storage (e.g., `5Gi`, `10Gi`)                                                                                 |
| `--plugins-path`          | Path for plugins storage                                                                                                              |
| `--historic-retention`    | Historic block retention threshold (`0` = unlimited)                                                                                  |
| `--recent-retention`      | Recent block retention threshold (default: `96000`)                                                                                   |
| `--load-balancer-enabled` | Inject MetalLB address-pool annotation into the block node service; set to `false` for environments without MetalLB (default: `true`). See [Block-node service exposure](./block-node-service-exposure.md) for how this interacts with `service.type` and the chart's split topology. |
| `--firewall-enabled`      | Apply the node-level host firewall (`inet weaver-host-firewall` table: SSH/mgmt allowlist, ICMP policy, in-cluster ports). Opt-in (default: `false`); set to `true` to have this tool manage the host firewall |
| `--mgmt-cidrs`            | Host firewall SSH/management allowlist CIDRs (IPv4 and/or IPv6 — each entry is routed to the matching `ipv4_addr`/`ipv6_addr` set). Empty skips the host firewall. |
| `--blocked-cidrs`         | Host firewall operator-curated block list CIDRs (IPv4 and/or IPv6), dropped inbound, outbound, and forwarded — including established connections, and including pod-bound traffic. Distinct from the BN workload plane's `bn-restricted` set, which the traffic-shaper daemon manages automatically. |
| `--ssh-port`              | Host firewall SSH/management TCP port (default `22`)                                                                                  |
| `--pod-cidr`              | Host firewall pod CIDR for the in-cluster host-service ports rule (defaults to the cluster pod subnet). May be IPv4 and/or IPv6 (repeat or comma-separate for dual-stack). |
| `--in-cluster-ports`      | Host firewall in-cluster host-service ports (defaults to `6443,4244,7472,10250`)                                                     |
| `--traffic-shaping-enabled` | Create the BN workload network-policy plane (`inet weaver-workload-policy` classification) and tc HTB traffic shaping, and install the traffic-shaper daemon. Opt-in (default: `false`); set to `true` to get all three |
| `--egress-interface`      | Physical NIC for the `$EGRESS` HTB traffic-shaper hierarchy (e.g. `eth0`). Auto-detected from the default route when omitted; use this flag to override on multi-NIC hosts. Renders `/usr/local/sbin/solo-provisioner-bandwidth-shaper.sh` and installs `solo-provisioner-bandwidth-shaper.service` so the HTB hierarchy survives reboot. |
| `--link-rate`             | NIC line rate in tc-style format (e.g. `1gbit`, `100mbit`), or `auto` to detect and store the link speed at install time (written as explicit proportional class rates). Auto-detected from sysfs at each boot when omitted. Interactively, the prompt accepts a rate, `auto`, or blank. The link is full-duplex, so this rate parameterizes both the `$EGRESS` and `$VETH` HTB trunks. |
| `--shape`                 | Per-class HTB bandwidth override, repeatable: `--shape <class>=rate=<r>,ceil=<c>,prio=<p>` (e.g. `--shape publisher=rate=800mbit,ceil=1gbit,prio=0`). Any subset of `rate`/`ceil`/`prio` may be given; classes not overridden use the profile defaults. Valid classes: `publisher`, `backfill-response`, `reserve-ingress`, `partner`, `public`, `reserve-egress`. |
| `--daemon-version`        | Version of `solo-provisioner-daemon` to auto-download when traffic shaping installs the daemon (defaults to this CLI's own version). Ignored when `--daemon-bin` is set. |
| `--daemon-bin`            | Local path to a pre-built `solo-provisioner-daemon` binary, installed as-is instead of downloaded. Highest-precedence source; omit it to resolve the binary automatically (`SOLO_PROVISIONER_DAEMON_BIN`, then an already-installed binary, then the catalog). |
| `--statusz-base-url`      | Override the daemon's block-node statusz endpoint with an explicit `http(s)` base URL (e.g. `http://127.0.0.1:8080`) for a port-forward or directly-reachable BN. Merged into `daemon.yaml` (`components.block_node.statusz.base_url`); omitting the flag preserves any value already on disk. Only when no `base_url` is set at all does the daemon discover the endpoint from the watched BN pod. |
| `--statusz-poll-interval` | Cadence at which the daemon's block-node traffic-shaper monitor polls statusz, as a positive Go duration (e.g. `5s`, `30s`). Merged into `daemon.yaml` (`components.block_node.statusz.poll_interval`); omitting the flag preserves any value already on disk. Only when no `poll_interval` is set at all does the daemon fall back to its `5s` default. |

> **Host firewall**: `block node install` can lay down the node-level `inet
> host` firewall (SSH/management allowlist, ICMP policy, in-cluster host-service
> ports) — regardless of whether it's bootstrapping a bare-metal host or deploying
> onto an already-existing cluster. This is opt-in (`--firewall-enabled`, default
> `false`) so existing non-interactive callers are unaffected; when enabled, the
> firewall is owned by the block-node workflow, not by the generic `kube cluster
> install` (which provisions clusters for other purposes too and should not
> unconditionally apply node-specific rules). The `--mgmt-cidrs` / `--blocked-cidrs`
> / `--ssh-port` / `--pod-cidr` / `--in-cluster-ports` flags (and interactive
> prompts) configure it once enabled. An empty management allowlist skips the
> firewall to avoid locking the host out of SSH. `--blocked-cidrs` only takes
> effect when `--firewall-enabled` is also true — both live on the same `inet
> host` table, rendered by the same step — but the *set itself* is a plain deny
> list, dropped before every other rule (including established connections),
> and is purely operator-managed for its whole lifecycle, unlike the
> daemon-owned `bn-restricted` set on the BN workload plane.

> **IPv6 / dual-stack**: both the host firewall (`inet weaver-host-firewall`) and the BN workload
> plane (`inet weaver-workload-policy`, which drives traffic-shaping classification) are
> dual-stack. The v4 and v6 sets are always rendered; `--mgmt-cidrs`,
> `--blocked-cidrs`, `--pod-cidr` (and `network policy --cidrs`) accept mixed
> IPv4/IPv6 lists and route each entry to the matching family's set. The host
> firewall's default-drop input chain explicitly admits ICMPv6 Neighbor Discovery
> (NS/NA/RS/RA, hop-limit-255 guarded), MLD, and `packet-too-big` (the IPv6 PMTUD
> signal) — without these IPv6 would be non-functional under the drop policy.
> IPv6 workload classification is active once a v6 `--pod-cidr` is supplied
> (auto-detection resolves only the v4 pod CIDR today; pass the v6 companion
> explicitly).

> **Traffic shaping gate**: daemon activation is not a separate decision from
> traffic shaping — `block node install` automatically installs and provisions
> the traffic-shaper daemon whenever traffic shaping is enabled, with no
> separate prompt or flag. `--traffic-shaping-enabled` is opt-in (default
> `false`) so existing non-interactive callers are unaffected; enabling it
> creates the BN workload network-policy plane (`inet weaver-workload-policy` classification),
> tc HTB shaping, and installs the daemon, all together — declining (or the
> default) skips the egress NIC/link-rate prompts too, since there would be
> nothing left for any of it to reconcile. This decision is durable: it's
> recorded in the runtime state and honored by later `reconfigure`/`upgrade`
> runs too, not just the install where it was made. This whole gate is
> independent of `--firewall-enabled`, which only gates the host's own SSH/mgmt
> firewall.

> **Traffic-shaper defaults**: `block node install` records both the `$EGRESS`
> (physical NIC) and the `$VETH` (per-pod ingress) HTB shapes using the design's
> default per-class budgets, so the daemon can install the ingress hierarchy on
> the next pod create without any manual `network shape` step. The `$EGRESS` shape
> is persisted for reboot replay (`solo-provisioner-bandwidth-shaper.service`); the
> ingress shape is recorded as config only (the `$VETH` is ephemeral and replayed
> per-pod). Ingress bandwidth defaults to the egress `--link-rate`. Tune any class
> afterward with `network shape set --class <name> --rate/--ceil/--prio`, or at
> install time with `--shape <class>=rate=...,ceil=...,prio=...`.

> **Network policies**: when traffic shaping is enabled (see the traffic shaping
> gate above), `block node install` lays down the `inet weaver-workload-policy`
> classification plane by creating the fixed set of BN policies (`bn-publisher`,
> `bn-subscriber-in`, `bn-partner-out`, `bn-public-out`, `bn-health`,
> `bn-restricted`, `bn-backfill`) idempotently, then persists the rendered
> `network-weaver-workload-policy.nft` for reboot replay. `bn-health` is the one
> non-classification entry: it drops the block node's health/statusz port
> (`blockNode.ports.health`) on the forward path from every source. Its only
> consumers are the node's kubelet and the provisioner, both of which reach the
> pod without ever traversing that hook, so nothing has to be allowlisted and the
> port is unreachable from off-node. No set is seeded with membership at install:
> every set's membership — including `bn-restricted` — is reconciled at runtime
> by the traffic-shaper daemon from the block node's statusz; `bn-restricted` in
> particular reflects a "restricted" category the block node itself reports, so it
> always starts empty and has no install-time flag. A permanent, purely
> operator-managed block list lives instead on the host firewall
> (`--blocked-cidrs`, a different table).
> Re-running the install never clobbers operator-applied set membership or per-class
> shape values; `--force` re-renders the static rules from these definitions.

> **Traffic-shaper daemon**: a block node without its traffic-shaper daemon has no
> ingress prioritization (the `$VETH` HTB is never installed) and nothing
> reconciling the daemon-owned nft sets from statusz. At the end of a successful
> install, `block node install` installs and provisions the daemon for this
> block node's namespace automatically whenever traffic shaping is enabled (see
> the traffic-shaping gate above) — no separate prompt or flag. If the daemon is
> already active, this is a no-op.
>
> The daemon binary itself is resolved automatically, in this order: `--daemon-bin`,
> then `SOLO_PROVISIONER_DAEMON_BIN`, then a daemon binary already installed in the
> weaver bin directory, then a download from the infrastructure catalog at
> `--daemon-version` (defaulting to this CLI's own version). Locally-built CLIs carry
> a placeholder version (`0.0.0`) with no release to download, but
> `sudo solo-provisioner install` copies the co-built `solo-provisioner-daemon` from
> the same `bin/` directory onto the host, so a `task build` followed by a self-install
> leaves a matching daemon already in place and nothing needs to be supplied. When every
> source comes up empty the command fails with a resolution hint listing the ways to
> supply a binary — it never prompts for a path.

#### Upgrade Block Node

Upgrade an existing Block Node deployment:

```bash
# Upgrade with new values file
sudo solo-provisioner block node upgrade \
  --profile=mainnet \
  --values=/path/to/new-values.yaml

# Upgrade to specific chart version
sudo solo-provisioner block node upgrade \
  --profile=mainnet \
  --chart-version=0.23.0

# Upgrade and reset to chart defaults (don't reuse previous values)
sudo solo-provisioner block node upgrade \
  --profile=mainnet \
  --values=/path/to/values.yaml \
  --no-reuse-values
```

**Additional Flags**:

| Flag                | Description                                                  | Default |
|---------------------|--------------------------------------------------------------|---------|
| `--no-reuse-values` | Don't reuse previous release values                          | `false` |
| `--with-reset`      | Wipe block node data directories; PVs and PVCs are preserved | `false` |
| `--timeout`         | Timeout for the block node Helm install/upgrade, as a Go duration (e.g. `10m`, `600s`, `1h`). The operation is rolled back (`--atomic`) if it exceeds this budget | `5m0s` |

> **Firewall & traffic shaping on upgrade**: `upgrade` does **not** expose the
> `--firewall-enabled` / `--traffic-shaping-enabled` gates and never prompts for
> them — a version bump is a routine operation, so it reads the persisted install
> decision and silently re-asserts the matching network plane (host firewall,
> BN policy plane, tc shaping, daemon monitor) create-if-missing. It never turns a
> feature on or off from an upgrade; to change enablement, use `reconfigure`.

#### Reset Block Node

Reset Block Node storage by clearing all data files. This is useful for re-provisioning or when you need to start fresh:

```bash
# Basic reset - clears all storage directories
sudo solo-provisioner block node reset --profile=mainnet

# Reset with custom config
sudo solo-provisioner block node reset \
  --profile=mainnet \
  --config=/path/to/config.yaml
```

**What it does**:

1. Scales down the Block Node StatefulSet to 0 replicas
2. Waits for all pods to terminate
3. Clears all storage directories (archive, live, log, plus the version-specific optional storages: `verification` on chart versions below 0.37.0, `application-state` on chart versions 0.37.0 and above, and `plugins` on chart versions 0.28.1 and above)
4. Scales the StatefulSet back up to 1 replica
5. Waits for pods to become ready

> **Warning**: This command will delete all block data. Use with caution in production environments.

**Upgrade with Reset**:

If you need to upgrade and reset storage in one operation, use the upgrade command with `--with-reset`:

```bash
# Upgrade chart version and reset storage
sudo solo-provisioner block node upgrade \
  --profile=mainnet \
  --chart-version=0.24.0 \
  --with-reset

# Upgrade with new values and reset
sudo solo-provisioner block node upgrade \
  --profile=mainnet \
  --values=/path/to/new-values.yaml \
  --with-reset
```

#### Reconfigure Block Node

Re-apply configuration to an existing Block Node deployment without changing its chart version:

```bash
# Reconfigure with updated values file
sudo solo-provisioner block node reconfigure \
  --profile=mainnet \
  --values=/path/to/updated-values.yaml

# Reconfigure without reusing previous Helm values
sudo solo-provisioner block node reconfigure \
  --profile=mainnet \
  --values=/path/to/values.yaml \
  --no-reuse-values

# Reconfigure and skip the pod rollout-restart
sudo solo-provisioner block node reconfigure \
  --profile=mainnet \
  --values=/path/to/values.yaml \
  --no-restart

# Enable traffic shaping on a block node that was installed without it
sudo solo-provisioner block node reconfigure \
  --profile=mainnet \
  --traffic-shaping-enabled=true

# Disable the host firewall that was previously enabled (tears the table down)
sudo solo-provisioner block node reconfigure \
  --profile=mainnet \
  --firewall-enabled=false

# Point the daemon's statusz monitor at a port-forwarded BN and slow its poll
sudo solo-provisioner block node reconfigure \
  --profile=mainnet \
  --statusz-base-url=http://127.0.0.1:8080 \
  --statusz-poll-interval=10s
```

**Additional Flags**:

| Flag                | Description                                                                                           | Default |
|---------------------|-------------------------------------------------------------------------------------------------------|---------|
| `--no-reuse-values` | Don't reuse previous release values                                                                   | `false` |
| `--no-restart`      | Skip rollout-restart of the block node pod after reconfiguring                                        | `false` |
| `--with-reset`      | Wipe block node data directories; PVs and PVCs are preserved                                          | `false` |
| `--purge-storage`   | Delete PersistentVolumes and PersistentVolumeClaims in addition to wiping data (implies --with-reset) | `false` |
| `--firewall-enabled` | Enable or disable the node-level host firewall (`inet weaver-host-firewall` table) on an existing install. Seeded from the firewall's current on-host state — a live table always seeds enabled, however it was created — so a no-flag reconfigure keeps it as-is; pass `=false` to tear the table down, `=true` (with `--mgmt-cidrs`) to create it. Same sub-flags as `install` (`--mgmt-cidrs`, `--blocked-cidrs`, `--ssh-port`, `--pod-cidr`, `--in-cluster-ports`). | current state |
| `--traffic-shaping-enabled` | Enable or disable the BN traffic-shaping bundle (network-policy plane + tc HTB shaping + daemon traffic-shaper monitor) on an existing install. Seeded from the persisted install decision, so a no-flag reconfigure keeps it; pass `=true` to create it (with `--egress-interface`/`--link-rate`/`--shape`/`--daemon-bin` as on `install`), `=false` to tear it down. | persisted state |
| `--statusz-base-url`      | Override the daemon's block-node statusz endpoint with an explicit `http(s)` base URL (e.g. `http://127.0.0.1:8080`) for a port-forward or directly-reachable BN. Merged per-field into `daemon.yaml` (`components.block_node.statusz.base_url`); omitting the flag preserves whatever is already on disk. Only when no `base_url` exists on disk does the daemon fall back to discovering the endpoint from the watched BN pod. | preserved on disk |
| `--statusz-poll-interval` | Cadence at which the daemon's block-node traffic-shaper monitor polls statusz, as a positive Go duration (e.g. `5s`, `30s`). Merged per-field into `daemon.yaml` (`components.block_node.statusz.poll_interval`); omitting the flag preserves whatever is already on disk. Only when no `poll_interval` exists on disk does the daemon fall back to its `5s` default. | preserved on disk |

> **Storage path changes**: Local PV `hostPath.path` is immutable. If your
> reconfigure changes any storage path, you must pass `--purge-storage` so the
> existing PV/PVCs are deleted and recreated at the new paths. Running
> `reconfigure --with-reset` with a path change is rejected with a clear error.

> **Enabling/disabling firewall & traffic shaping after install**: `reconfigure`
> exposes the same `--firewall-enabled` / `--traffic-shaping-enabled` gates as
> `install`, so an operator can turn either feature on or off on an
> already-deployed block node without a full `install --force` reinstall. Both
> gates are seeded from the block node's **current** state — the host firewall from
> the last enable/disable decision *and* from whether the `inet weaver-host-firewall` table is
> live (a table that exists always seeds enabled, including one created by hand with
> `network firewall create`), traffic shaping from the persisted install
> decision — so a routine reconfigure that doesn't pass the flag (or accepts the
> interactive default) never changes enablement. Teardown only happens on an
> explicit toggle: answering **No** (or passing `=false`) for a currently-enabled
> feature removes it — the `inet weaver-host-firewall` table for the firewall; every `bn-*` policy,
> the tc egress shaping, and the daemon's block-node traffic-shaper monitor for
> traffic shaping. Disabling the traffic-shaper monitor does **not** uninstall the
> shared daemon service, so a co-located component (e.g. consensus-node monitoring)
> keeps running. Enabling traffic shaping activates the daemon automatically, just
> like `install`.

#### Uninstall Block Node

`block node uninstall` has three variants depending on what you want to keep:

| Command                                | Helm release | Data on disk | PV/PVC objects |
|----------------------------------------|--------------|--------------|----------------|
| `block node uninstall`                 | removed      | kept         | kept           |
| `block node uninstall --with-reset`    | removed      | **wiped**    | kept           |
| `block node uninstall --purge-storage` | removed      | **wiped**    | **deleted**    |

```bash
# Basic uninstall — release removed, data and PV/PVCs preserved for a future re-install
sudo solo-provisioner block node uninstall --profile=mainnet

# Wipe data but keep PV/PVCs so a re-install can reuse them
sudo solo-provisioner block node uninstall \
  --profile=mainnet \
  --with-reset

# Fully clean up — release, data, PVCs, and PVs all removed
sudo solo-provisioner block node uninstall \
  --profile=mainnet \
  --purge-storage
```

**Additional Flags**:

| Flag              | Description                                                                                           | Default |
|-------------------|-------------------------------------------------------------------------------------------------------|---------|
| `--with-reset`    | Wipe block node data directories; PVs and PVCs are preserved                                          | `false` |
| `--purge-storage` | Delete PersistentVolumes and PersistentVolumeClaims in addition to wiping data (implies --with-reset) | `false` |

> **Picking the right one**: use the default uninstall if you plan to re-install
> against the same data. Use `--with-reset` to start fresh on disk but keep the
> PV/PVC topology. Use `--purge-storage` for a full cleanup; this is the only
> targeted way to remove the block-node PVs without tearing down the whole
> cluster via `kube cluster uninstall`.

#### Reconcile Traffic Shaper

Reconcile the block node traffic-shaper's `inet weaver-workload-policy` nft policy set membership
from the block node's statusz endpoints. The command fetches the active
inbound/outbound endpoints, maps them to the owned policy sets
(`bn-publisher`, `bn-partner`, `bn-restricted`, `bn-backfill`), and applies only
the policies whose membership changed.

```bash
# Detect only (unprivileged): print a digest of the desired membership; touches no nft state
solo-provisioner block node reconcile-shaper \
  --statusz-url=http://10.0.0.5:8080 \
  --check

# Apply (root): read live nft, diff, and rewrite the changed policies
sudo solo-provisioner block node reconcile-shaper \
  --statusz-url=http://10.0.0.5:8080

# Machine-readable output
sudo solo-provisioner block node reconcile-shaper \
  --statusz-url=http://10.0.0.5:8080 \
  --output=json
```

**Additional Flags**:

| Flag             | Description                                                                          | Default |
|------------------|--------------------------------------------------------------------------------------|---------|
| `--statusz-url`  | Base URL of the block node's statusz endpoints (required)                            | —       |
| `--check`        | Only fetch and print the desired-membership digest; read/write no nft state (unprivileged) | `false` |

> **Privilege**: `--check` never touches nft and needs no privilege. The apply
> path (no `--check`) reads and writes the live `inet weaver-workload-policy` sets and must run as
> root; the daemon invokes it via sudo.

#### Attach Ingress Traffic Shaper (`tc-attach`)

Install the `$VETH` ingress HTB hierarchy (classes `1:10`/`1:20`/`1:30`, default
`reserve-ingress`, `fq_codel` leaves, no tc filters) on a block node pod's
host-side veth, using the per-class ingress budgets recorded by
`network shape`. The solo-provisioner-daemon's pod-lifecycle watcher runs this
via sudo on each block node pod create, passing the veth it resolved for the
pod; you can also run it by hand for debugging.

```bash
# Install the ingress HTB hierarchy on a resolved host-side veth (root)
sudo solo-provisioner block node tc-attach --veth lxc1a2b3c

# Tear it down (best-effort; the kernel also removes it when the pod's veth disappears)
sudo solo-provisioner block node tc-attach --veth lxc1a2b3c --detach
```

**Additional Flags**:

| Flag       | Description                                                                                 | Default |
|------------|---------------------------------------------------------------------------------------------|---------|
| `--veth`   | Host-side veth interface to (de)install the ingress HTB hierarchy on, e.g. `lxc1a2b3c` (required) | —       |
| `--detach` | Tear down the ingress HTB hierarchy on the veth instead of installing it (best-effort)      | `false` |

> **Privilege**: `tc-attach` always mutates live kernel tc state and must run as
> root (the daemon invokes it via sudo). Unlike `reconcile-shaper`, it has no
> unprivileged `--check` mode. It requires the ingress shape to have been
> recorded first (normally by `block node install` via `network shape`).

---

### Kubernetes Commands

Manage the underlying Kubernetes cluster and its components.

#### Install Kubernetes Cluster

Sets up a complete single-node Kubernetes environment with all required components:

**Components Installed**:

- **kubeadm/kubelet**: Kubernetes cluster initialization and node agent
- **CRI-O**: Container runtime
- **Cilium**: Container networking (CNI)
- **MetalLB**: Load balancer for bare-metal Kubernetes
- **Helm**: Kubernetes package manager
- **kubectl**: Kubernetes CLI
- **k9s**: Terminal-based Kubernetes UI
- **Metrics Server**: Resource metrics for pods and nodes

`kube cluster install` provisions a Kubernetes cluster independent of any specific node type — it does **not** apply any node-specific firewall rules. The node-level **host firewall** (the `inet weaver-host-firewall` nftables table) is applied by the block-node workflow instead (see [Install Block Node](#install-block-node) below).

Cluster install is **workload-agnostic**: it validates only the Kubernetes substrate
hardware floor (what Kubernetes itself needs to run), not any per-workload sizing. It
therefore takes **no `--profile` or `--node-type`**. Workload-sized hardware validation
happens later, at `block node check` / `block node install`, where the deployment
profile and plugin preset are known.

```bash
# Install the full Kubernetes stack
sudo solo-provisioner kube cluster install

# With error handling
sudo solo-provisioner kube cluster install --rollback-on-error
```

**Flags**:

| Flag                 | Short | Description                                                        | Required |
|----------------------|-------|-------------------------------------------------------------------|----------|
| `--rollback-on-error`| —     | Roll back completed steps if a later step fails                   | No       |
| `--stop-on-error`    | —     | Stop at the first failing step (default)                          | No       |
| `--continue-on-error`| —     | Continue past failing steps                                       | No       |

> **Deprecated:** `--profile` and `--node-type` are no longer used by `kube cluster install`.
> They are still accepted (hidden) so existing scripts do not break, but their values are
> **ignored** and a notice is printed if you pass them. Remove them from your invocations.

#### Uninstall Kubernetes Cluster

Tears down the entire Kubernetes stack including all components (kubeadm, CRI-O, Cilium, etc.) while preserving the
downloads cache:

```bash
# Basic uninstall
sudo solo-provisioner kube cluster uninstall

# Continue even if some steps fail
sudo solo-provisioner kube cluster uninstall --continue-on-error
```

> **Warning**: This tears down the entire cluster. All running workloads will be stopped.

---

### Network Commands

Manage node-level network state behind the traffic shaper. The `firewall` scope manages the node-agnostic `inet weaver-host-firewall` nftables table — the host's own management allowlist, ICMP policy, in-cluster host-service ports, and any number of named allow rules. It is separate from the `inet weaver-workload-policy` workload plane and applies to every node type (block, consensus, mirror, relay).

The table holds two kinds of record:

- **Three reserved blocks** — `mgmt` (management allowlist), `blocked` (operator block list, dropped on `prerouting`, `input` and `output`), and `in_cluster` (host-service ports reachable from the pod CIDR). They are first-class because weaver derives or defaults their content and omitting one is dangerous.
- **Named allow rules** — an operator-authored source list x port list x protocol accept, for anything else the host must admit (Kubernetes control-plane ports, Cilium VXLAN, an admin jump host).

Structure — which rules exist, and what protocol each matches — is declared with `create-allow-rule`, or stated for the whole table at once in a config file passed to `create --from-file`. Membership — the addresses and ports inside a rule — is mutable straight from the CLI either way.

#### Create the Host Firewall

create-if-missing: if the `inet weaver-host-firewall` table already exists, the command makes no changes unless `--force` is passed (which re-renders from the flags or file). Every mutation applies to the live kernel in one atomic `nft -f` transaction and atomically rewrites both `/etc/solo-provisioner/network-weaver-host-firewall.nft` and the config it was rendered from, `/etc/solo-provisioner/network-weaver-host-firewall.yaml`.

```bash
# Create with a management allowlist and the default in-cluster ports
sudo solo-provisioner network firewall create \
  --mgmt-cidrs 10.0.0.0/8 \
  --ssh-port 22 \
  --pod-cidr 10.4.0.0/24 \
  --in-cluster-ports 6443,4244,10250

# Re-render an existing table from new flags
sudo solo-provisioner network firewall create --mgmt-cidrs 10.0.0.0/8,192.168.0.0/16 --force
```

**Flags**:

| Flag                 | Description                                                       | Default            |
|----------------------|-------------------------------------------------------------------|--------------------|
| `--mgmt-cidrs`       | Management/SSH allowlist CIDRs (comma-separated or repeated) — **omitting this flag leaves the management allow rule with an empty source set under the default-drop policy, which will lock you out of new SSH connections** | (none) |
| `--blocked-cidrs`    | Operator block list CIDRs, dropped before any other rule           | (none)             |
| `--in-cluster-ports` | Host-service ports reachable from the pod CIDR                     | `4244,6443,7472,10250` |
| `--ssh-port`         | Management TCP port accepted from the allowlist (shorthand for a one-element `mgmt.ports`) | `22` |
| `--pod-cidr`         | Pod CIDR allowed to reach the in-cluster host-service ports        | auto-detected      |
| `--from-file`        | Declarative YAML config to render the whole table from (mutually exclusive with the flags above) | (none) |
| `--force`            | Re-render the table even if it already exists (global flag)        | `false`            |

When `--pod-cidr` is omitted it is **auto-detected** from the local node's `.spec.podCIDR` via the Kubernetes API (the node is matched by hostname, or the sole node on a single-node host). Detection is best-effort: `network firewall create` is node-agnostic and may run before a cluster exists, so if no cluster is reachable the command logs a warning and **omits the in-cluster-ports rule** — pass `--pod-cidr` explicitly to render it anyway.

ICMP is a fixed, safe ruleset: full ICMP from the management allowlist, and from every other source the path-health subset — `destination-unreachable` (Path MTU Discovery) and `time-exceeded` (traceroute) always accepted, with `echo-request` (ping) rate-limited to 10/second. There are deliberately no ICMP flags: dropping ICMP errors would silently break PMTUD for legitimate clients. The one configurable part is `icmp_echo` on an allow rule, which grants that rule's sources unmetered `echo-request`.

> There is no `--service-ports`: BN ports live only in `network policy --ports`. That traffic is forwarded rather than delivered locally, so an `input` rule for it would never match.

#### Declare Named Allow Rules

`create-allow-rule` declares one named allow rule; `add` then supplies its addresses and ports. No file is involved, and both lists take comma-separated values, so one `add` finishes the rule in a single atomic apply:

```bash
# Declare the rule, then populate it
sudo solo-provisioner network firewall create-allow-rule --name rudder_server --proto tcp --icmp-echo
sudo solo-provisioner network firewall add --name rudder_server \
  --cidr 200.201.203.205/32,10.1.0.0/16 --port 5309,8443,9000-9100

# Deletion needs no separate verb
sudo solo-provisioner network firewall delete --name rudder_server
```

**Flags**:

| Flag           | Description                                                                          | Default |
|----------------|--------------------------------------------------------------------------------------|---------|
| `--name`       | Name of the allow rule to declare (may not be a reserved block: `mgmt`, `blocked`, `in_cluster`) | (required) |
| `--proto`      | L4 protocol the rule's ports match: `tcp` or `udp`                                   | `tcp`   |
| `--icmp-echo`  | Grant this rule's sources unmetered ICMP echo-request, above the rate meter          | `false` |
| `--force`      | Replace an existing rule, **resetting the whole rule** — addresses, ports, `proto` and `icmp_echo` all return to their defaults unless supplied again (global flag) | `false` |

A rule is declared before it has any members, and **renders nothing** until it has at least one CIDR and either a port or `--icmp-echo` — so running the declare and the populate as separate commands never opens access early. An incomplete rule is reported as a warning on every apply.

Declaring is deliberately a separate verb from `add`: an unknown `--name` on `add`/`remove`/`set` keeps failing, so a typo edits nothing rather than quietly creating a second rule alongside the intended one. Re-declaring an existing name without `--force` warns and changes nothing, mirroring `network firewall create`. With `--force` the declaration **replaces** the rule outright, so `create-allow-rule --name x --force` on its own resets `proto` and `icmp_echo` as well as emptying the address and port lists — use `set` to change one field of a populated rule.

`--proto` and `--icmp-echo` are also settable on `set` (see below), so a rule's protocol can be corrected without deleting and re-declaring it. The reserved blocks reject both — they render a fixed shape.

##### Declaring the whole table from a file

`create --from-file` states the whole table at once, as an alternative to the sequence above:

```yaml
version: 1

mgmt:                                          # required
  cidrs: ["192.168.68.0/24"]                   # required
  ports: ["22"]                                # omitted -> 22

blocked:                                       # required
  cidrs: []                                    # required; [] means block nobody

in_cluster:                                    # required
  cidrs: ["10.4.0.0/14"]                       # omitted -> auto-detected; [] -> no rule
  ports: ["4244", "6443", "7472", "10250"]     # omitted -> the defaults above

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

| Field       | Required | Notes                                                                        |
|-------------|----------|------------------------------------------------------------------------------|
| `name`      | yes      | Also the nft set name. `mgmt`, `blocked` and `in_cluster` are reserved.       |
| `cidrs`     | yes      | IPv4 and/or IPv6 in one list; each entry is routed to `@<name>` or `@<name>6` by family |
| `ports`     | yes\*    | Single ports and inclusive ranges (`2379-2380`). \*Optional when `icmp_echo` is set, for an echo-only rule |
| `proto`     | no       | `tcp` (default) or `udp`. nft has no combined match, so a service on both is two rules |
| `icmp_echo` | no       | Grants unmetered `echo-request`, rendered above the rate meter                |

**The file is the whole table.** Nothing is inherited from the host's current firewall — only `add`/`remove`/`set` merge with what is already there. Two consequences:

- **`allow:` is declarative** — a rule absent from the file is **deleted**.
- **All three reserved blocks are required**, as is `cidrs` inside `mgmt` and `blocked`. An omitted block would fall back to a weaver default the file never stated, and for `mgmt` that default is an empty allowlist under the default-drop policy — a lockout nobody wrote down. To render no rule for a block, state it with an empty list (`in_cluster: {cidrs: []}`); the block still cannot be removed.

| Key                | Required | Omitted means                                                     |
|--------------------|----------|-------------------------------------------------------------------|
| `version`          | no       | the current schema version (`1`)                                   |
| `mgmt`             | **yes**  | — (rejected)                                                       |
| `blocked`          | **yes**  | — (rejected)                                                       |
| `in_cluster`       | **yes**  | — (rejected)                                                       |
| `mgmt.cidrs`       | **yes**  | — (rejected: no safe default exists)                               |
| `mgmt.ports`       | no       | `22`                                                               |
| `blocked.cidrs`    | **yes**  | — (rejected; write `[]` to block nobody)                           |
| `in_cluster.cidrs` | no       | auto-detect this node's pod CIDR (`[]` renders no in-cluster rule) |
| `in_cluster.ports` | no       | `4244,6443,7472,10250`                                             |
| `allow`            | no       | no named allow rules — **and any that exist are deleted**          |

`in_cluster.cidrs` is the one address list weaver can legitimately derive on its own, which is why it stays optional; its absence costs a rule rather than access to the host.

The same rule applies to the persisted config at `/etc/solo-provisioner/network-weaver-host-firewall.yaml`: a truncated or hand-edited file is refused rather than loaded with a defaulted management allowlist. To repair one, recover the retained previous generation (see [Re-apply / Recover the Host Firewall](#re-apply--recover-the-host-firewall)); failing that, re-run `create --from-file`, or delete the file and re-run the `create` + `create-allow-rule` sequence.

#### Modify a Rule's Addresses / Ports

`add`/`remove` merge with what is already there; `set` atomically replaces the full list. `--name` selects the rule — a reserved block or an allow rule:

```bash
sudo solo-provisioner network firewall add    --name mgmt     --cidr 10.1.0.0/16
sudo solo-provisioner network firewall add    --name blocked  --cidr 203.0.113.9/32
sudo solo-provisioner network firewall add    --name k8s-node --cidr 10.0.0.5/32 --port 9345
sudo solo-provisioner network firewall remove --name k8s-node --port 9345
sudo solo-provisioner network firewall set    --name mgmt     --cidrs 10.0.0.0/8,192.168.0.0/16
sudo solo-provisioner network firewall set    --name mgmt     --cidrs-file /etc/mgmt-cidrs.txt

# Both lists are comma-separated or repeated, and one invocation is one atomic
# apply — so a rule can be populated in full without a command per element
sudo solo-provisioner network firewall add --name k8s-node \
  --cidr 10.0.0.5/32,10.0.0.6/32 --port 6443,2379-2380,10250

# --proto and --icmp-echo change what an allow rule matches, rather than who is in it
sudo solo-provisioner network firewall set --name cilium-vxlan --proto udp
sudo solo-provisioner network firewall set --name admin --icmp-echo
sudo solo-provisioner network firewall set --name admin --icmp-echo=false
```

**Flags**:

| Verb                  | Flag           | Description                                                          |
|-----------------------|----------------|----------------------------------------------------------------------|
| `add`/`remove`/`set`  | `--name`       | Rule to modify: `mgmt`, `blocked`, `in_cluster`, or an allow rule name |
| `add`/`remove`        | `--cidr`       | CIDR(s) to add/remove (comma-separated or repeated)                  |
| `add`/`remove`        | `--port`       | Port(s) to add/remove; single ports or ranges                        |
| `set`                 | `--cidrs`      | Full CIDR list (replaces the existing list; an empty value clears it — except for `mgmt`, see below) |
| `set`                 | `--cidrs-file` | Alternative to `--cidrs`: a flat file of CIDRs, one per line or comma-separated, `#` comments allowed |
| `set`                 | `--ports`      | Full port list (replaces the existing list)                          |
| `set`                 | `--proto`      | L4 protocol the rule's ports match: `tcp` or `udp` (allow rules only; empty restores the `tcp` default) |
| `set`                 | `--icmp-echo`  | Grant or revoke unmetered ICMP echo-request for this rule's sources (allow rules only) |

> **Emptying the `mgmt` rule is guarded.** Clearing its addresses or ports — `set` with an empty value, or a
> `remove` of the last entry — would drop every **new** SSH connection under the default-drop policy, so the
> command fails unless `--force` (`-y`) is passed. Only `mgmt` is guarded: clearing `blocked` or `in_cluster`
> is the supported way to disable them.

> `add`/`remove` operate on membership only. To change an allow rule's `--proto` or `--icmp-echo` after it is declared, use `set` — `create-allow-rule --force` would reset the rest of the rule. The reserved blocks reject both flags outright, **including `--proto tcp`**: they render a fixed shape (TCP, with `mgmt` carrying its own broader ICMP type list), so accepting the value that happens to match would report a change the renderer ignores.

The pre-existing per-block flags are retained as shorthands that name their reserved block implicitly, so every earlier invocation still works unchanged:

```bash
sudo solo-provisioner network firewall add    --mgmt-cidr 10.1.0.0/16      # = --name mgmt --cidr
sudo solo-provisioner network firewall remove --blocked-cidr 203.0.113.9/32
sudo solo-provisioner network firewall add    --in-cluster-port 9100
sudo solo-provisioner network firewall set    --mgmt-cidrs 10.0.0.0/8 --in-cluster-ports 6443,4244
```

> Ports are removed by exact spec: removing `2379` from a rule holding `2379-2380` does nothing. An nft range is a single set element, so replace the range with `set --ports` rather than relying on an implicit split.

> Adding a CIDR already covered by one in the rule — `10.0.0.5/32` into a rule holding `10.0.0.0/24` — is accepted. The config keeps both entries, so removing the wider prefix later leaves the narrower one in force, but the kernel folds them into one interval. `show` dumps the kernel and will print the folded form; `show --output yaml` reads the config and prints what you authored.
>
> A ruleset the kernel would refuse is rejected before anything is written: the CLI errors, and `/etc/solo-provisioner/network-weaver-host-firewall.{yaml,nft}` are left exactly as they were, so the ruleset that replays at boot is always one that loads.

#### Show / Delete the Host Firewall

```bash
# Show the live inet weaver-host-firewall table
sudo solo-provisioner network firewall show

# Show the declarative config the ruleset was rendered from
sudo solo-provisioner network firewall show --output yaml

# Inspect one rule
sudo solo-provisioner network firewall show --name k8s-node

# Delete one named allow rule
sudo solo-provisioner network firewall delete --name k8s-node

# Remove the whole table and its on-disk artifacts
sudo solo-provisioner network firewall delete --all
```

`show --output yaml` prints exactly the schema `create --from-file` accepts, so it round-trips:

```bash
sudo solo-provisioner network firewall show --output yaml > rules.yaml
sudo solo-provisioner network firewall create --from-file rules.yaml --force   # a no-op
```

`--output commands` is the per-rule counterpart, for carrying **one** allow rule to other hosts. It requires `--name` and works on named allow rules only (the reserved blocks are configured by `create`/`set`, not declared):

```bash
sudo solo-provisioner network firewall show --name rudder_server --output commands
```

```
solo-provisioner network firewall create-allow-rule --name rudder_server --proto tcp --icmp-echo
solo-provisioner network firewall add --name rudder_server --cidr 200.201.203.205/32 --port 5309,8443
```

Unlike `show --name <rule> --output yaml` — which is an inspection view, not a config — this sequence is **safe to replay against a host that already has a firewall**. `create-allow-rule` and `add` are additive, so they bring the one rule into existence and leave every other rule untouched. Save it and run it on the target host:

```bash
# on the source host
sudo solo-provisioner network firewall show --name rudder_server --output commands > rudder.sh
# on the target host
sudo sh rudder.sh
```

The emitted lines carry no `sudo` of their own, so the file is a script you run once with privilege rather than a list of individually-escalating commands. Addresses come out in stored order (kept sorted), which is what the host actually has, not the order they were typed in.

> `delete --all` (the default when `--name` is omitted, which is what this verb has always done) removes the table and both `/etc/solo-provisioner/network-weaver-host-firewall.{nft,yaml}`, leaving the host with no weaver-managed firewall — including no management allowlist. It asks for confirmation in an interactive session; pass `--force` to skip the prompt. It does not disable the shared `solo-provisioner-network-nft.service` (shared with `inet weaver-workload-policy`); disable it manually if you need it off.
>
> The reserved blocks cannot be deleted individually — clear their addresses instead (`network firewall set --name mgmt --cidrs "" --force`).

> **`create` and `delete --all` record the enable decision.** Both write it into the host's runtime
> state (`machineState.firewall.disabled`), so `block node reconfigure` agrees with what you did
> here: a firewall you created by hand survives a later reconfigure instead of being torn down, and
> one you deleted here is not re-created by it. A live table always wins over the recorded decision,
> so removing an active host firewall through `block node reconfigure` needs an explicit
> `--firewall-enabled=false`. The membership verbs (`add`, `remove`, `set`) change no decision —
> `reconfigure` reads their result straight out of
> `/etc/solo-provisioner/network-weaver-host-firewall.yaml`, so an urgent
> `add --name mgmt --cidr …` is not reverted by the next reconfigure.

#### Re-apply / Recover the Host Firewall

`reapply` re-renders and re-applies the persisted config without changing it. It takes no arguments — it states no intent, so there is nothing to supply:

```bash
sudo solo-provisioner network firewall reapply
```

Use it to re-assert the table after something else on the host disturbed it, or after recovering the config. Two properties worth knowing:

- It **records no enable/disable decision**, unlike `create` and `delete --all`. A later `block node reconfigure` behaves exactly as if the `reapply` had not run.
- It replaces only the `inet weaver-host-firewall` table. The rendered ruleset scopes its flush to that table, so any third-party nftables table on the host is left alone.

If no config is persisted, `reapply` fails rather than applying a default table — a default-drop policy with an empty management allowlist would lock the host out. Run `create` first.

There is deliberately no way to point `reapply` at a file. To apply a file, that is `create --from-file <path> --force`.

**Recovering a corrupt config.** Every apply retains the generation it replaces:

| Path | Contents |
|------|----------|
| `/etc/solo-provisioner/network-weaver-host-firewall.yaml` | the config currently applied |
| `/etc/solo-provisioner/network-weaver-host-firewall.yaml.prev` | the generation immediately before it |

So a truncated or hand-mangled config is a two-step repair that **keeps the named allow rules**:

```bash
sudo cp /etc/solo-provisioner/network-weaver-host-firewall.yaml.prev \
        /etc/solo-provisioner/network-weaver-host-firewall.yaml
sudo solo-provisioner network firewall reapply
```

Three things this retention deliberately is and is not:

- **One generation deep.** It is a recovery artifact, not a version history — keep history in your own repository, holding the output of `show --output yaml`.
- **Always loadable.** The retained copy is only written when the config it replaces parses, so it is never itself corrupt. This also means recovering from it does not consume it: the bad config is not promoted over the good one.
- **Not the last line of defence.** Without it, a lost config falls through to re-parsing the rendered `.nft`, which recovers the three reserved blocks but **loses every named allow rule**. That fallback still exists; the retained copy is what keeps you from needing it.

`delete --all` removes the retained copy along with the config and the `.nft` artifact.

#### Create a Traffic Policy

The `policy` scope is a generic, category-agnostic primitive that manages the `inet weaver-workload-policy` workload traffic plane: named per-category rules that classify traffic into an HTB priority class, or quarantine a set of CIDRs. It is not tied to any specific node type — the CLI takes CIDRs and class names directly (statusz-agnostic); the examples below use the block-node categories because `block node install` is the only caller today. Each `create` renders the rule(s) into the `inet weaver-workload-policy` forward chain, ensures the policy's nft set `@<name>` exists, writes a per-policy registry file under `/etc/solo-provisioner/policies/`, applies the full chain to the live kernel with `nft -f`, and atomically rewrites `/etc/solo-provisioner/network-weaver-workload-policy.nft`.

`create` is create-if-missing, mirroring `network firewall create`: a policy that already exists is left untouched unless `--force` is passed, in which case its config and membership are **replaced** (not merged) from the given flags/`--cidrs`. Without `--force`, an existing policy warns and makes no changes — even if the flags/`--cidrs` given this time differ from before.

Specify **exactly one** action: `--stamp <class>` (classify into an HTB priority class) or `--deny` (drop). A `--deny` always matches its `@<name>` CIDR set unless `--from-entity world` replaces that with a match on any source; `--ports` adds a listener-port clause on top of either — it does not on its own remove the set match, so `--deny --ports` without `--from-entity world` still needs membership to match anything. A membership `--deny` drops both directions; a port-scoped `--deny` is confined to the pod CIDR and drops the **request leg only**, qualified with `ct direction original` — a listener port sits inside the ephemeral range, so an unqualified drop would also catch the reply leg of an unrelated connection that drew that port as its source port. Combining it with `--from-entity world` locks the port down from every source. There is no `--direction` flag — every class has exactly one direction (see the class list below), so `--stamp <class>` determines it.

```bash
# Publisher: highest-priority ingress class on the publisher listener port
sudo solo-provisioner network policy create --name bn-publisher \
  --ports 40840 --stamp publisher

# Subscriber ingress from any source (no IP-set clause): reserve class
sudo solo-provisioner network policy create --name bn-subscriber-in \
  --ports 40980,40981 --stamp reserve-ingress --from-entity world

# Partner egress (specific destinations), curated CIDR list
sudo solo-provisioner network policy create --name bn-partner-out \
  --ports 40980,40981 --stamp partner --cidrs 10.20.0.0/16

# Backfill egress with an asymmetric reply class (conntrack reply gets higher priority)
sudo solo-provisioner network policy create --name bn-backfill \
  --stamp reserve-egress --reply-stamp backfill-response \
  --cidrs 10.30.5.7:43473

# Quarantine: drop all traffic to/from a set of CIDRs, both directions
sudo solo-provisioner network policy create --name bn-restricted \
  --deny --cidrs 10.99.0.0/16

# Port lockdown: drop inbound connections to a workload listener port, from every source
sudo solo-provisioner network policy create --name bn-health \
  --deny --ports 40983 --from-entity world
```

**Flags**:

| Flag            | Description                                                                                          | Default       |
|-----------------|------------------------------------------------------------------------------------------------------|---------------|
| `--name`        | Policy name; also the nft set name `@<name>` (**required**)                                          | (none)        |
| `--stamp`       | HTB class to classify matching packets into; also fixes the policy's direction (mutually exclusive with `--deny`) | (none) |
| `--deny`        | Drop the `--cidrs` (both directions), the `--ports` (request leg), or their intersection (mutually exclusive with `--stamp`) | `false` |
| `--reply-stamp` | Reply class for an asymmetric conntrack reply (requires `--stamp` to resolve to an egress class; `--reply-stamp` must resolve to the mirror ingress class) | (none) |
| `--from-entity` | `world` — match any source/dest with no IP-set clause (mutually exclusive with `--cidrs`)            | (none)        |
| `--ports`       | Workload listener ports for the match key (comma-separated or repeated)                              | (none)        |
| `--cidrs`       | Initial set membership (comma-separated or repeated); `ip:port` entries for `--reply-stamp` policies | (none)        |
| `--cidrs-file`  | Alternative to `--cidrs`: a file of CIDRs (one per line or comma-separated)                          | (none)        |
| `--pod-cidr`    | Pod CIDR to scope classification to                                                                  | auto-detected |
| `--force`       | Replace an existing policy's config and membership (root flag, `-y`); without it, an existing policy is left untouched | `false` |

`--stamp` references a QoS class name — `publisher`, `reserve-ingress` (ingress); `partner`, `public`, `reserve-egress` (egress); `backfill-response` (ingress, `--reply-stamp` only) — referencing an unknown class is an error. Rule position in the chain is determined by action type and match specificity (deny → reply-restore → specific stamp → fallthrough stamp), never by creation order.

When `--pod-cidr` is omitted it is **auto-detected** from the local node's `.spec.podCIDR` via the Kubernetes API — but only for policies that reference `POD_CIDR`: every `--stamp` policy, and a `--deny` that carries `--ports`. A membership-only `--deny` just drops on set membership, so detection is skipped entirely for it. Unlike `network firewall create`, a `--stamp` policy's detection failure with no `--pod-cidr` is a hard error, not a warning-and-continue. If a `--deny` create's merged chain still includes a `--stamp` sibling that needs `POD_CIDR`, the value is recovered from the existing `/etc/solo-provisioner/network-weaver-workload-policy.nft` instead of being required again — it's a deployment-wide constant, not a per-call argument.


> Set **membership** (the CIDRs) is never persisted to `network-weaver-workload-policy.nft` — statusz is the source of truth and the daemon reconciles it. `--cidrs` seeds the live set only, and only takes effect on a brand-new policy or a `--force` re-create (which replaces membership with exactly what's passed, not a merge with what was live before).

#### Mutate Set Membership (add / remove / set)

Use these verbs to modify a policy's live CIDR set after it has been created. **None of them re-render `network-weaver-workload-policy.nft`** — only the live kernel set changes (§8.3.1).

```bash
# Add one or more CIDRs to a policy's live set (repeatable or comma-separated)
sudo solo-provisioner network policy add --name bn-publisher --cidr 10.1.0.1/32
sudo solo-provisioner network policy add --name bn-publisher --cidr 10.1.0.2/32,10.1.0.3/32

# Remove one or more CIDRs from a policy's live set
sudo solo-provisioner network policy remove --name bn-publisher --cidr 10.1.0.1/32

# Atomically replace the full membership list (flush + re-add in one kernel transaction)
sudo solo-provisioner network policy set --name bn-publisher --cidrs 10.2.0.0/16
# Or clear the set entirely (omit --cidrs):
sudo solo-provisioner network policy set --name bn-publisher
```

**`add` / `remove` flags** (use `--cidr` for each CIDR; comma-separated lists are also accepted):

| Flag     | Description                                        | Required |
|----------|----------------------------------------------------|----------|
| `--name` | Policy name                                        | yes      |
| `--cidr` | CIDR to add or remove (repeatable or comma-separated) | yes   |

**`set` flags**:

| Flag           | Description                                                             | Required |
|----------------|-------------------------------------------------------------------------|----------|
| `--name`       | Policy name                                                             | yes      |
| `--cidrs`      | Replacement membership (comma-separated or repeated); omit to clear     | no       |
| `--cidrs-file` | Alternative to `--cidrs`: a file of CIDRs (one per line or comma-separated) | no  |

For `--reply-stamp` policies the CIDR entries must be `ip:port` pairs for all three verbs, same as `create --cidrs`.

#### Inspect a Policy (show)

Print a policy's registry config and current live set membership. Without `--name`, `show` lists **all** configured policies (sorted by name); with `--name`, it prints just that one. This mirrors `network shape show`, where a bare `show` lists everything and flags narrow the scope.

```bash
# List every configured policy
sudo solo-provisioner network policy show

# Inspect a single policy
sudo solo-provisioner network policy show --name bn-publisher
```

Output example (single policy). `direction` leads, and the live set is nested under the policy:
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

With no policies configured, a bare `show` prints `no policies configured` rather than failing.

| Flag     | Description                              | Required |
|----------|------------------------------------------|----------|
| `--name` | Policy name (omit to list all policies)  | no       |

#### Remove a Policy (delete)

Remove a policy's rules, set, and registry file, and re-render the `inet weaver-workload-policy` chain:

```bash
sudo solo-provisioner network policy delete --name bn-restricted
```

`delete` re-renders the full chain without the removed policy, snapshots and restores remaining policies' live membership (so the destructive `delete table; add table` does not wipe their sets), removes the registry file, and atomically overwrites `network-weaver-workload-policy.nft`. If this is the last policy, the table is torn down entirely (live and on disk); the boot oneshot stays enabled.

| Flag     | Description     | Required |
|----------|-----------------|----------|
| `--name` | Policy name     | yes      |

---

#### `network shape` — tc HTB Bandwidth Class Management

Manage the tc HTB shaping hierarchy for the node's egress NIC. Each `create/set/delete` mutation atomically re-renders `/usr/local/sbin/solo-provisioner-bandwidth-shaper.sh` and restarts `solo-provisioner-bandwidth-shaper.service` so the live kernel and boot script stay in sync.

`block node install` drives the shape registry automatically (from `--link-rate`); `network shape` lets you inspect or adjust individual classes after install.

**Create device root**

```bash
# Explicit trunk rate: written into the boot script as concrete tc values; no sysfs detection.
sudo solo-provisioner network shape create \
  --device egress --rate 1gbit --default reserve-egress

# Auto-detect trunk rate NOW from sysfs (/sys/class/net/<NIC>/speed) and store
# the resolved value (e.g. 1gbit) as an explicit rate. Unreadable speed → 1gbit.
sudo solo-provisioner network shape create \
  --device egress --rate auto --default reserve-egress

# Replace an existing device config.
sudo solo-provisioner network shape create \
  --device egress --rate 1gbit --default reserve-egress --force
```

`--rate auto` reads the detected NIC's link speed from sysfs **at create time** — while the link is up and stable — and stores the resolved value (e.g. `1gbit`) as an ordinary explicit rate. `network shape show` then reports the concrete number and the boot script carries explicit values, with no `SPEED` variable and no sysfs read at boot. If the speed is not readable then (e.g. a virtual NIC reporting `-1`), `auto` falls back to a concrete `1gbit` (1000 Mbit) — still explicit and `SPEED`-free, not a dynamic script. Either way, `--rate auto` always produces a concrete stored rate; the sysfs-at-boot form only appears when no shape device is configured at all (e.g. `block node install` run without `--link-rate` in non-interactive mode).

Until you add the first `--class`, the device root renders a placeholder hierarchy (the three default egress classes at proportional rates: partner 40%/70%, public 30%/70%, reserve-egress 30%/100% of the trunk) at the resolved concrete rate. Adding explicit `--class` configs replaces the placeholder.

**Create / replace HTB leaf classes**

```bash
sudo solo-provisioner network shape create --class partner        --rate 400mbit --ceil 700mbit  --prio 0
sudo solo-provisioner network shape create --class public         --rate 300mbit --ceil 700mbit  --prio 5
sudo solo-provisioner network shape create --class reserve-egress --rate 300mbit --ceil 1000mbit --prio 1
```

Once all three egress class configs are present, the boot script switches to fully explicit rates (no SPEED variable at all).

**Live update (no qdisc teardown)**

```bash
sudo solo-provisioner network shape set --class partner --rate 500mbit
sudo solo-provisioner network shape set --class public  --ceil 600mbit
```

`set` runs `tc class change` on the live kernel and re-renders the boot script immediately.

**Show**

```bash
sudo solo-provisioner network shape show              # all devices and classes
sudo solo-provisioner network shape show --class partner  # single class
```

`show` reports the **stored** configuration (rate/ceil/prio). To see **live** traffic flowing through the classes, use `watch`.

**Watch (live counters, read-only)**

```bash
# Watch the egress NIC every 2s — both --device and --iface are required.
# Runs until Ctrl-C.
sudo solo-provisioner network shape watch --device egress --iface enp0s1

# Narrow to a single class, faster sampling, bounded to 5 samples then exit.
sudo solo-provisioner network shape watch --device egress --iface enp0s1 --class partner --interval 1s --count 5

# Watch a block node's ingress veth (find it with `ip link` or `tc qdisc show`).
sudo solo-provisioner network shape watch --device ingress --iface lxc1a2b3c
```

`watch` samples `tc -s class show dev <iface>` at `--interval` and prints, per class, the throughput (from the byte delta) plus the change in overlimits and drops since the previous sample — "rate over time". It is read-only: it never mutates tc or the shape registry. Use it to confirm traffic is actually being classified and shaped — e.g. partner traffic landing in `1:40` with a non-zero rate and climbing overlimits. Both `--device` (egress or ingress — selects the class set) and `--iface` (the interface to sample) are **required**: the command does no environment probing — no NIC or veth auto-detection — so it stays independent of any running block node. For egress, `--iface` is the physical NIC (e.g. `enp0s1`); for ingress, the per-pod host veth (e.g. `lxc1a2b3c`), which you can find with `ip link` or `tc qdisc show`. `--class` narrows the output to one class within `--device`. This complements the Prometheus counters, which target dashboards rather than an operator at a terminal.

**Delete**

```bash
# Fails if the class is the device default or referenced by a policy --stamp.
sudo solo-provisioner network shape delete --class reserve-egress
```

| Flag        | Description                                                                     | Required              |
|-------------|---------------------------------------------------------------------------------|-----------------------|
| `--device`  | Traffic direction: `egress` or `ingress`                                        | one of `--device` / `--class` |
| `--class`   | HTB class name (`partner`, `public`, `reserve-egress`, …)                      | one of `--device` / `--class` |
| `--rate`    | Bandwidth rate (`100mbit`, `1gbit`) or `"auto"` (sysfs, `--device` form only)  | yes (create/set)      |
| `--ceil`    | Burst ceiling ≥ `--rate`; defaults to `--rate` when omitted                     | no                    |
| `--prio`    | HTB scheduling priority `[0,7]`; 0 is highest                                   | no (default 0)        |
| `--default` | Default class for unmatched traffic (`--device` form only)                      | yes (`--device`)      |
| `--force`   | Replace an existing device or class config                                       | no                    |
| `--iface`   | Interface to sample (`watch`); **required** — e.g. `enp0s1` (egress) or `lxc1a2b3c` (ingress veth). No auto-detection | `watch`               |
| `--interval`| Sampling interval for `watch` (e.g. `1s`, `500ms`); default `2s`                 | no                    |
| `--count`   | Number of `watch` samples to print then exit; `0` = run until interrupted        | no                    |

---

### Teleport Commands

Configure secure access using Teleport agents.

#### Install Node Agent (SSH Access)

Install the Teleport node agent for secure SSH access to the host:

```bash
# Install with required token and proxy address
sudo solo-provisioner teleport node install \
  --token=<join-token> \
  --proxy=proxy.teleport.example.com:443

# With error handling
sudo solo-provisioner teleport node install \
  --token=<join-token> \
  --proxy=proxy.teleport.example.com \
  --stop-on-error
```

**Required Flags**:

| Flag      | Description                        |
|-----------|------------------------------------|
| `--token` | Join token for Teleport agent      |
| `--proxy` | Teleport proxy address (host:port) |

#### Install Cluster Agent (kubectl Access)

Install the Teleport Kubernetes cluster agent for secure kubectl access:

```bash
# Install with values file
sudo solo-provisioner teleport cluster install \
  --values=/path/to/teleport-values.yaml

# With specific version
sudo solo-provisioner teleport cluster install \
  --values=/path/to/teleport-values.yaml \
  --version=16.0.0
```

**Required Flags**:

| Flag       | Description                       |
|------------|-----------------------------------|
| `--values` | Path to Teleport Helm values file |

**Optional Flags**:

| Flag        | Description                 |
|-------------|-----------------------------|
| `--version` | Teleport Helm chart version |

#### Uninstall Node Agent

Remove the Teleport node agent, stopping the systemd service and cleaning up binaries and configuration:

```bash
sudo solo-provisioner teleport node uninstall
```

#### Uninstall Cluster Agent

Remove the Teleport Kubernetes cluster agent Helm release:

```bash
sudo solo-provisioner teleport cluster uninstall
```

---

### External Secrets Operator (ESO) Commands

Manage the External Secrets Operator (ESO), which syncs secrets from external stores into Kubernetes. Installing ESO is the prerequisite for syncing secrets used by other components (e.g. the `grafana-alloy-secrets` Secret consumed by `alloy cluster install`).

#### Prerequisites

| Prerequisite              | Description                                            |
|---------------------------|--------------------------------------------------------|
| **Root privileges**       | The install command requires `sudo`                    |
| **Reachable K8s cluster** | The cluster must be reachable via the admin kubeconfig |

#### Install External Secrets Operator

Install the `external-secrets/external-secrets` Helm chart into the cluster. The command is idempotent: if ESO is already installed in the target namespace, installation is skipped with a clear message. The chart version is pinned by the infrastructure catalog.

```bash
# Install with defaults (namespace: external-secrets)
sudo solo-provisioner eso operator install

# Install into a custom namespace
sudo solo-provisioner eso operator install --namespace my-eso
```

**Additional Flags**:

| Flag              | Default            | Description                                                                                                          |
|-------------------|--------------------|----------------------------------------------------------------------------------------------------------------------|
| `--namespace`     | `external-secrets` | Kubernetes namespace for the External Secrets Operator                                                               |

#### Uninstall External Secrets Operator

Uninstall the ESO Helm release. The command is idempotent: if ESO is not installed in the target namespace, uninstallation is skipped with a clear message.

> **Warning:** uninstalling ESO removes its cluster-scoped CRDs, which deletes every `ExternalSecret`/`SecretStore` resource in the cluster (and the Kubernetes Secrets they sync). Do not run this while other components still rely on synced secrets.

```bash
# Uninstall from the default namespace (external-secrets)
sudo solo-provisioner eso operator uninstall

# Uninstall from a custom namespace
sudo solo-provisioner eso operator uninstall --namespace my-eso
```

**Additional Flags**:

| Flag          | Default            | Description                                                        |
|---------------|--------------------|--------------------------------------------------------------------|
| `--namespace` | `external-secrets` | Kubernetes namespace for the External Secrets Operator |

#### Create an ExternalSecret

Create (or update) an `ExternalSecret` that ESO reconciles into a Kubernetes Secret. The command uses server-side apply, so re-running it with the same `--name`/`--namespace` updates the existing `ExternalSecret` in place.

> **Prerequisite:** a `ClusterSecretStore` named by `--store` and the target `--namespace` must both already exist in the cluster (the store points to your secrets backend — Vault, AWS, GCP, …). Setting up the `ClusterSecretStore` is cluster-operator configuration and is out of scope for this command.

```bash
sudo solo-provisioner eso secret create \
  --store=vault-store \
  --name=grafana-alloy-secrets \
  --namespace=grafana-alloy \
  --set PROMETHEUS_PASSWORD_PRIMARY=secret/data/grafana/alloy/prod/prometheus/primary#password \
  --set LOKI_PASSWORD_PRIMARY=secret/data/grafana/alloy/prod/loki/primary#password
```

**Additional Flags**:

| Flag                 | Default | Description                                                                             |
|----------------------|---------|-----------------------------------------------------------------------------------------|
| `--store`            | —       | **Required.** Name of the `ClusterSecretStore` resource to sync from                    |
| `--name`             | —       | **Required.** Name of the resulting Kubernetes Secret (and the ExternalSecret)          |
| `--namespace`        | —       | **Required.** Namespace for both the ExternalSecret and the Kubernetes Secret           |
| `--set`              | —       | Repeatable. Map a Secret key to a store path: `KEY=store/path[#field]`. At least one required |
| `--refresh-interval` | `1h`    | How often ESO re-syncs the secret from the store                                        |

---

### Alloy Commands

Manage Grafana Alloy observability stack for metrics and logs.

#### Prerequisites

When installing Alloy with remote endpoints (`--add-prometheus-remote` or `--add-loki-remote`), ensure the following
prerequisites are met:

| Prerequisite                   | Description                                                                                                                                                                                                |
|--------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **Running Kubernetes Cluster** | A cluster must be set up via `solo-provisioner block node install` or `solo-provisioner kube cluster install`                                                                                              |
| **K8s Secret**                 | A Kubernetes Secret named `grafana-alloy-secrets` must exist in the `grafana-alloy` namespace with password keys for each configured remote (e.g., `PROMETHEUS_PASSWORD_PRIMARY`, `LOKI_PASSWORD_PRIMARY`) |
| **Reachable Remote Endpoints** | Prometheus/Loki URLs must be reachable from the cluster                                                                                                                                                    |
| **Block Node (optional)**      | If using `--monitor-block-node`, the block node must be installed first                                                                                                                                    |

> **Note**: Without `--add-prometheus-remote` or `--add-loki-remote` flags, Alloy installs without remotes and does not
> require the K8s secret.

#### Install Alloy Stack

```bash
# Step 1: Create the K8s secret with passwords for remote endpoints
kubectl create namespace grafana-alloy
kubectl create secret generic grafana-alloy-secrets \
  --namespace=grafana-alloy \
  --from-literal=PROMETHEUS_PASSWORD_PRIMARY=<password> \
  --from-literal=LOKI_PASSWORD_PRIMARY=<password>

# Step 2: Install Alloy with remotes
sudo solo-provisioner alloy cluster install \
  --cluster-name=mainnet-block-01 \
  --profile=mainnet \
  --add-prometheus-remote=name=primary,url=https://prom1.example.com/api/v1/write,username=user1 \
  --add-loki-remote=name=primary,url=https://loki1.example.com/loki/api/v1/push,username=user1 \
  --monitor-block-node

# Install Alloy without remotes (no secret needed)
sudo solo-provisioner alloy cluster install \
  --cluster-name=mainnet-block-01
```

**Available Flags**:

| Flag                      | Description                                                                                                                       |
|---------------------------|-----------------------------------------------------------------------------------------------------------------------------------|
| `--cluster-name`          | Cluster name for metrics/logs labels                                                                                              |
| `--profile`, `-p`         | Deployment profile (`local`, `perfnet`, `testnet`, `previewnet`, `mainnet`); sets the `environment` label for the `ops` profile. Optional; if omitted, the value from `--config` is used |
| `--monitor-block-node`    | Enable Block Node specific monitoring                                                                                             |
| `--add-prometheus-remote` | Add a Prometheus remote (format: `name=<name>,url=<url>,username=<username>[,labelProfile=eng\|ops]`). Repeatable. Default: `eng` |
| `--add-loki-remote`       | Add a Loki remote (format: `name=<name>,url=<url>,username=<username>[,labelProfile=eng\|ops]`). Repeatable. Default: `eng`       |
| `--prometheus-url`        | Prometheus remote write URL *(deprecated: use `--add-prometheus-remote`)*                                                         |
| `--prometheus-username`   | Prometheus authentication username *(deprecated)*                                                                                 |
| `--loki-url`              | Loki remote write URL *(deprecated: use `--add-loki-remote`)*                                                                     |
| `--loki-username`         | Loki authentication username *(deprecated)*                                                                                       |
| `--stop-on-error`         | Stop execution on first error (default behavior when no execution-mode flag is set)                                             |
| `--rollback-on-error`     | Rollback executed steps on error                                                                                                 |
| `--continue-on-error`     | Continue executing steps even if some steps fail                                                                                 |

> **Note**: `--stop-on-error`, `--rollback-on-error`, and `--continue-on-error` are mutually exclusive and apply to
> both `alloy cluster install` and `alloy cluster uninstall`. When Alloy fails to install because the Kubernetes
> cluster is not reachable, install the cluster first with `solo-provisioner kube cluster install` — Alloy deploys
> into an existing cluster and does not create one.

> **Note**: Passwords must be pre-created in a K8s Secret named `grafana-alloy-secrets` in the `grafana-alloy`
> namespace. The secret can be created manually, via ESO, Terraform, or any other mechanism.

#### Multiple Remote Endpoints

The `--add-prometheus-remote` and `--add-loki-remote` flags use the format
`name=<name>,url=<url>,username=<username>[,labelProfile=<profile>]`:

- **name**: Unique identifier for the remote (e.g., `primary`, `backup`, `grafana-cloud`)
- **url**: The remote write endpoint URL
- **username**: Authentication username (password is read from the K8s Secret)
- **labelProfile** *(optional)*: Label profile to auto-inject additional labels (default: `eng`, which adds only
  `cluster`). See [Label Profiles](#label-profiles) below

**K8s Secret Keys** (for multiple remotes):

Each remote requires a corresponding password key in the `grafana-alloy-secrets` K8s Secret. The key name is derived
from the remote type and name:

- Prometheus: `PROMETHEUS_PASSWORD_<REMOTE_NAME>`
- Loki: `LOKI_PASSWORD_<REMOTE_NAME>`

Example for a cluster with `primary` and `backup` remotes, create the secret with:

```bash
kubectl create namespace grafana-alloy
kubectl create secret generic grafana-alloy-secrets \
  --namespace=grafana-alloy \
  --from-literal=PROMETHEUS_PASSWORD_PRIMARY=<password> \
  --from-literal=PROMETHEUS_PASSWORD_BACKUP=<password> \
  --from-literal=LOKI_PASSWORD_PRIMARY=<password> \
  --from-literal=LOKI_PASSWORD_BACKUP=<password>
```

#### Managing Remote Endpoints

The `alloy cluster install` command is **declarative** - it replaces the entire remote configuration with what you
specify. To manage endpoints:

**Add a new remote:** Include all existing remotes plus the new one:

```bash
# If you had 'primary', and want to add 'backup':
sudo solo-provisioner alloy cluster install \
  --cluster-name=mainnet-block-01 \
  --add-prometheus-remote=name=primary,url=https://prom1.example.com/api/v1/write,username=user1 \
  --add-prometheus-remote=name=backup,url=https://prom2.example.com/api/v1/write,username=user2 \
  --add-loki-remote=name=primary,url=https://loki1.example.com/loki/api/v1/push,username=user1
```

**Remove a remote:** Simply omit it from the command:

```bash
# Remove 'backup', keep only 'primary':
sudo solo-provisioner alloy cluster install \
  --cluster-name=mainnet-block-01 \
  --add-prometheus-remote=name=primary,url=https://prom1.example.com/api/v1/write,username=user1 \
  --add-loki-remote=name=primary,url=https://loki1.example.com/loki/api/v1/push,username=user1
```

**Modify a remote URL:** Specify the same name with the new URL:

```bash
# Change 'primary' Prometheus URL:
sudo solo-provisioner alloy cluster install \
  --cluster-name=mainnet-block-01 \
  --add-prometheus-remote=name=primary,url=https://new-prom.example.com/api/v1/write,username=user1 \
  --add-loki-remote=name=primary,url=https://loki1.example.com/loki/api/v1/push,username=user1
```

**Remove all remotes (install without remotes):**

```bash
sudo solo-provisioner alloy cluster install \
  --cluster-name=mainnet-block-01
```

> **Important:** Each run replaces the previous remote configuration. Always specify all the remotes you want to keep.

#### Label Profiles

Label profiles auto-inject additional labels into every metric and log stream. The optional `labelProfile` key on any
remote activates a profile.

**Available profiles:**

| Profile           | Labels Added                                                                 |
|-------------------|------------------------------------------------------------------------------|
| `eng` *(default)* | `cluster`                                                                    |
| `ops`             | `cluster`, `environment`, `instance`, `instance_type`, `inventory_name`, `ip` (optional) |

**Example** — install with the `ops` label profile:

```bash
sudo solo-provisioner alloy cluster install \
  --cluster-name=lfh02-previewnet-blocknode \
  --add-prometheus-remote=name=primary,url=https://prom.example.com/api/v1/write,username=user1,labelProfile=ops \
  --add-loki-remote=name=primary,url=https://loki.example.com/loki/api/v1/push,username=user1,labelProfile=ops \
  --monitor-block-node
```

With `--cluster-name=lfh02-previewnet-blocknode` and `--profile=previewnet`, the `ops` profile derives:

| Label            | Value                        | Source                                                           |
|------------------|------------------------------|------------------------------------------------------------------|
| `cluster`        | `lfh02-previewnet-blocknode` | Always set (from `--cluster-name`)                               |
| `environment`    | `previewnet`                 | From `--profile` (deploy profile)                                |
| `instance`       | `lfh02-previewnet-blocknode` | Full cluster name; overrides the auto-scraped `IP:port`          |
| `instance_type`  | `lfh`                        | Alphabetic prefix of the first segment of cluster name           |
| `inventory_name` | `lfh02-previewnet-blocknode` | Full cluster name                                                |
| `ip`             | `<ip>`                       | Optional; set when an IP address label is available for the node |

> **Note:** If `labelProfile` is omitted for a given remote, that remote uses the default `eng` profile (only the
`cluster` label). Each remote can specify its own `labelProfile`.

#### Uninstall Alloy Stack

```bash
sudo solo-provisioner alloy cluster uninstall
```

### Daemon Service Commands

Manage the `solo-provisioner-daemon` systemd service that coordinates consensus-node upgrade handoffs as well as other
automation requirements.

#### Prerequisites

| Prerequisite              | Description                                            |
|---------------------------|--------------------------------------------------------|
| **Root privileges**       | All daemon service commands require `sudo`             |
| **Reachable K8s cluster** | The cluster must be reachable via the admin kubeconfig |

#### Install Daemon Service

Bootstrap `daemon.yaml`, provision K8s RBAC, generate the daemon kubeconfig, and
start the systemd service in one step.

```bash
# Interactive install — prompts for components, cn-node-id, and cn-orbit when daemon.yaml is absent
sudo solo-provisioner daemon service install

# Enable consensus-node only (non-interactive / CI)
sudo solo-provisioner daemon service install \
  --components=consensus-node --cn-node-id=0.0.3 --cn-orbit=hedera-network

# Override the CN upgrade staging directory
sudo solo-provisioner daemon service install \
  --components=consensus-node --cn-node-id=0.0.3 --cn-orbit=hedera-network \
  --cn-upgrade-dir=/custom/path/data/upgrade/current

# Copy a pre-built daemon.yaml into place, then run the workflow
sudo solo-provisioner daemon service install --from-config=/path/to/daemon.yaml
```

**Additional Flags**

| Flag                | Default                                                       | Description                                                                                              |
|---------------------|---------------------------------------------------------------|----------------------------------------------------------------------------------------------------------|
| `--components`      | _(prompted)_                                                  | Comma-separated list of components to enable: `consensus-node`, `block-node`                             |
| `--cn-node-id`      | _(prompted)_                                                  | Hedera node identifier for the consensus node (e.g. `0.0.3`)                                            |
| `--cn-orbit`        | _(prompted)_                                                  | Kubernetes namespace (orbit) where consensus-node `NetworkUpgradeExecute` CRs are watched                |
| `--cn-upgrade-dir`  | `/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current` | Path to the consensus-node upgrade staging directory                                                    |
| `--bn-orbit`        | _(prompted)_                                                  | Kubernetes namespace (orbit) for the block-node component _(supported in a future release)_              |
| `--from-config`     | _(none)_                                                      | Path to an existing `daemon.yaml` to copy into `/opt/solo/weaver/config/daemon.yaml`                     |

> **Config bootstrap logic:** If `daemon.yaml` already exists its values are used.
> Individual fields can still be overridden with the component-scoped flags above.
> In interactive mode the prompts are pre-filled with any existing values so pressing
> Enter accepts them unchanged.
>
> **Adding or removing components:** Run `daemon service uninstall` first, then
> re-run `daemon service install` with the updated `--components` list. At least
> one component must be selected — RBAC and kubeconfigs are only provisioned for
> the chosen components.

#### Uninstall Daemon Service

```bash
sudo solo-provisioner daemon service uninstall
```

#### Check Daemon Service Health

Prints the full `/status` response (per-component monitor state, connectivity errors, and prerequisite
probe failures). `status` is an alias for `check`.

```bash
sudo solo-provisioner daemon service check
# or, equivalently:
sudo solo-provisioner daemon service status
```

#### Start / Stop Daemon Service

```bash
sudo solo-provisioner daemon service start
sudo solo-provisioner daemon service stop
```

---

### Consensus Migration Soak Commands

Drive the consensus-node migration **soak watcher** that runs inside `solo-provisioner-daemon`. These
commands talk to the running daemon over its Unix socket; the daemon must already be installed and
running (see [Daemon Service Commands](#daemon-service-commands)). Soak lifecycle lives under
`consensus migration soak`, separate from the `daemon service` tree (which is scoped to daemon lifecycle
only).

#### Start a Soak

```bash
sudo solo-provisioner consensus migration soak start \
  --node-id=0.0.3 \
  --cutover-ts=2025-09-01T00:00:00Z \
  --migration-plan=/path/to/migration-plan.yaml
```

**Required Flags**:

| Flag               | Description                                                        |
|--------------------|--------------------------------------------------------------------|
| `--node-id`        | Consensus node ID                                                  |
| `--cutover-ts`     | Cutover timestamp in RFC-3339 format (e.g. `2025-09-01T00:00:00Z`) |
| `--migration-plan` | Path to the migration plan file on the host                        |

#### Stop a Soak

```bash
# Stop and delete state (clean stop — daemon will NOT auto-resume)
sudo solo-provisioner consensus migration soak stop

# Stop but preserve elapsed soak time (daemon WILL auto-resume on next restart)
sudo solo-provisioner consensus migration soak stop --keep-state
```

**Additional Flags**:

| Flag           | Description                                                                       | Default |
|----------------|-----------------------------------------------------------------------------------|---------|
| `--keep-state` | Preserve `cutover-state.jsonl` so the daemon resumes the soak on its next restart | `false` |

#### Show Soak Status

```bash
sudo solo-provisioner consensus migration soak status
```

---

### Utility Commands

#### Show Version

```bash
# Default human-readable text output
solo-provisioner version

# JSON output
solo-provisioner version --output=json

# Short flag
solo-provisioner -v
```

---

## Configuration

### Configuration File

Solo Provisioner supports YAML configuration files with the `--config` flag:

```yaml
# config.yaml
log:
  level: debug           # Log level: debug, info, warn, error
  consoleLogging: true   # Enable console output
  fileLogging: false     # Enable file logging

blockNode:
  namespace: "block-node"
  release: "block-node"
  chart: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
  version: "0.22.1"
  storage:
    basePath: "/mnt/fast-storage"
    archivePath: ""       # Optional: defaults to basePath/archive
    livePath: ""          # Optional: defaults to basePath/live
    logPath: ""           # Optional: defaults to basePath/log
    liveSize: "10Gi"
    archiveSize: "100Gi"
    logSize: "5Gi"

alloy:
  monitorBlockNode: true
  clusterName: "mainnet-block-01"
  prometheusRemotes:
    - name: "primary"
      url: "https://prometheus.example.com/api/v1/write"
      username: "metrics"
      labelProfile: "ops"    # Optional: auto-inject additional labels
  lokiRemotes:
    - name: "primary"
      url: "https://loki.example.com/loki/api/v1/push"
      username: "logs"
      labelProfile: "ops"    # Optional: auto-inject additional labels

teleport:
  version: "16.0.0"
  valuesFile: "/path/to/teleport-values.yaml"
  nodeAgentToken: ""      # Set via flag for security
  nodeAgentProxyAddr: "proxy.teleport.example.com:443"

proxy:
  enabled: false                # Set to true to route traffic through a proxy
  url: "127.0.0.1:3128"        # Proxy address as host:port
  sslCertFile: "/etc/ssl/certs/ca-certificates.crt"
  containerRegistryProxy: "localhost:5050"
```

### Configuration Precedence

Solo Provisioner uses this precedence order (highest to lowest):

1. Command-line flags
2. Environment variables (when using `--config`)
3. Configuration file
4. Built-in defaults

### Proxy Configuration

Solo Provisioner supports routing all network traffic through an HTTP/HTTPS proxy. This is useful for:

- **Caching**: Speed up repeated deployments by caching binary downloads and container images through a local proxy
- **Security**: Route traffic through a corporate proxy for auditing, filtering, or compliance requirements
- **Air-gapped environments**: Use a proxy to reach external registries from restricted networks

To enable proxy support, add a `proxy` section to your config file:

```yaml
proxy:
  enabled: true
  url: "127.0.0.1:3128"
  sslCertFile: "/etc/ssl/certs/ca-certificates.crt"
  containerRegistryProxy: "localhost:5050"
```

| Field                    | Description                                                                              |
|--------------------------|------------------------------------------------------------------------------------------|
| `enabled`                | Enable proxy mode                                                                        |
| `url`                    | Proxy address as `host:port` (sets both `HTTP_PROXY` and `HTTPS_PROXY`)                  |
| `noProxy`                | Comma-separated hosts/CIDRs to bypass proxy (defaults to localhost and private networks) |
| `sslCertFile`            | CA certificate bundle path for TLS verification (sets `SSL_CERT_FILE`)                   |
| `containerRegistryProxy` | Container image pull-through cache as `host:port` (configures CRI-O registry mirror)     |

When proxy is enabled, Solo Provisioner sets the appropriate environment variables so that all HTTP clients and Helm
operations automatically route through the proxy. The `sslCertFile` allows trusting custom CA certificates (e.g., for
MITM proxy inspection) without disabling TLS verification.

### Environment Variables

Environment variables can override configuration file values. They require a config file to be provided via `--config`
flag.

**Format**: `SOLO_PROVISIONER_<SECTION>_<FIELD>` (uppercase, underscores for nested fields)

```bash
# Override block node storage base path
export SOLO_PROVISIONER_BLOCKNODE_STORAGE_BASEPATH=/data/block-node

# Override block node namespace
export SOLO_PROVISIONER_BLOCKNODE_NAMESPACE=my-block-node

# Then run with a config file
sudo solo-provisioner block node install --profile=mainnet --config=/etc/solo-provisioner/config.yaml
```

---

## Workflow Examples

### Complete Block Node Deployment (Production)

```bash
# Step 1: Deploy the block node (includes preflight checks and K8s setup)
sudo solo-provisioner block node install \
  --profile=mainnet \
  --config=/etc/solo-provisioner/config.yaml \
  --values=/etc/solo-provisioner/block-node-values.yaml

# Step 2: (Optional) Set up secure SSH access
sudo solo-provisioner teleport node install \
  --token=$TELEPORT_JOIN_TOKEN \
  --proxy=teleport.hedera.com:443

# Step 3: (Optional) Set up secure kubectl access
sudo solo-provisioner teleport cluster install \
  --values=/etc/solo-provisioner/teleport-kube-values.yaml

# Step 4: (Optional) Set up monitoring
sudo solo-provisioner alloy cluster install \
  --monitor-block-node \
  --cluster-name=mainnet-block-01 \
  --add-prometheus-remote=name=primary,url=https://metrics.hedera.internal/write,username=block-metrics \
  --add-loki-remote=name=primary,url=https://loki.hedera.internal/loki/api/v1/push,username=block-logs
```

### Development Environment Setup

```bash
# Quick local setup for development
sudo solo-provisioner block node install --profile=local

# Verify deployment
kubectl get pods -n block-node
```

### Upgrade Workflow

```bash
# Step 1: Prepare new values file with updated config

# Step 2: Perform upgrade
sudo solo-provisioner block node upgrade \
  --profile=mainnet \
  --values=/etc/solo-provisioner/block-node-values-v2.yaml \
  --chart-version=0.24.0

# Step 3: Verify
kubectl get pods -n block-node
```

### Clean Teardown

```bash
# Remove Teleport agents (if installed)
sudo solo-provisioner teleport cluster uninstall
sudo solo-provisioner teleport node uninstall

# Remove Alloy monitoring
sudo solo-provisioner alloy cluster uninstall

# Remove Kubernetes cluster (removes block node)
sudo solo-provisioner kube cluster uninstall

# Uninstall Solo Provisioner itself (--yes is required)
sudo solo-provisioner uninstall --yes
```

---

## Troubleshooting

### Common Issues

**1. Permission Denied**

```bash
# Most commands require root privileges
sudo solo-provisioner block node install --profile=local
```

**2. Profile Not Specified**

```bash
# Error: profile flag is required
# Solution: Always specify --profile
sudo solo-provisioner block node check --profile=mainnet
```

**3. Invalid Storage Path**

```bash
# Error: invalid base path
# Ensure path exists and has correct permissions
sudo mkdir -p /mnt/storage
sudo solo-provisioner block node install --profile=mainnet --base-path=/mnt/storage
```

**4. Helm Chart Issues**

```bash
# Check specific chart version availability
# Use explicit version if needed
sudo solo-provisioner block node install \
  --profile=mainnet \
  --chart-version=0.22.1
```

### Getting Help

```bash
# General help
solo-provisioner --help

# Command-specific help
solo-provisioner block --help
solo-provisioner block node --help
solo-provisioner block node install --help
```

### Debug Output

Enable debug logging in your config file:

```yaml
log:
  level: debug
  consoleLogging: true
```

---

## Quick Reference Card

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
sudo solo-provisioner block node uninstall   --profile=<profile> [--with-reset]

# KUBERNETES
sudo solo-provisioner kube cluster install
sudo solo-provisioner kube cluster uninstall

# TELEPORT
sudo solo-provisioner teleport node install    --token=<token> --proxy=<addr>
sudo solo-provisioner teleport node uninstall
sudo solo-provisioner teleport cluster install --values=<file>
sudo solo-provisioner teleport cluster uninstall

# EXTERNAL SECRETS OPERATOR (ESO)
sudo solo-provisioner eso operator install    [--namespace=<ns>]
sudo solo-provisioner eso operator uninstall  [--namespace=<ns>]
sudo solo-provisioner eso secret create       --store=<name> --name=<secret> --namespace=<ns> --set KEY=store/path[#field] [--refresh-interval=<interval>]

# ALLOY
sudo solo-provisioner alloy cluster install   [--monitor-block-node] [--cluster-name=<name>]
sudo solo-provisioner alloy cluster uninstall

# DAEMON
sudo solo-provisioner daemon service install [--components=<list>] [--cn-node-id=<id>] [--cn-orbit=<ns>] [--cn-upgrade-dir=<path>]
sudo solo-provisioner daemon service install --from-config=<path>
sudo solo-provisioner daemon service uninstall
sudo solo-provisioner daemon service check          # alias: status
sudo solo-provisioner daemon service start
sudo solo-provisioner daemon service stop

# CONSENSUS MIGRATION SOAK
sudo solo-provisioner consensus migration soak start  --node-id=<id> --cutover-ts=<RFC-3339> --migration-plan=<path>
sudo solo-provisioner consensus migration soak stop   [--keep-state]
sudo solo-provisioner consensus migration soak status

# UTILITIES
solo-provisioner version [--output=text|json]
solo-provisioner --help
```

---

*Document Version: 1.3.0 | Last Updated: June 2026*

