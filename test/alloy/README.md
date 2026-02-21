# Alloy Stack - Grafana Alloy Observability

Complete Alloy observability stack for Solo Provisioner.

**Components:**
- Grafana Alloy - Metrics and log collection
- Prometheus - Metrics storage
- Loki - Log aggregation
- Grafana - Visualization

**Optional (for production secret management):**
- HashiCorp Vault - Secret management
- External Secrets Operator - Secret synchronization from Vault/AWS/GCP

## 🎯 Architecture Overview

Alloy expects passwords in a K8s Secret named `grafana-alloy-secrets` in the `grafana-alloy` namespace.
How that secret gets created is up to you — manually, via ESO/Vault, Terraform, or any other mechanism.

```
Manual (local dev):                 ESO + Vault (production):
kubectl create secret               Vault/AWS/GCP
     ↓                                   ↓
K8s Secret                     External Secrets Operator
  "grafana-alloy-secrets"              ↓
     ↓                         K8s Secret (auto-synced)
Alloy Pod (metrics/logs)          "grafana-alloy-secrets"
                                       ↓
                                Alloy Pod (metrics/logs)
```

**Key naming convention:**
- `PROMETHEUS_PASSWORD_<REMOTE_NAME>` — password for each `--add-prometheus-remote name=<remote_name>`
- `LOKI_PASSWORD_<REMOTE_NAME>` — password for each `--add-loki-remote name=<remote_name>`

---

## 🚀 Quick Start - Local Development

### Prerequisites: Build Solo Provisioner

From your Mac, ensure you have the latest build:
```bash
task build
```

### Step 1: Start Observability Stack

Start and SSH into a fresh VM:
```bash
task vm:ssh
```

Then, from within the VM, start the observability stack (Prometheus, Loki, Grafana):
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

### Step 3: Create K8s Secret

Create the K8s secret containing passwords for the remote endpoints.

**Option A: Using the task (recommended for local dev):**
```bash
task alloy:create-secret
```

This creates secret `grafana-alloy-secrets` in namespace `grafana-alloy` with keys
`PROMETHEUS_PASSWORD_LOCAL` and `LOKI_PASSWORD_LOCAL` set to `dev-password`.

**Option B: Using kubectl directly:**
```bash
kubectl create namespace grafana-alloy --dry-run=client -o yaml | kubectl apply -f -

kubectl create secret generic grafana-alloy-secrets \
  --namespace=grafana-alloy \
  --from-literal=PROMETHEUS_PASSWORD_LOCAL=dev-password \
  --from-literal=LOKI_PASSWORD_LOCAL=dev-password \
  --dry-run=client -o yaml | kubectl apply -f -
```

> **Convention:** The key names follow the pattern `{PROMETHEUS|LOKI}_PASSWORD_{REMOTE_NAME}`,
> where `REMOTE_NAME` matches the `name=` value in the `--add-*-remote` flags (uppercased, dashes replaced with underscores).

### Step 4: Install Alloy with Remotes

```bash
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
echo "Node IP: $NODE_IP"

sudo solo-provisioner alloy cluster install \
  --add-prometheus-remote=name=local,url=http://$NODE_IP:9090/api/v1/write,username=admin \
  --add-loki-remote=name=local,url=http://$NODE_IP:3100/loki/api/v1/push,username=admin \
  --cluster-name=vm-cluster \
  --monitor-block-node
```

> **Note:** The remote name `local` maps to secret keys `PROMETHEUS_PASSWORD_LOCAL` and `LOKI_PASSWORD_LOCAL`.

Wait for pods to be ready:
```bash
kubectl get pods -n grafana-alloy
# Should show Running
```

> **Tip:** To install Alloy without remote endpoints (local-only mode, no secrets needed):
> ```bash
> sudo solo-provisioner alloy cluster install --cluster-name=vm-cluster
> ```

### Step 5: Access Grafana

From your Mac, forward the ports:
```bash
task vm:alloy-forward
```

