# Quickstart Guide

Below is a quickstart guide to get you up and running with Solo Provisioner.

## Prerequisites

- Unix operating system (Tested on: Debian 13.1.0, Ubuntu 22.04)
- `curl` installed

## Install

- Run the single-line installer:

```
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

| Flag | Short | Description | Default |
|------|-------|-------------|---------|
| `--config` | `-c` | Path to configuration file | None |
| `--profile` | `-p` | Deployment profile | Required for most commands |
| `--output` | `-o` | Output format (yaml\|json) | `yaml` |
| `--version` | `-v` | Show version | - |
| `--help` | `-h` | Show help | - |

### Error Handling Flags

Most installation commands support these execution control flags:

| Flag | Description | Default |
|------|-------------|---------|
| `--stop-on-error` | Stop execution on first error | `true` |
| `--rollback-on-error` | Rollback executed steps on error | `false` |
| `--continue-on-error` | Continue executing steps even if some fail | `false` |

---

## Deployment Profiles

Solo Provisioner supports four deployment profiles that configure behavior and defaults:

| Profile | Description | Use Case |
|---------|-------------|----------|
| `local` | Local development and testing | Development, CI/CD |
| `perfnet` | Performance testing network | Load testing |
| `testnet` | Hedera Testnet | Integration testing |
| `mainnet` | Hedera Mainnet | Production deployment |

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
sudo solo-provisioner block node install \
  --profile=mainnet \
  --base-path=/mnt/nvme \
  --live-size=50Gi \
  --archive-size=500Gi \
  --verification-size=50Gi \
  --log-size=10Gi

# With specific chart version
sudo solo-provisioner block node install \
  --profile=testnet \
  --chart-version=0.22.1 \
  --namespace=hedera-block
```

**Available Flags**:

| Flag | Description |
|------|-------------|
| `--values`, `-f` | Custom Helm values file |
| `--chart-repo` | Helm chart repository URL |
| `--chart-version` | Specific chart version |
| `--namespace` | Kubernetes namespace |
| `--release-name` | Helm release name |
| `--base-path` | Base path for all storage |
| `--archive-path` | Archive storage path |
| `--live-path` | Live storage path |
| `--verification-path` | Verification storage path |
| `--log-path` | Log storage path |
| `--live-size` | Live storage size (e.g., 10Gi) |
| `--archive-size` | Archive storage size |
| `--verification-size` | Verification storage size |
| `--log-size` | Log storage size |

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

| Flag | Description | Default |
|------|-------------|---------|
| `--no-reuse-values` | Don't reuse previous release values | `false` |
| `--with-reset` | Reset storage (clear all data) before upgrade | `false` |

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
3. Clears all storage directories (archive, live, log, verification)
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
- **External Secrets Operator**: Secret management integration
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

| Flag | Short | Description | Required |
|------|-------|-------------|----------|
| `--node-type` | `-n` | Type of node (block\|consensus\|mirror) | Yes |

#### Uninstall Kubernetes Cluster

Tears down the entire Kubernetes stack including all components (kubeadm, CRI-O, Cilium, etc.) while preserving the downloads cache:

```bash
# Basic uninstall
sudo solo-provisioner kube cluster uninstall

# Continue even if some steps fail
sudo solo-provisioner kube cluster uninstall --continue-on-error
```

> **Warning**: This tears down the entire cluster. All running workloads will be stopped.

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

| Flag | Description |
|------|-------------|
| `--token` | Join token for Teleport agent |
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

| Flag | Description |
|------|-------------|
| `--values` | Path to Teleport Helm values file |

**Optional Flags**:

| Flag | Description |
|------|-------------|
| `--version` | Teleport Helm chart version |

---

### Alloy Commands

Manage Grafana Alloy observability stack for metrics and logs.

#### Install Alloy Stack

```bash
# Basic installation with single remote (legacy mode)
sudo solo-provisioner alloy cluster install \
  --cluster-name=mainnet-block-01 \
  --prometheus-url=https://prometheus.example.com/api/v1/write \
  --prometheus-username=metrics-user \
  --loki-url=https://loki.example.com/loki/api/v1/push \
  --loki-username=logs-user

# Multiple remotes using repeatable flags (recommended)
sudo solo-provisioner alloy cluster install \
  --cluster-name=mainnet-block-01 \
  --add-prometheus-remote=name=primary,url=https://prom1.example.com/api/v1/write,username=user1 \
  --add-prometheus-remote=name=backup,url=https://prom2.example.com/api/v1/write,username=user2 \
  --add-loki-remote=name=primary,url=https://loki1.example.com/loki/api/v1/push,username=user1 \
  --add-loki-remote=name=backup,url=https://loki2.example.com/loki/api/v1/push,username=user2 \
  --monitor-block-node
```

