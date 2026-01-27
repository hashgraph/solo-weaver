# Alloy Stack - Grafana Alloy with Vault

Complete Alloy observability stack for Solo Weaver with Vault-managed secrets.

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

### Prerequisites: Build Weaver

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

Copy the weaver binary:
```bash
cp /mnt/solo-weaver/bin/weaver-linux-arm64 ~/.

sudo ~/weaver-linux-arm64 install
```

Inside the VM (`task vm:ssh`):
```bash
sudo weaver block node install \
  --profile=local \
  --config=/mnt/solo-weaver/test/config/config.yaml
```

### Step 3: Connect Vault

Inside the VM:
```bash
task vault:setup-secret-store
```

### Step 4: Add Alloy

Inside the VM:
```bash
sudo weaver block node install \
  --profile=local \
  --config=/mnt/solo-weaver/test/config/config_with_alloy.yaml
```

Wait ~30 seconds for secrets to sync, then check pods:
```bash
kubectl get pods -n grafana-alloy
# Should show Running after secret sync completes
```

### Step 5: Access Grafana

From your Mac:
```bash
task vm:alloy-forward
```

Open http://localhost:3000

**Note:** Anonymous authentication is enabled - you'll be logged in automatically as Admin. No username/password required.

### Step 6: Verify in Grafana

Navigate to **Explore** and test these queries:

**Prometheus queries:**
```promql
# Alloy is running
up{job="alloy"}

# Node metrics
node_cpu_seconds_total

# Block Node metrics (if monitorBlockNode: true)
{namespace="block-node"}
```

**Loki queries** (switch datasource to Loki):
```logql
# System logs (journald)
{cluster="vm-cluster"}

# Alloy logs
{namespace="grafana-alloy"}

# Block Node logs
{namespace="block-node"}
```

---

## 🧹 Cleanup

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

**When `monitorBlockNode: true`:**
- ✅ Block Node application metrics (via ServiceMonitor)
- ✅ Block Node pod logs
- ✅ Block Node container metrics (CPU, memory, disk via cAdvisor)

---

## 🔧 Advanced

### Manage Vault Separately

```bash
task vault:start   # Start Vault only
task vault:stop    # Stop Vault
task vault:clean   # Remove Vault data
```

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
| `config_with_alloy.yaml` | Weaver config with Alloy enabled |
| `prometheus.yml` | Prometheus configuration |
| `loki-config.yml` | Loki configuration |
| `grafana-datasources.yml` | Grafana datasources |