This forwards:
- **Grafana:** http://localhost:3000 (anonymous auth enabled - no login required)
- **Prometheus:** http://localhost:9090
- **Loki:** http://localhost:3100

### Step 6: Verify in Grafana

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
{cluster="vm-cluster", unit="kubelet.service"}
{cluster="vm-cluster", unit="docker.service"}

# Filter by priority (err, warning, info, debug)
{cluster="vm-cluster"} | priority = "err"

# Filter by syslog identifier
{cluster="vm-cluster", syslog_identifier="kernel"}
```

#### Block Node Module (block-node.alloy) - if `--monitor-block-node` is used

**Prometheus queries:**
```promql
# Block Node application metrics (via ServiceMonitor)
up{cluster="vm-cluster", job=~".*block-node.*"}

# Block Node service metrics
{cluster="vm-cluster", job=~".*block-node.*"}
```

**Loki queries:**
```logql
# Block Node pod logs
{cluster="vm-cluster", job="block-node/block-node-server"}

# Search for errors in Block Node logs
{cluster="vm-cluster", job="block-node/block-node-server"} |= "error"
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
- ✅ Block Node application metrics (via ServiceMonitor - auto-created by solo-provisioner)
- ✅ Block Node pod logs (via PodLogs CRD - auto-created by solo-provisioner)
- ✅ Block Node container metrics (CPU, memory, disk via cAdvisor)

---

## 🔧 Advanced

### Multiple Remote Endpoints

You can configure multiple Prometheus and Loki remote endpoints for redundancy or multi-tenancy:

```bash
sudo solo-provisioner alloy cluster install \
  --cluster-name=vm-cluster \
  --add-prometheus-remote=name=primary,url=http://prom1:9090/api/v1/write,username=user1 \
  --add-prometheus-remote=name=backup,url=http://prom2:9090/api/v1/write,username=user2 \
  --add-loki-remote=name=primary,url=http://loki1:3100/loki/api/v1/push,username=user1 \
  --add-loki-remote=name=grafana-cloud,url=https://logs.grafana.net/loki/api/v1/push,username=12345 \
  --monitor-block-node
```

Each remote requires a corresponding key in K8s Secret `grafana-alloy-secrets`:

| Remote flag | Expected secret key |
|---|---|
| `--add-prometheus-remote=name=primary,...` | `PROMETHEUS_PASSWORD_PRIMARY` |
| `--add-prometheus-remote=name=backup,...` | `PROMETHEUS_PASSWORD_BACKUP` |
| `--add-loki-remote=name=primary,...` | `LOKI_PASSWORD_PRIMARY` |
| `--add-loki-remote=name=grafana-cloud,...` | `LOKI_PASSWORD_GRAFANA_CLOUD` |

Create the secret with all required keys:
```bash
kubectl create secret generic grafana-alloy-secrets \
  --namespace=grafana-alloy \
  --from-literal=PROMETHEUS_PASSWORD_PRIMARY=pass1 \
  --from-literal=PROMETHEUS_PASSWORD_BACKUP=pass2 \
  --from-literal=LOKI_PASSWORD_PRIMARY=pass3 \
  --from-literal=LOKI_PASSWORD_GRAFANA_CLOUD=pass4 \
  --dry-run=client -o yaml | kubectl apply -f -
```

### Managing Remote Endpoints

The `alloy cluster install` command is **declarative** - it replaces the entire configuration with what you specify. To manage endpoints:

**Add a new remote:** Include all existing remotes plus the new one:
```bash
# If you had 'primary', and want to add 'backup':
sudo solo-provisioner alloy cluster install \
  --cluster-name=vm-cluster \
  --add-prometheus-remote=name=primary,url=http://prom1:9090/api/v1/write,username=user1 \
  --add-prometheus-remote=name=backup,url=http://prom2:9090/api/v1/write,username=user2 \
  --add-loki-remote=name=primary,url=http://loki1:3100/loki/api/v1/push,username=user1
```

