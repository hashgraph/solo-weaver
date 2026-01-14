# Observability for Local Development

This guide explains how observability works for local development with solo-weaver. The observability stack (Prometheus, Loki, Grafana) runs in Docker containers **inside the same VM** as your Kubernetes cluster, providing a complete metrics and logging solution for Block Node and cluster monitoring.

> **TL;DR**: Alloy automatically collects metrics and logs from your Kubernetes cluster (including Block Node) and sends them to Prometheus and Loki. View everything in Grafana with zero authentication.

> ✅ **Good News**: When you run `task vm:reset`, the observability stack is **automatically set up** for you! No separate setup required for local development.

---

## 🚀 Quick Start

### Step 1: Set Up Your VM (Automatic Observability Setup)

From your Mac, reset your VM to get a fresh development environment with observability pre-configured:

```bash
task vm:reset
```

**What this does automatically:**
- ✅ Clones the golden VM to a working instance
- ✅ Configures SSH access with passwordless login
- ✅ Installs Docker
- ✅ **Sets up and starts the observability stack** (Prometheus, Loki, Grafana)
- ✅ Installs MITM proxy CA certificate

**Expected output (excerpt):**
```
✅ VM reset complete with full development environment!

Configured:
  ✅ SSH access with passwordless login
  ✅ MITM proxy with CA certificate
  ✅ Docker installed and configured
  ✅ Observability stack (Prometheus, Loki, Grafana)
```

**Verify observability is running** (optional):
```bash
task vm:ssh
# Inside VM:
docker ps | grep -E "prometheus|loki|grafana"
```

### Step 2: Deploy Block Node with Monitoring

SSH into the VM and deploy Block Node with the pre-configured test config:

```bash
# SSH into VM
task vm:ssh

# Inside VM: Deploy Block Node with monitoring enabled
cp /mnt/solo-weaver/bin/weaver-linux-arm64 ~/.
sudo ~/weaver-linux-arm64 install
sudo weaver block node install -p local -c /mnt/solo-weaver/test/config/config_with_observability.yaml
```

**The test config** (`test/config/config_with_observability.yaml`) is already configured with:
- ✅ Alloy enabled (`enabled: true`)
- ✅ Block Node monitoring enabled (`monitorBlockNode: true`)
- ✅ Correct localhost URLs (`http://localhost:9090/api/v1/write`)
- ✅ Cluster name configured (`vm-cluster`)

**Note:** Prometheus and Loki are already running and ready to receive data when Alloy starts!

### Step 3: Verify Metrics Are Flowing

Inside the VM, check that everything is working:

```bash
# Check pods are running
kubectl get pods -n grafana-alloy    # Alloy should be Running
kubectl get pods -n block-node        # Block Node should be Running

# Verify metrics in Prometheus
METRIC_COUNT=$(curl -s 'http://localhost:9090/api/v1/query?query=%7Bnamespace%3D%22block-node%22%7D' | grep -o '"__name__"' | wc -l)
echo "Found $METRIC_COUNT Block Node metric series"
# Expected: 40+ metric series

# Check Alloy is reporting
curl -s 'http://localhost:9090/api/v1/query?query=up%7Bjob%3D%22alloy%22%7D' | grep -q '"1"' && echo "✅ Alloy is UP" || echo "❌ Alloy is DOWN"
```

### Step 4: Access Grafana from Your Mac

Open a new terminal on your Mac and set up port forwarding:

```bash
# Forward Grafana port
ssh -L 3000:localhost:3000 -i .ssh/id_rsa_vm provisioner@localhost -p 2222

# Or use the convenience task
task vm:observability-forward
```

Then open in your browser: **http://localhost:3000** (no login required!)

#### Query Metrics in Grafana

Select **Prometheus** from the dropdown and try these queries:

```promql
# All Block Node metrics
{namespace="block-node"}

# Block Node memory usage
container_memory_working_set_bytes{pod=~"block-node.*"}

# Block Node CPU usage rate (over 5 minutes)
rate(container_cpu_usage_seconds_total{namespace="block-node"}[5m])

# Thread count
container_threads{pod=~"block-node.*"}

# File system I/O
rate(container_fs_reads_total{namespace="block-node"}[5m])
rate(container_fs_writes_total{namespace="block-node"}[5m])
```

#### Query Logs in Grafana

Select **Loki** from the dropdown and try these queries:

```logql
# All Block Node logs
{namespace="block-node"}

# Error logs only
{namespace="block-node"} |= "ERROR"

# Logs from specific pod
{namespace="block-node", pod="block-node-block-node-server-0"}

# Search for specific patterns
{namespace="block-node"} |= "started"
{namespace="block-node"} |= "listening on port"
{namespace="block-node"} |~ "error|exception|failed"
```

**That's it!** Your Block Node metrics and logs are now flowing to Grafana.

---

### Optional: Custom Configuration

If you need to customize the observability configuration, create your own `config.yaml`:

```yaml
alloy:
  enabled: true
  monitorBlockNode: true

  # For VM-based clusters (kubeadm/k3s) - use localhost
  # Alloy automatically enables hostNetwork for localhost URLs
  prometheusUrl: "http://localhost:9090/api/v1/write"
  prometheusUsername: "local-dev"
  prometheusPassword: "local-dev"

  lokiUrl: "http://localhost:3100/loki/api/v1/push"
  lokiUsername: "local-dev"
  lokiPassword: "local-dev"

  clusterName: "vm-dev-cluster"
```

**Important:** Use `http://localhost:9090` for VM-based clusters. For Kind/k3d clusters, use `http://host.docker.internal:9090`.

### Manual Observability Setup (Advanced)

If you already have a VM and just want to add/restart the observability stack:

```bash
# From your Mac
task vm:observability-setup   # Set up observability in existing VM
task vm:observability-stop    # Stop observability stack
task vm:observability-clean   # Stop and remove all data
```

---

## 🏗️ How It Works

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│  VM (Debian/Ubuntu) - 192.168.x.x                           │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │  Kubernetes Cluster (kubeadm/k3s)                       │ │
│  │                                                          │ │
│  │  ┌──────────────┐   ┌──────────────┐   ┌────────────┐ │ │
│  │  │ Node         │   │ Alloy        │   │ Block Node │ │ │
│  │  │ Exporter     │──▶│ DaemonSet    │◀──│ Pods       │ │ │
│  │  └──────────────┘   │(hostNetwork) │   └────────────┘ │ │
│  │                     └──────────────┘                    │ │
│  │                            │ Scrapes metrics            │ │
│  │                            │ Collects logs              │ │
│  └────────────────────────────┼────────────────────────────┘ │
│                               │ localhost (via hostNetwork) │
│                               ▼                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │  Docker Compose (observability stack)                  │ │
│  │                                                          │ │
│  │  ┌──────────────┐   ┌──────────────┐   ┌────────────┐ │ │
│  │  │ Prometheus   │   │ Loki         │   │ Grafana    │ │ │
│  │  │ :9090        │   │ :3100        │   │ :3000      │ │ │
│  │  │ (Metrics)    │   │ (Logs)       │   │ (UI)       │ │ │
│  │  └──────────────┘   └──────────────┘   └────────────┘ │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
└──────────────────────────────────────────────────────────────┘
         │ SSH port forwarding (for access from Mac)
         ▼
┌─────────────────────────────────────────────────────────────┐
│  Your Mac                                                    │
│  ssh -L 3000:localhost:3000 provisioner@<VM_IP>             │
│  Browser: http://localhost:3000 → Grafana Dashboard         │
└─────────────────────────────────────────────────────────────┘
```

### Data Flow

1. **Grafana Alloy** (DaemonSet) runs on each Kubernetes node with `hostNetwork: true`
2. **Alloy scrapes metrics** from:
   - Node Exporter (system metrics: CPU, memory, disk, network)
   - Block Node pods (via Kubernetes service discovery)
   - cAdvisor (container metrics for all pods)
   - Itself (self-monitoring)

3. **Alloy collects logs** from:
   - Block Node container logs
   - System logs (via /var/log)

4. **Alloy forwards data** to:
   - **Prometheus** (`http://localhost:9090`) - for metrics storage
   - **Loki** (`http://localhost:3100`) - for log aggregation

5. **Grafana** (`http://localhost:3000`) provides a unified UI to query and visualize both metrics and logs

### Key Features

✅ **Zero Authentication** - Grafana has anonymous access enabled for local dev  
✅ **Automatic Discovery** - Alloy automatically discovers Block Node pods  
✅ **hostNetwork Mode** - When using `localhost` URLs, Alloy automatically enables hostNetwork  
✅ **Container Metrics** - Full cAdvisor metrics for CPU, memory, I/O, network  
✅ **Unified View** - Correlate metrics and logs in a single interface  

---

## 🔍 Advanced Verification (Optional)

If you need to troubleshoot or verify the observability stack in more detail:

### Health Checks (Inside VM)

```bash
# Check Prometheus is healthy
curl -sf http://localhost:9090/-/healthy && echo "✅ Prometheus OK" || echo "❌ Prometheus down"

# Check Loki is ready
curl -sf http://localhost:3100/ready && echo "✅ Loki OK" || echo "❌ Loki down"

# Check Grafana is healthy
curl -sf http://localhost:3000/api/health && echo "✅ Grafana OK" || echo "⚠️ Grafana down"

# List available Block Node metrics (sample)
curl -s 'http://localhost:9090/api/v1/label/__name__/values?match[]=%7Bnamespace%3D%22block-node%22%7D' | grep container_ | head -10

# Verify logs in Loki
curl -s 'http://localhost:3100/loki/api/v1/query_range?query=%7Bnamespace%3D%22block-node%22%7D&limit=1' | grep -o '"result":\[.*\]' | wc -c
# Expected: A number > 0
```

---

## 📊 What Metrics Are Available?

After successful deployment, you should see these metric categories in Prometheus:

