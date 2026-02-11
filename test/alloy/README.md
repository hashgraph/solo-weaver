# Alloy Stack - Grafana Alloy with Vault

Complete Alloy observability stack for Solo Provisioner with Vault-managed secrets.

**Components:**
- Grafana Alloy - Metrics and log collection
- HashiCorp Vault - Secret management
- External Secrets Operator - Secret synchronization
- Prometheus - Metrics storage
- Loki - Log aggregation
- Grafana - Visualization

## 🎯 Architecture Overview

**All secrets are managed by Vault via External Secrets Operator** - no plain Kubernetes secrets!

```
Local Dev:                          Production:
Docker Vault (dev mode)             Enterprise Vault cluster
     ↓                                   ↓
External Secrets Operator  ←→  External Secrets Operator
     ↓                                   ↓
K8s Secret (auto-synced)       K8s Secret (auto-synced)
     ↓                                   ↓
Alloy Pod (metrics/logs)       Alloy Pod (metrics/logs)
```

---

## 🚀 Quick Start - Local Development

### Prerequisites: Build Solo Provisioner

From your Mac, ensure you have the latest build:
```bash
task build
```

### Step 1: Start Alloy Stack

Start and SSH into a fresh VM:
```bash
task vm:ssh
```

Then, from within the VM, start the Alloy stack:
```bash
task alloy:start
```

### Step 2: Install Cluster

Copy the solo-provisioner binary:
```bash
cp /mnt/solo-weaver/bin/solo-provisioner-linux-arm64 ~/.

sudo ~/solo-provisioner-linux-arm64 install
```

Inside the VM (`task vm:ssh`):
```bash
sudo solo-provisioner block node install \
  --profile=local \
  --config=/mnt/solo-weaver/test/config/config.yaml
```

### Step 3: Install Alloy (Minimal)

Install Alloy without remote endpoints. This installs External Secrets Operator and the basic Alloy stack without requiring secrets from Vault:

```bash
sudo solo-provisioner alloy cluster install \
  --cluster-name=vm-cluster
```

> **Note:** Without `--add-prometheus-remote` or `--add-loki-remote` flags, Alloy installs in "local-only" mode. No secrets are required.

### Step 4: Configure Vault Connection

Now that ESO is installed, configure the ClusterSecretStore to connect to Vault:
```bash
task vault:setup-secret-store
```

This will auto-detect the node IP and configure the ClusterSecretStore.

### Step 5: Upgrade Alloy with Remotes

Now that secrets can sync, upgrade Alloy with remote endpoints:
```bash
# Get the node IP for remote endpoints
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
echo "Node IP: $NODE_IP"

sudo solo-provisioner alloy cluster install \
  --add-prometheus-remote=local:http://$NODE_IP:9090/api/v1/write:admin \
  --add-loki-remote=local:http://$NODE_IP:3100/loki/api/v1/push:admin \
  --cluster-name=vm-cluster \
  --monitor-block-node
```

> **Note:** The `--add-*-remote` flags use the format `name:url:username`. The password is fetched from Vault at path `grafana/alloy/{clusterName}/{prometheus|loki}/{remoteName}`.

Wait for pods to be ready:
```bash
kubectl get pods -n grafana-alloy
# Should show Running
```

### Step 6: Access Grafana and Vault UI

From your Mac, forward the ports:
```bash
task vm:alloy-forward
```

This forwards:
- **Grafana:** http://localhost:3000 (anonymous auth enabled - no login required)
- **Vault UI:** http://localhost:8200 (token: `devtoken`)

### Step 7: Verify in Grafana

Navigate to **Explore** and test these queries organized by module:

#### Agent Metrics Module (agent-metrics.alloy)

**Prometheus queries:**
```promql
# Alloy is running
up{job="alloy"}

# Alloy internal metrics
alloy_build_info{cluster="vm-cluster"}

# Alloy component health
alloy_component_controller_running_components
```

#### Node Exporter Module (node-exporter.alloy)

**Prometheus queries:**
```promql
# CPU usage
node_cpu_seconds_total{cluster="vm-cluster"}

# Memory usage
node_memory_MemAvailable_bytes{cluster="vm-cluster"}
node_memory_MemTotal_bytes{cluster="vm-cluster"}

# Disk usage
node_filesystem_avail_bytes{cluster="vm-cluster"}
node_filesystem_size_bytes{cluster="vm-cluster"}

# Network I/O
node_network_receive_bytes_total{cluster="vm-cluster"}
node_network_transmit_bytes_total{cluster="vm-cluster"}
```

#### Kubelet/cAdvisor Module (kubelet.alloy)

