# Weaver CLI Reference Guide

A comprehensive guide to Solo Weaver, your one-stop tool for provisioning Hedera network components.

> **Audience**: Node engineers and operators deploying Hedera Nodes in production and test environments.

---

## Table of Contents

- [Overview](#overview)
- [Installation](#installation)
- [Global Flags](#global-flags)
- [Deployment Profiles](#deployment-profiles)
- [Command Reference](#command-reference)
  - [Block Node Commands](#block-node-commands)
  - [Kubernetes Commands](#kubernetes-commands)
  - [Teleport Commands](#teleport-commands)
  - [Alloy Commands](#alloy-commands)
  - [Utility Commands](#utility-commands)
- [Configuration](#configuration)
- [Workflow Examples](#workflow-examples)
- [Troubleshooting](#troubleshooting)
- [Command Structure Analysis](#command-structure-analysis)

---

## Overview

Solo Weaver automates the deployment and management of Hedera network components, including:

- **Block Nodes**: Hedera Block Node deployment on Kubernetes
- **Kubernetes Clusters**: Single-node Kubernetes cluster with kubeadm, CRI-O, Cilium, and more
- **Teleport**: Secure SSH and kubectl access agents
- **Alloy**: Grafana Alloy observability stack for metrics and logs

### Command Hierarchy

```
weaver
├── install          # Self-install Solo Weaver
├── uninstall        # Self-uninstall Solo Weaver
├── block            # Block node management
│   └── node
│       ├── check    # Run preflight checks
│       ├── install  # Install block node
│       └── upgrade  # Upgrade block node
├── kube             # Kubernetes cluster management
│   └── cluster
│       ├── install  # Install full K8s stack
│       └── uninstall# Remove K8s stack
├── teleport         # Secure access management
│   ├── node
│   │   └── install  # Install SSH agent
│   └── cluster
│       └── install  # Install K8s agent
├── alloy            # Observability stack
│   └── cluster
│       ├── install  # Install Alloy stack
│       └── uninstall# Remove Alloy stack
└── version          # Show version info
```

---

## Installation

### Install Weaver

Download the latest release from GitHub and run the self-install command:

```bash
# Download the latest release for your architecture (example for Linux amd64)
curl -L -o weaver https://github.com/hashgraph/solo-weaver/releases/latest/download/weaver-linux-amd64
chmod +x weaver

# Run self-install (installs to system paths)
sudo ./weaver install

# Verify installation
weaver --help
```


### Uninstall

```bash
sudo weaver uninstall
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

Weaver supports four deployment profiles that configure behavior and defaults:

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
sudo weaver block node check --profile=mainnet

# With custom config file
sudo weaver block node check --profile=testnet --config=/path/to/config.yaml
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
sudo weaver block node install --profile=local

# Production installation with custom values
sudo weaver block node install \
  --profile=mainnet \
  --config=/path/to/config.yaml \
  --values=/path/to/custom-values.yaml

# With custom storage configuration
sudo weaver block node install \
  --profile=mainnet \
  --base-path=/mnt/nvme \
  --live-size=50Gi \
  --archive-size=500Gi \
  --log-size=10Gi

# With specific chart version
sudo weaver block node install \
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
| `--log-path` | Log storage path |
| `--live-size` | Live storage size (e.g., 10Gi) |
| `--archive-size` | Archive storage size |
| `--log-size` | Log storage size |

#### Upgrade Block Node

Upgrade an existing Block Node deployment:

```bash
# Upgrade with new values file
sudo weaver block node upgrade \
  --profile=mainnet \
  --values=/path/to/new-values.yaml

# Upgrade to specific chart version
sudo weaver block node upgrade \
  --profile=mainnet \
  --chart-version=0.23.0

# Upgrade and reset to chart defaults (don't reuse previous values)
sudo weaver block node upgrade \
  --profile=mainnet \
  --values=/path/to/values.yaml \
  --no-reuse-values
```

**Additional Flags**:

| Flag | Description | Default |
|------|-------------|---------|
| `--no-reuse-values` | Don't reuse previous release values | `false` |

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
sudo weaver kube cluster install \
  --profile=local \
  --node-type=block

# With error handling
sudo weaver kube cluster install \
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
sudo weaver kube cluster uninstall

# Continue even if some steps fail
sudo weaver kube cluster uninstall --continue-on-error
```

> **Warning**: This tears down the entire cluster. All running workloads will be stopped.

---

### Teleport Commands

Configure secure access using Teleport agents.

#### Install Node Agent (SSH Access)

Install the Teleport node agent for secure SSH access to the host:

```bash
# Install with required token and proxy address
sudo weaver teleport node install \
  --token=<join-token> \
  --proxy=proxy.teleport.example.com:443

# With error handling
sudo weaver teleport node install \
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
sudo weaver teleport cluster install \
  --values=/path/to/teleport-values.yaml

# With specific version
sudo weaver teleport cluster install \
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
# Basic installation
sudo weaver alloy cluster install

# Full configuration with monitoring
sudo weaver alloy cluster install \
  --monitor-block-node \
  --cluster-name=mainnet-block-01 \
  --prometheus-url=https://prometheus.example.com/api/v1/write \
  --prometheus-username=metrics-user \
  --loki-url=https://loki.example.com/loki/api/v1/push \
  --loki-username=logs-user
```

**Available Flags**:

| Flag | Description |
|------|-------------|
| `--monitor-block-node` | Enable Block Node specific monitoring |
| `--cluster-name` | Cluster name for metrics/logs labels |
| `--prometheus-url` | Prometheus remote write URL |
| `--prometheus-username` | Prometheus authentication username |
| `--loki-url` | Loki remote write URL |
| `--loki-username` | Loki authentication username |

> **Note**: Passwords are managed via Vault and External Secrets Operator, not via CLI flags.

#### Uninstall Alloy Stack

```bash
sudo weaver alloy cluster uninstall
```

---

### Utility Commands

#### Show Version

```bash
# Default YAML output
weaver version

# JSON output
weaver version --output=json

# Short flag
weaver -v
```

---

## Configuration

### Configuration File

Weaver supports YAML configuration files with the `--config` flag:

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

Weaver uses this precedence order (highest to lowest):

1. Command-line flags
2. Environment variables (when using `--config`)
3. Configuration file
4. Built-in defaults

### Environment Variables

Environment variables can override configuration file values. They require a config file to be provided via `--config` flag.

**Format**: `WEAVER_<SECTION>_<FIELD>` (uppercase, underscores for nested fields)

```bash
# Override block node storage base path
export WEAVER_BLOCKNODE_STORAGE_BASEPATH=/data/block-node

# Override block node namespace
export WEAVER_BLOCKNODE_NAMESPACE=my-block-node

# Then run with a config file
sudo weaver block node install --profile=mainnet --config=/etc/weaver/config.yaml
```

---

## Workflow Examples

### Complete Block Node Deployment (Production)

```bash
# Step 1: Deploy the block node (includes preflight checks and K8s setup)
sudo weaver block node install \
  --profile=mainnet \
  --config=/etc/weaver/config.yaml \
  --values=/etc/weaver/block-node-values.yaml

# Step 2: (Optional) Set up secure SSH access
sudo weaver teleport node install \
  --token=$TELEPORT_JOIN_TOKEN \
  --proxy=teleport.hedera.com:443

# Step 3: (Optional) Set up secure kubectl access
sudo weaver teleport cluster install \
  --values=/etc/weaver/teleport-kube-values.yaml

# Step 4: (Optional) Set up monitoring
sudo weaver alloy cluster install \
  --monitor-block-node \
  --cluster-name=mainnet-block-01 \
  --prometheus-url=https://metrics.hedera.internal/write \
  --prometheus-username=block-metrics
```

### Development Environment Setup

```bash
# Quick local setup for development
sudo weaver block node install --profile=local

# Verify deployment
kubectl get pods -n block-node
```

### Upgrade Workflow

```bash
# Step 1: Prepare new values file with updated config

# Step 2: Perform upgrade
sudo weaver block node upgrade \
  --profile=mainnet \
  --values=/etc/weaver/block-node-values-v2.yaml \
  --chart-version=0.24.0

# Step 3: Verify
kubectl get pods -n block-node
```

### Clean Teardown

```bash
# Remove Alloy monitoring
sudo weaver alloy cluster uninstall

# Remove Kubernetes cluster (removes block node)
sudo weaver kube cluster uninstall

# Uninstall Weaver itself
sudo weaver uninstall
```

---

## Troubleshooting

### Common Issues

**1. Permission Denied**
```bash
# Most commands require root privileges
sudo weaver block node install --profile=local
```

**2. Profile Not Specified**
```bash
# Error: profile flag is required
# Solution: Always specify --profile
sudo weaver block node check --profile=mainnet
```

**3. Invalid Storage Path**
```bash
# Error: invalid base path
# Ensure path exists and has correct permissions
sudo mkdir -p /mnt/storage
sudo weaver block node install --profile=mainnet --base-path=/mnt/storage
```

**4. Helm Chart Issues**
```bash
# Check specific chart version availability
# Use explicit version if needed
sudo weaver block node install \
  --profile=mainnet \
  --chart-version=0.22.1
```

### Getting Help

```bash
# General help
weaver --help

# Command-specific help
weaver block --help
weaver block node --help
weaver block node install --help
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
sudo ./weaver install

# BLOCK NODE
sudo weaver block node check   --profile=<profile>
sudo weaver block node install --profile=<profile> [--values=<file>]
sudo weaver block node upgrade --profile=<profile> [--values=<file>]

# KUBERNETES
sudo weaver kube cluster install   --profile=<profile> --node-type=block
sudo weaver kube cluster uninstall

# TELEPORT
sudo weaver teleport node install    --token=<token> --proxy=<addr>
sudo weaver teleport cluster install --values=<file>

# ALLOY
sudo weaver alloy cluster install   [--monitor-block-node] [--cluster-name=<name>]
sudo weaver alloy cluster uninstall

# UTILITIES
weaver version [--output=json|yaml]
weaver --help
```

---

*Document Version: 1.0.0 | Last Updated: January 2026*

