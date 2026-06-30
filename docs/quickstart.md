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
sudo solo-provisioner uninstall
```

---

## Global Flags

These flags are available for all commands:

| Flag                | Short | Description                                              | Default                    |
|---------------------|-------|----------------------------------------------------------|----------------------------|
| `--config`          | `-c`  | Path to configuration file                               | None                       |
| `--profile`         | `-p`  | Deployment profile                                       | Required for most commands |
| `--output`          | `-o`  | Output format (yaml\|json)                               | `yaml`                     |
| `--non-interactive` | —     | Disable TUI and output raw logs; useful for CI/pipelines | `false`                    |
| `--version`         | `-v`  | Show version                                             | -                          |
| `--help`            | `-h`  | Show help                                                | -                          |

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
```

**What it checks**:

- System requirements (CPU, memory, disk)
- Required dependencies
- Network connectivity
- Storage availability

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
| `--plugin-preset`         | Plugin preset to deploy (`tier1-lfh`, `tier1-rfh`, or `custom`); prompts interactively when omitted                                   |
| `--plugins`               | Comma-separated plugin list; overrides `--plugin-preset` when set                                                                     |
| `--plugins-size`          | PV/PVC size for plugins storage (e.g., `5Gi`, `10Gi`)                                                                                 |
| `--plugins-path`          | Path for plugins storage                                                                                                              |
| `--historic-retention`    | Historic block retention threshold (`0` = unlimited)                                                                                  |
| `--recent-retention`      | Recent block retention threshold (default: `96000`)                                                                                   |
| `--load-balancer-enabled` | Inject MetalLB address-pool annotation into the block node service; set to `false` for environments without MetalLB (default: `true`) |

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
```

**Additional Flags**:

| Flag                | Description                                                                                           | Default |
|---------------------|-------------------------------------------------------------------------------------------------------|---------|
| `--no-reuse-values` | Don't reuse previous release values                                                                   | `false` |
| `--no-restart`      | Skip rollout-restart of the block node pod after reconfiguring                                        | `false` |
| `--with-reset`      | Wipe block node data directories; PVs and PVCs are preserved                                          | `false` |
| `--purge-storage`   | Delete PersistentVolumes and PersistentVolumeClaims in addition to wiping data (implies --with-reset) | `false` |

> **Storage path changes**: Local PV `hostPath.path` is immutable. If your
> reconfigure changes any storage path, you must pass `--purge-storage` so the
> existing PV/PVCs are deleted and recreated at the new paths. Running
> `reconfigure --with-reset` with a path change is rejected with a clear error.

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

```bash
# Install full Kubernetes stack for block nodes
sudo solo-provisioner kube cluster install \
  --profile=local \
  --node-type=block

# With error handling
sudo solo-provisioner kube cluster install \
  --profile=mainnet \
  --node-type=block \
  --rollback-on-error
```

**Flags**:

| Flag          | Short | Description                             | Required |
|---------------|-------|-----------------------------------------|----------|
| `--node-type` | `-n`  | Type of node (block\|consensus\|mirror) | Yes      |

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

Manage node-level network state behind the traffic shaper. The `firewall` scope manages the node-agnostic `inet host` nftables table — the host's own SSH/management allowlist, ICMP policy, and in-cluster host-service ports. It is separate from the `inet weaver` workload plane and applies to every node type (block, consensus, mirror, relay).

#### Create the Host Firewall

create-if-missing: if the `inet host` table already exists, the command makes no changes unless `--force` is passed (which re-renders from the flags). Every mutation applies to the live kernel in one atomic `nft -f` transaction and atomically rewrites `/etc/solo-provisioner/network-host.nft`.

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
| `--mgmt-cidrs`       | Management/SSH allowlist CIDRs (comma-separated or repeated) — **omitting this flag leaves the SSH allow rule with an empty source set under the default-drop policy, which will lock you out of new SSH connections** | (none) |
| `--in-cluster-ports` | Host-service ports reachable from the pod CIDR                     | `4244,6443,7472,10250` |
| `--ssh-port`         | SSH/management TCP port accepted from the allowlist                | `22`               |
| `--pod-cidr`         | Pod CIDR allowed to reach the in-cluster host-service ports        | auto-detected      |
| `--force`            | Re-render the table even if it already exists (global flag)        | `false`            |

When `--pod-cidr` is omitted it is **auto-detected** from the local node's `.spec.podCIDR` via the Kubernetes API (the node is matched by hostname, or the sole node on a single-node host). Detection is best-effort: `network firewall create` is node-agnostic and may run before a cluster exists, so if no cluster is reachable the command logs a warning and **omits the in-cluster-ports rule** — pass `--pod-cidr` explicitly to render it anyway.

ICMP is a fixed, safe ruleset (not configurable): full ICMP from the management allowlist, and from every other source the path-health subset — `destination-unreachable` (Path MTU Discovery) and `time-exceeded` (traceroute) always accepted, with `echo-request` (ping) rate-limited to 10/second. There are deliberately no ICMP flags: dropping ICMP errors would silently break PMTUD for legitimate clients.

> There is no `--service-ports`: BN ports live only in `network policy --ports` (the host firewall is bypassed by the eBPF datapath).

#### Modify the Allowlist / Ports

`add`/`remove` operate on a single element; `set` atomically replaces the full list.

```bash
sudo solo-provisioner network firewall add    --mgmt-cidr 10.1.0.0/16
sudo solo-provisioner network firewall remove --mgmt-cidr 10.0.0.0/8
sudo solo-provisioner network firewall set    --mgmt-cidrs 10.0.0.0/8,192.168.0.0/16