**Remove a remote:** Simply omit it from the command:
```bash
# Remove 'backup', keep only 'primary':
sudo solo-provisioner alloy cluster install \
  --cluster-name=vm-cluster \
  --add-prometheus-remote=name=primary,url=http://prom1:9090/api/v1/write,username=user1 \
  --add-loki-remote=name=primary,url=http://loki1:3100/loki/api/v1/push,username=user1
```

**Modify a remote URL:** Specify the same name with the new URL:
```bash
# Change 'primary' Prometheus URL:
sudo solo-provisioner alloy cluster install \
  --cluster-name=vm-cluster \
  --add-prometheus-remote=name=primary,url=http://new-prom:9090/api/v1/write,username=user1 \
  --add-loki-remote=name=primary,url=http://loki1:3100/loki/api/v1/push,username=user1
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
├── agent-metrics.alloy          # Alloy self-monitoring
├── core.alloy                   # Basic Alloy setup, logging config
├── kubelet.alloy                # Kubelet/cAdvisor container metrics
├── node-exporter.alloy          # Host metrics via ServiceMonitor
├── remotes.alloy                # Prometheus/Loki remote write (if configured)
├── syslog.alloy                 # System logs via journald
├── block-node.alloy             # Block Node monitoring (if --monitor-block-node)
├── block-node-servicemonitor.yaml  # ServiceMonitor for Block Node metrics
├── block-node-podlogs.yaml      # PodLogs for Block Node logs
├── configmap.yaml               # ConfigMap manifest template
└── namespace.yaml               # Namespace manifest template

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

### Vault + ESO (Optional)

For production or when you want secrets synced automatically from Vault:

```bash
# Start the full stack including Vault
task alloy:start-with-vault

# Configure ClusterSecretStore (from within the VM)
task vault:setup-secret-store
```

Manage Vault separately:
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

In production, the K8s Secret `grafana-alloy-secrets` can be created by any mechanism:

**Option 1: ESO + Vault** — Use `solo-provisioner eso` commands to install ESO and create
ExternalSecret resources that sync passwords from Vault into the K8s Secret automatically.

**Option 2: ESO + Cloud Provider** — Use ESO with AWS Secrets Manager, GCP Secret Manager,
or Azure Key Vault as the backend.

**Option 3: Terraform / CI pipeline** — Create the K8s Secret as part of your infrastructure
provisioning.

**Option 4: Manual** — Create the secret with `kubectl` as shown in the Quick Start.

The only requirement is that the K8s Secret named `grafana-alloy-secrets` exists in namespace
`grafana-alloy` with the expected keys before running `alloy cluster install`.

---

## 📁 Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Container definitions (Prometheus, Loki, Grafana, Vault) |
| `init-vault.sh` | Initialize Vault with dev secrets (used by `task vault:start`) |
| `cluster-secret-store-local.yaml` | ESO → Vault connection template (advanced, optional) |
| `prometheus.yml` | Prometheus configuration |
| `loki-config.yml` | Loki configuration |
| `grafana-datasources.yml` | Grafana datasources |

## 🛠️ Task Reference

| Task | Description |
|------|-------------|
| `task alloy:start` | Start Prometheus, Loki, Grafana |
| `task alloy:start-with-vault` | Start full stack including Vault |
| `task alloy:stop` | Stop all containers |
| `task alloy:clean` | Stop and remove all data |
| `task alloy:create-secret` | Create `grafana-alloy-secrets` K8s Secret with dev passwords |
| `task alloy:delete-secret` | Delete the K8s Secret |
| `task alloy:status` | Show container status |
| `task alloy:logs` | Tail container logs |
| `task vault:start` | Start Vault only |
| `task vault:stop` | Stop Vault |
| `task vault:clean` | Remove Vault data |
| `task vault:setup-secret-store` | Configure ESO ClusterSecretStore → Vault |