**Available Flags**:

| Flag | Description |
|------|-------------|
| `--cluster-name` | Cluster name for metrics/logs labels |
| `--monitor-block-node` | Enable Block Node specific monitoring |
| `--add-prometheus-remote` | Add a Prometheus remote (format: `name=<name>,url=<url>,username=<username>`). Repeatable |
| `--add-loki-remote` | Add a Loki remote (format: `name=<name>,url=<url>,username=<username>`). Repeatable |
| `--prometheus-url` | Prometheus remote write URL *(deprecated: use `--add-prometheus-remote`)* |
| `--prometheus-username` | Prometheus authentication username *(deprecated)* |
| `--loki-url` | Loki remote write URL *(deprecated: use `--add-loki-remote`)* |
| `--loki-username` | Loki authentication username *(deprecated)* |

> **Note**: Passwords are managed via Vault and External Secrets Operator, not via CLI flags.

#### Multiple Remote Endpoints

The `--add-prometheus-remote` and `--add-loki-remote` flags use the format `name=<name>,url=<url>,username=<username>`:
- **name**: Unique identifier for the remote (e.g., `primary`, `backup`, `grafana-cloud`)
- **url**: The remote write endpoint URL
- **username**: Authentication username (password is fetched from Vault)

**Vault Secret Paths** (for multiple remotes):
- Prometheus: `grafana/alloy/{clusterName}/prometheus/{remoteName}` → property: `password`
- Loki: `grafana/alloy/{clusterName}/loki/{remoteName}` → property: `password`

Example for `mainnet-block-01` cluster with `primary` and `backup` remotes:
```
grafana/alloy/mainnet-block-01/prometheus/primary
grafana/alloy/mainnet-block-01/prometheus/backup
grafana/alloy/mainnet-block-01/loki/primary
grafana/alloy/mainnet-block-01/loki/backup
```

#### Managing Remote Endpoints

The `alloy cluster install` command is **declarative** - it replaces the entire remote configuration with what you specify. To manage endpoints:

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

**Remove all remotes (local-only mode):**
```bash
sudo solo-provisioner alloy cluster install \
  --cluster-name=mainnet-block-01
```

> **Important:** Each run replaces the previous remote configuration. Always specify all the remotes you want to keep.


#### Uninstall Alloy Stack

```bash
sudo solo-provisioner alloy cluster uninstall
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
  prometheusUrl: "https://prometheus.example.com/api/v1/write"
  prometheusUsername: "metrics"
  lokiUrl: "https://loki.example.com/loki/api/v1/push"
  lokiUsername: "logs"

teleport:
  version: "16.0.0"
  valuesFile: "/path/to/teleport-values.yaml"
  nodeAgentToken: ""      # Set via flag for security
  nodeAgentProxyAddr: "proxy.teleport.example.com:443"
```

### Configuration Precedence

Solo Provisioner uses this precedence order (highest to lowest):

1. Command-line flags
2. Environment variables (when using `--config`)
3. Configuration file
4. Built-in defaults

### Environment Variables

Environment variables can override configuration file values. They require a config file to be provided via `--config` flag.

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
  --prometheus-url=https://metrics.hedera.internal/write \
  --prometheus-username=block-metrics
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
sudo solo-provisioner block node check   --profile=<profile>
sudo solo-provisioner block node install --profile=<profile> [--values=<file>]
sudo solo-provisioner block node upgrade --profile=<profile> [--values=<file>] [--with-reset]
sudo solo-provisioner block node reset   --profile=<profile>

# KUBERNETES
sudo solo-provisioner kube cluster install   --profile=<profile> --node-type=block
sudo solo-provisioner kube cluster uninstall

# TELEPORT
sudo solo-provisioner teleport node install    --token=<token> --proxy=<addr>
sudo solo-provisioner teleport cluster install --values=<file>

# ALLOY
sudo solo-provisioner alloy cluster install   [--monitor-block-node] [--cluster-name=<name>]
sudo solo-provisioner alloy cluster uninstall

# UTILITIES
solo-provisioner version [--output=json|yaml]
solo-provisioner --help
```

---

*Document Version: 1.1.0 | Last Updated: February 2026*