sudo solo-provisioner network firewall add    --in-cluster-port 9100
sudo solo-provisioner network firewall remove --in-cluster-port 10250
sudo solo-provisioner network firewall set    --in-cluster-ports 6443,4244
```

**Flags**:

| Verb           | Flag                 | Description                                                          |
|----------------|----------------------|----------------------------------------------------------------------|
| `add`/`remove` | `--mgmt-cidr`        | A single management CIDR (mutually exclusive with `--in-cluster-port`) |
| `add`/`remove` | `--in-cluster-port`  | A single in-cluster host-service port                                |
| `set`          | `--mgmt-cidrs`       | Full management allowlist (replaces the existing list)               |
| `set`          | `--in-cluster-ports` | Full in-cluster host-service port list (replaces the existing list)  |

#### Show / Delete the Host Firewall

```bash
# Show the live inet host table
sudo solo-provisioner network firewall show

# Remove the table and /etc/solo-provisioner/network-host.nft
sudo solo-provisioner network firewall delete
```

> `delete` removes the table and `/etc/solo-provisioner/network-host.nft` but does not disable the shared `solo-provisioner-network-nft.service` (shared with `inet weaver`); disable it manually if you need it off.

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
| `--monitor-block-node`    | Enable Block Node specific monitoring                                                                                             |
| `--add-prometheus-remote` | Add a Prometheus remote (format: `name=<name>,url=<url>,username=<username>[,labelProfile=eng\|ops]`). Repeatable. Default: `eng` |
| `--add-loki-remote`       | Add a Loki remote (format: `name=<name>,url=<url>,username=<username>[,labelProfile=eng\|ops]`). Repeatable. Default: `eng`       |
| `--prometheus-url`        | Prometheus remote write URL *(deprecated: use `--add-prometheus-remote`)*                                                         |
| `--prometheus-username`   | Prometheus authentication username *(deprecated)*                                                                                 |
| `--loki-url`              | Loki remote write URL *(deprecated: use `--add-loki-remote`)*                                                                     |
| `--loki-username`         | Loki authentication username *(deprecated)*                                                                                       |

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
| `ops`             | `cluster`, `environment`, `instance_type`, `inventory_name`, `ip` (optional) |

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
# Default YAML output
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

# Uninstall Solo Provisioner itself
sudo solo-provisioner uninstall
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
sudo solo-provisioner kube cluster install   --profile=<profile> --node-type=block
sudo solo-provisioner kube cluster uninstall

# TELEPORT
sudo solo-provisioner teleport node install    --token=<token> --proxy=<addr>
sudo solo-provisioner teleport node uninstall
sudo solo-provisioner teleport cluster install --values=<file>
sudo solo-provisioner teleport cluster uninstall

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
solo-provisioner version [--output=json|yaml]
solo-provisioner --help
```

---

*Document Version: 1.3.0 | Last Updated: June 2026*