**Prometheus queries:**
```promql
# Container CPU usage
container_cpu_usage_seconds_total{cluster="vm-cluster"}

# Container memory usage
container_memory_usage_bytes{cluster="vm-cluster"}

# Container network I/O
container_network_receive_bytes_total{cluster="vm-cluster"}
container_network_transmit_bytes_total{cluster="vm-cluster"}

# Kubelet metrics
kubelet_running_pods{cluster="vm-cluster"}
kubelet_running_containers{cluster="vm-cluster"}
```

#### Syslog Module (syslog.alloy)

**Loki queries** (switch datasource to Loki):
```logql
# All system logs from journald
{cluster="vm-cluster"}

# Filter by systemd unit
{cluster="vm-cluster", unit="k3s.service"}
{cluster="vm-cluster", unit="docker.service"}

# Filter by priority (err, warning, info, debug)
{cluster="vm-cluster"} | priority = "err"

# Filter by syslog identifier
{cluster="vm-cluster", syslog_identifier="kernel"}
```

#### Block Node Module (block-node.alloy) - if `--monitor-block-node` is used

**Prometheus queries:**
```promql
# Block Node application metrics
{cluster="vm-cluster", namespace="block-node"}

# Block Node up status
up{cluster="vm-cluster", namespace="block-node"}
```

**Loki queries:**
```logql
# Block Node pod logs
{cluster="vm-cluster", namespace="block-node"}

# Filter Block Node logs by pod
{cluster="vm-cluster", namespace="block-node", pod=~"block-node-server.*"}

# Search for errors in Block Node logs
{cluster="vm-cluster", namespace="block-node"} |= "error"
```

#### Remotes Module (remotes.alloy) - if remotes are configured

The remotes module doesn't produce metrics itself - it configures where metrics and logs are sent. To verify remotes are working:

**Prometheus queries:**
```promql
# Check remote write status (should show samples being sent)
prometheus_remote_storage_samples_total

# Check for remote write failures
prometheus_remote_storage_failed_samples_total
```

---

## 🧹 Cleanup

### Uninstall Alloy from Kubernetes

Inside the VM:
```bash
sudo solo-provisioner alloy cluster uninstall
```

### Stop Local Stack

```bash
# Stop Alloy stack
task vm:alloy:stop

# Remove all data
task vm:alloy:clean
```

---

## 📋 What Gets Monitored

**Always monitored:**
- ✅ Kubernetes node metrics (via node-exporter ServiceMonitor)
- ✅ Container CPU, memory, disk I/O (via cAdvisor)
- ✅ Kubelet metrics
- ✅ System logs from journald (syslog)
- ✅ Alloy self-monitoring

**When `--monitor-block-node` flag is used:**
- ✅ Block Node application metrics (via ServiceMonitor)
- ✅ Block Node pod logs
- ✅ Block Node container metrics (CPU, memory, disk via cAdvisor)

---

## 🔧 Advanced

### Multiple Remote Endpoints

You can configure multiple Prometheus and Loki remote endpoints for redundancy or multi-tenancy:

```bash
sudo solo-provisioner alloy cluster install \
  --cluster-name=vm-cluster \
  --add-prometheus-remote=primary:http://prom1:9090/api/v1/write:user1 \
  --add-prometheus-remote=backup:http://prom2:9090/api/v1/write:user2 \
  --add-loki-remote=primary:http://loki1:3100/loki/api/v1/push:user1 \
  --add-loki-remote=grafana-cloud:https://logs.grafana.net/loki/api/v1/push:12345 \
  --monitor-block-node
```

Each remote requires a corresponding Vault secret at:
- `grafana/alloy/{clusterName}/prometheus/{remoteName}` → property: `password`
- `grafana/alloy/{clusterName}/loki/{remoteName}` → property: `password`

### Managing Remote Endpoints

The `alloy cluster install` command is **declarative** - it replaces the entire configuration with what you specify. To manage endpoints:

**Add a new remote:** Include all existing remotes plus the new one:
```bash
# If you had 'primary', and want to add 'backup':
sudo solo-provisioner alloy cluster install \
  --cluster-name=vm-cluster \
  --add-prometheus-remote=primary:http://prom1:9090/api/v1/write:user1 \
  --add-prometheus-remote=backup:http://prom2:9090/api/v1/write:user2 \
  --add-loki-remote=primary:http://loki1:3100/loki/api/v1/push:user1
```

**Remove a remote:** Simply omit it from the command:
```bash
# Remove 'backup', keep only 'primary':
sudo solo-provisioner alloy cluster install \
  --cluster-name=vm-cluster \
  --add-prometheus-remote=primary:http://prom1:9090/api/v1/write:user1 \
  --add-loki-remote=primary:http://loki1:3100/loki/api/v1/push:user1
```