### Block Node Application Metrics
If Block Node exposes a `/metrics` endpoint (typically on port 8080 or 9090), you'll get application-specific metrics such as:
- **JVM Metrics** (Java-based application):
  - `jvm_memory_used_bytes` - JVM heap and non-heap memory usage
  - `jvm_memory_committed_bytes` - Committed JVM memory
  - `jvm_gc_collection_seconds` - Garbage collection time
  - `jvm_threads_current` - Current thread count
  - `jvm_threads_daemon` - Daemon thread count
- **Application Metrics**:
  - `http_requests_total` - HTTP request counts (if exposed)
  - `http_request_duration_seconds` - Request latency
  - Block-specific metrics (blocks processed, transactions, etc.)
  - Custom business metrics

**Note**: These are automatically discovered and scraped by Alloy when `monitorBlockNode: true` is set in your config.

### Container Metrics (from cAdvisor)
These are **always available** for all pods, including Block Node:
- **CPU**: `container_cpu_usage_seconds_total`, `container_cpu_system_seconds_total`, `container_cpu_user_seconds_total`
- **Memory**: `container_memory_usage_bytes`, `container_memory_working_set_bytes`, `container_memory_rss`, `container_memory_cache`
- **Disk I/O**: `container_fs_reads_total`, `container_fs_writes_total`, `container_fs_reads_bytes_total`, `container_fs_writes_bytes_total`
- **Network**: `container_network_receive_bytes_total`, `container_network_transmit_bytes_total` (if available)
- **Processes**: `container_threads`, `container_processes`

### Node Metrics (from Node Exporter)
System-level metrics for the VM/nodes:
- **System CPU**: `node_cpu_seconds_total` (by mode: idle, system, user, iowait)
- **System Memory**: `node_memory_MemAvailable_bytes`, `node_memory_MemTotal_bytes`, `node_memory_MemFree_bytes`
- **Disk**: `node_disk_io_time_seconds_total`, `node_filesystem_avail_bytes`, `node_filesystem_size_bytes`
- **Network**: `node_network_receive_bytes_total`, `node_network_transmit_bytes_total`
- **Load Average**: `node_load1`, `node_load5`, `node_load15`

### Alloy Metrics (self-monitoring)
- **Scrape Health**: `up{job="alloy"}` - Is Alloy up and scraping successfully
- **Alloy Build Info**: `alloy_build_info` - Version and build information
- **Scrape Duration**: `prometheus_target_scrape_duration_seconds` - How long scrapes take

### Metric Labels

All metrics are automatically labeled with:
- `cluster="vm-dev-cluster"` (or your configured cluster name)
- `namespace="block-node"` (for Block Node specific metrics)
- `pod="block-node-block-node-server-0"` (specific pod name)
- `container="block-node"` (container name within pod)
- `node="debian"` (Kubernetes node name)
- `instance`, `job`, and other Kubernetes labels

This rich labeling allows you to filter and aggregate metrics across different dimensions.

---

## 🎯 Common Use Cases

### Monitor Block Node Performance

```promql
# Memory usage trend
container_memory_working_set_bytes{pod=~"block-node.*"}

# CPU usage percentage
rate(container_cpu_usage_seconds_total{namespace="block-node"}[5m]) * 100

# Is the pod being throttled?
rate(container_cpu_cfs_throttled_seconds_total{namespace="block-node"}[5m])
```

### Track Resource Usage

```promql
# Total cluster memory usage
sum(container_memory_working_set_bytes{namespace=~".*"})

# Per-namespace memory
sum(container_memory_working_set_bytes) by (namespace)

# Disk I/O by pod
rate(container_fs_writes_bytes_total{namespace="block-node"}[5m])
```

### Debug Issues with Logs

```logql
# Find errors in the last hour
{namespace="block-node"} |= "ERROR" 

# Search for exceptions
{namespace="block-node"} |~ "(?i)exception|error|fail"

# Filter by log level (if structured)
{namespace="block-node"} | json | level="ERROR"
```

---

## 📁 Quick Reference

### Essential Commands (From Mac)

```bash
# Set up VM with observability (includes everything!)
task vm:reset

# Manual observability commands (if needed separately):
# Set up observability stack in VM
task vm:observability:start

# Stop observability stack in VM
task vm:observability:stop

# SSH with port forwarding for Grafana access
ssh -L 3000:localhost:3000 -i .ssh/id_rsa_vm provisioner@localhost -p 2222

# Or use the dedicated port forwarding task
task vm:observability-forward

# Access Grafana
open http://localhost:3000
```

### Useful URLs

- **Prometheus**: http://localhost:9090
- **Loki**: http://localhost:3100  
- **Grafana**: http://localhost:3000 (no login required)

---

## 📚 Additional Resources

- **Grafana Alloy Documentation**: https://grafana.com/docs/alloy/
- **Prometheus Query Language**: https://prometheus.io/docs/prometheus/latest/querying/basics/
- **LogQL (Loki Query Language)**: https://grafana.com/docs/loki/latest/logql/
- **Grafana Tutorials**: https://grafana.com/tutorials/

