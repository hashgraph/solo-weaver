# Acceptance Tests

Manual runbook for end-to-end validation of solo-weaver workflows. Each scenario can be run
independently, but they are listed in a logical order. All commands assume a UTM VM environment
on macOS.

## Running

**From inside the VM** (after `task vm:ssh:proxy`):
```bash
task uat:lifecycle  # Core lifecycle: setup → core upgrades → teardown (CI-friendly, no Docker)
task uat:compat     # Backward compatibility (standalone, installs released version first)
task uat:all        # Everything: lifecycle + teleport + alloy + compat (requires Docker)

# Individual steps (for debugging or manual runs):
task uat:setup      # Install cluster + block node (v0.26.0)
task uat:core       # Block node upgrades + reset (requires setup)
task uat:teleport   # Teleport install/uninstall (requires setup + Docker)
task uat:alloy      # Alloy install/uninstall (requires setup + Docker)
task uat:teardown   # Uninstall everything
```

These tasks are designed to run inside any Linux VM — both the local UTM VM
(via `task vm:ssh:proxy`) and CI QEMU VMs (via the `zxc-uat-test.yaml` workflow).

## Prerequisites

- UTM VM set up (`task vm:start`)
- Cache proxy running (`task proxy:start`)
- Binary built (`task build:weaver GOOS=linux GOARCH=arm64`)

## A. Core Block Node Lifecycle

Tests the primary user journey: install → upgrade → reset → uninstall.

### Setup

```bash
# On macOS:
task vm:reset                              # Clean VM state
task build:weaver GOOS=linux GOARCH=arm64  # Build latest binary
task proxy:start                           # Ensure proxy is running
task vm:ssh:proxy                          # SSH with proxy tunnels
```

### Steps

```bash
# Inside the VM:

# 1. Self-install
cd /tmp && cp /mnt/solo-weaver/bin/solo-provisioner-linux-arm64 .
sudo ./solo-provisioner-linux-arm64 install

# 2. Block node install (pins version 0.26.0 via config)
sudo solo-provisioner block node install -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify:**
```bash
kubectl get pods -n block-node          # Pods running
kubectl get pods -n block-node -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.image}{end}{"\n"}{end}'  # Image version
kubectl get pv                          # PVs created
solo-provisioner version                # CLI version matches build
```

```bash
# 3. Upgrade to intermediate version (v0.29.0)
sudo solo-provisioner block node upgrade -p local \
  --chart-version 0.29.0 \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify:**
```bash
helm list -n block-node                 # Chart version is 0.29.0
kubectl get pods -n block-node -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.image}{end}{"\n"}{end}'  # Image shows 0.29.0
```

```bash
# 4. Upgrade to latest (v0.30.2 — version from config file, no CLI flag)
sudo solo-provisioner block node upgrade -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy_latest.yaml
```

**Verify:**
```bash
helm list -n block-node                 # Chart version is 0.30.2
kubectl get pods -n block-node -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.image}{end}{"\n"}{end}'  # Image shows 0.30.2
```

```bash
# 5. Reset block node (clears storage, keeps cluster)
sudo solo-provisioner block node reset -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify:**
```bash
kubectl get pods -n block-node          # Pods running (recreated)
```

```bash
# 6. Block node uninstall
sudo solo-provisioner block node uninstall -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify:**
```bash
kubectl get pods -n block-node          # No pods (or namespace gone)
helm list -n block-node                 # No release
```

```bash
# 7. Kubernetes cluster uninstall
sudo solo-provisioner kube cluster uninstall --continue-on-error
```

**Verify:**
```bash
kubectl get nodes 2>&1                  # Should fail (no cluster)
```

```bash
# 8. Self-uninstall
sudo solo-provisioner uninstall
```

**Verify:**
```bash
which solo-provisioner 2>&1             # Not found
```

---

## B. Teleport Lifecycle

Tests teleport node agent and cluster agent install/uninstall.

### Prerequisites

Block node must be installed (run scenario A steps 1-2 first, or at minimum have a Kubernetes
cluster running).

### Cluster Agent

```bash
# 1. Install cluster agent
sudo solo-provisioner teleport cluster install \
  --values=/mnt/solo-weaver/test/teleport/teleport-values-local.yaml
```

**Verify:**
```bash
kubectl get pods -n teleport-agent      # Pods running
helm list -n teleport-agent             # Release present
```

```bash
# 2. Uninstall cluster agent
sudo solo-provisioner teleport cluster uninstall
```

**Verify:**
```bash
helm list -n teleport-agent             # No release
```