**Modify a remote URL:** Specify the same name with the new URL:
```bash
# Change 'primary' Prometheus URL:
sudo solo-provisioner alloy cluster install \
  --cluster-name=vm-cluster \
  --add-prometheus-remote=primary:http://new-prom:9090/api/v1/write:user1 \
  --add-loki-remote=primary:http://loki1:3100/loki/api/v1/push:user1
```

**Remove all remotes (local-only mode):**
```bash
sudo solo-provisioner alloy cluster install \
  --cluster-name=vm-cluster
```

> **Important:** Each run replaces the previous configuration. Always specify all the remotes you want to keep.

### Modular Configuration

Alloy configuration is built from modular template files in `internal/templates/files/alloy/`. 
The ConfigMap contains a single `config.alloy` key with all modules concatenated:

```
internal/templates/files/alloy/
├── agent-metrics.alloy  # Alloy self-monitoring
├── core.alloy           # Basic Alloy setup, logging config
├── kubelet.alloy        # Kubelet/cAdvisor container metrics
├── node-exporter.alloy  # Host metrics via ServiceMonitor
├── remotes.alloy        # Prometheus/Loki remote write (if configured)
├── syslog.alloy         # System logs via journald
├── block-node.alloy     # Block Node monitoring (if --monitor-block-node)
├── configmap.yaml       # ConfigMap manifest template
└── external-secret.yaml # ExternalSecret manifest template

grafana-alloy-cm ConfigMap:
└── config.alloy         # Concatenated modules (used by Alloy)
```

Modules are conditionally included based on the flags you provide:

| Module | Condition | What it monitors |
|--------|-----------|------------------|
| Core | Always | Basic Alloy setup, logging config |
| Remotes | If remotes configured | Prometheus/Loki remote write endpoints |
| Agent Metrics | Always | Alloy self-monitoring |
| Node Exporter | Always | Host metrics (CPU, memory, disk) |
| Kubelet/cAdvisor | Always | Container metrics |
| Syslog | Always | System logs via journald |
| Block Node | `--monitor-block-node` | Block Node metrics and logs |

To view the current ConfigMap contents:
```bash
kubectl get configmap grafana-alloy-cm -n grafana-alloy -o yaml
```

### Manage Vault Separately

```bash
task vault:start   # Start Vault only
task vault:stop    # Stop Vault
task vault:clean   # Remove Vault data
```

### Check Vault Secrets

To verify secrets exist in local Vault (useful for debugging):

```bash
# List all secrets under the alloy path
docker exec -e VAULT_ADDR='http://localhost:8200' -e VAULT_TOKEN='devtoken' \
  solo-weaver-vault vault kv list secret/grafana/alloy/vm-cluster/prometheus

# Read a specific secret (e.g., for the 'local' remote)
docker exec -e VAULT_ADDR='http://localhost:8200' -e VAULT_TOKEN='devtoken' \
  solo-weaver-vault vault kv get secret/grafana/alloy/vm-cluster/prometheus/local

# Read Loki secret
docker exec -e VAULT_ADDR='http://localhost:8200' -e VAULT_TOKEN='devtoken' \
  solo-weaver-vault vault kv get secret/grafana/alloy/vm-cluster/loki/local
```

To manually add a secret for a new remote:
```bash
# Add secret for a remote named 'myremote'
docker exec -e VAULT_ADDR='http://localhost:8200' -e VAULT_TOKEN='devtoken' \
  solo-weaver-vault vault kv put secret/grafana/alloy/vm-cluster/prometheus/myremote password="my-password"
```

You can also access the Vault UI at http://localhost:8200 (token: `devtoken`) after running `task vm:alloy-forward`.

### Production Setup

For production, configure ClusterSecretStore to point to your enterprise Vault:

```yaml
apiVersion: external-secrets.io/v1
kind: ClusterSecretStore
metadata:
  name: vault-secret-store
spec:
  provider:
    vault:
      server: "https://vault.example.com"
      path: "secret"
      version: v2
      auth:
        userPass:
          path: userpass
          username: "production-eso-user"
          secretRef:
            name: vault-credentials
            namespace: kube-system
            key: password
```

---

## 📁 Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Container definitions |
| `init-vault.sh` | Initialize Vault with dev secrets |
| `cluster-secret-store-local.yaml` | ESO → Vault connection template |
| `config_with_alloy.yaml` | Solo Provisioner config with Alloy enabled |
| `prometheus.yml` | Prometheus configuration |
| `loki-config.yml` | Loki configuration |
| `grafana-datasources.yml` | Grafana datasources |