### Node Agent

> Note: Requires a running Teleport proxy server. For local testing, set up Teleport
> via `task vm:teleport:start` first. See Taskfile for details.

```bash
# 3. Install node agent (replace token/proxy with your values)
sudo solo-provisioner teleport node install \
  --token=<join-token> \
  --proxy=<proxy-addr>:3080
```

**Verify:**
```bash
sudo systemctl status teleport          # Active and running
cat /opt/solo/weaver/state/state.yaml | grep -A3 teleportState  # nodeAgent.installed: true
```

```bash
# 4. Uninstall node agent
sudo solo-provisioner teleport node uninstall
```

**Verify:**
```bash
sudo systemctl status teleport 2>&1     # Not found or inactive
cat /opt/solo/weaver/state/state.yaml | grep -A3 teleportState  # nodeAgent.installed: false
```

---

## C. Alloy Lifecycle

Tests Grafana Alloy observability stack install/uninstall.

### Prerequisites

Kubernetes cluster must be running (from scenario A).

```bash
# 1. Install Alloy (no remotes — simplest config)
sudo solo-provisioner alloy cluster install \
  --cluster-name=uat-test
```

**Verify:**
```bash
kubectl get pods -n grafana-alloy       # Pods running
helm list -n grafana-alloy              # grafana-alloy release present
```

```bash
# 2. Uninstall Alloy
sudo solo-provisioner alloy cluster uninstall
```

**Verify:**
```bash
helm list -n grafana-alloy              # No releases
```

---

## D. Proxy Verification

Validates that the cache proxy is correctly routing and caching traffic.

### Setup

```bash
# On macOS:
task proxy:stop
# Remove cached data to start fresh:
docker volume rm cache-proxy_cache-data cache-proxy_registry-data cache-proxy_goproxy-cache 2>/dev/null; true
task proxy:start
```

### Steps

```bash
# On macOS (separate terminal) — watch traffic:
docker exec solo-weaver-cache-proxy tail -f /var/log/squid/access.log
```

```bash
# On macOS:
task vm:ssh:proxy

# Inside VM:
# 1. First run — downloads go through proxy (TCP_MISS)
sudo solo-provisioner block node install -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify (in Squid log):** Lines showing `TCP_MISS` for download URLs (cdn.dl.k8s.io, get.helm.sh, etc.)

```bash
# 2. Reset and reinstall — should hit cache (TCP_HIT)
sudo solo-provisioner kube cluster uninstall --continue-on-error
sudo solo-provisioner block node install -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify (in Squid log):** Lines showing `TCP_HIT` for the same URLs.

```bash
# On macOS:
task proxy:status                       # Shows cache sizes and hit ratios
```

---

## E. Backward Compatibility

Tests upgrading the CLI binary while preserving state from a prior version.

### Setup

```bash
# On macOS:
task vm:reset
task vm:ssh:proxy
```

### Steps

```bash
# Inside VM:

# 1. Install a released version (download from GitHub releases)
curl -sSL https://raw.githubusercontent.com/hashgraph/solo-weaver/main/install.sh | bash

# 2. Deploy block node with the released version
sudo solo-provisioner block node install -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify:**
```bash
solo-provisioner version                # Shows released version
kubectl get pods -n block-node          # Running
cat /opt/solo/weaver/state/state.yaml   # State file exists with current schema
```

```bash
# 3. Upgrade CLI to the new binary (from current branch)
cd /tmp && cp /mnt/solo-weaver/bin/solo-provisioner-linux-arm64 .
sudo ./solo-provisioner-linux-arm64 install
```

**Verify:**
```bash
solo-provisioner version                # Shows new version (0.0.0 for dev builds)
```

```bash
# 4. Run an upgrade — should trigger startup migrations if needed
sudo solo-provisioner block node upgrade -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy_latest.yaml
```

**Verify:**
```bash
kubectl get pods -n block-node          # Running with updated version
cat /opt/solo/weaver/state/state.yaml   # State file version matches current schema
# Check logs for migration messages:
grep -i migration /opt/solo/weaver/logs/solo-provisioner.log
```

---

## Quick Reference

| Scenario | Duration | Dependencies |
|----------|----------|-------------|
| A. Core Lifecycle | ~10 min | VM, proxy |
| B. Teleport | ~5 min | Kubernetes cluster |
| C. Alloy | ~3 min | Kubernetes cluster |
| D. Proxy Verification | ~15 min | VM, proxy (fresh caches) |
| E. Backward Compat | ~15 min | VM, proxy, released binary |
