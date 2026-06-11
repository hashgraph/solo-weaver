# solo-provisioner-daemon Testing Guide

> **Audience**: human testers running manual acceptance tests on a Linux node (UTM VM or real machine).
> For developer architecture details see [daemon-architecture.md](daemon-architecture.md).

## Reset: clean up a prior test run

If you have run tests before on this machine, clean up all leftover state before starting a
new session. Copy-paste the block below; each command is safe to run even if the resource does
not exist.

```bash
export ORBIT=consensus

# Stop and fully uninstall the daemon
sudo solo-provisioner daemon service uninstall --non-interactive 2>/dev/null || true

# Remove any config/binary left by a partial run
sudo rm -f /opt/solo/weaver/config/daemon.yaml
sudo rm -f /opt/solo/weaver/config/daemon-cn.kubeconfig
sudo rm -f /opt/solo/weaver/bin/solo-provisioner-daemon

# Remove RBAC created by previous installs (block-node RBAC deferred — no BN SA/CR/CRB yet)
sudo kubectl delete clusterrolebinding solo-provisioner-daemon-cn 2>/dev/null || true
sudo kubectl delete clusterrole solo-provisioner-daemon-cn 2>/dev/null || true
sudo kubectl delete serviceaccount solo-provisioner-daemon-cn -n $ORBIT 2>/dev/null || true

# Remove any lingering upgrade CRs and the NUE CRD itself
sudo kubectl delete networkupgradeexecute --all -n $ORBIT 2>/dev/null || true
sudo kubectl delete crd networkupgradeexecutes.hedera.com 2>/dev/null || true

# Remove daemon event logs (migration soak state, upgrade event logs)
sudo rm -rf /opt/solo/weaver/daemon/events/

# Restore upgrade staging dir if it was renamed by TC-19
sudo mv /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current.bak \
        /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current 2>/dev/null || true

echo "=== Reset complete ==="
systemctl is-active solo-provisioner-daemon.service 2>&1       # expected: inactive
ls /opt/solo/weaver/config/daemon.yaml 2>/dev/null || echo "daemon.yaml absent (good)"
```

> **Build tip**: the daemon binary file stays busy in the OS page cache right after `scp`.
> Copy it to `/tmp` before using it with `--daemon-bin` to avoid a "text file busy" error:
> ```bash
> cp ~/solo-provisioner-daemon-linux-amd64 /tmp/solo-provisioner-daemon-new
> chmod +x /tmp/solo-provisioner-daemon-new
> export DAEMON_BIN=/tmp/solo-provisioner-daemon-new
> ```

---

## Quick-start: full session setup

Copy-paste the block below on a **fresh Linux VM** to prepare everything the test suite needs.
Run it once before executing any test case. Skip steps you have already completed.

```bash
# ── 0. Build (on your dev machine, not the VM) ─────────────────────────────
task build:cli   GOOS=linux GOARCH=amd64
task build:daemon GOOS=linux GOARCH=amd64

# Copy both binaries to the VM.  Wait for scp to finish before continuing.
gcloud compute scp bin/solo-provisioner-linux-amd64 \
                    bin/solo-provisioner-daemon-linux-amd64 <VM_NAME>:~/

# ── 0b. On the VM — move daemon binary away from the scp landing path ──────
# scp leaves the file open in the kernel page cache; running it directly from ~/
# causes "text file busy".  Copy to /tmp first.
cp ~/solo-provisioner-daemon-linux-amd64 /tmp/solo-provisioner-daemon-new
chmod +x /tmp/solo-provisioner-daemon-new

# ── 1. Environment ─────────────────────────────────────────────────────────
export ORBIT=consensus        # orbit namespace (consensus-node CR namespace)
export NODE_ID=0              # numeric consensus-node ID
export SOCK=/opt/solo/weaver/daemon/daemon.sock
export DAEMON_BIN=/tmp/solo-provisioner-daemon-new   # use the /tmp copy

# ── 2. Install solo-provisioner CLI ────────────────────────────────────────
# Run the CLI installer from /tmp to avoid the same "text file busy" issue.
cp ~/solo-provisioner-linux-amd64 /tmp/solo-provisioner-new
chmod +x /tmp/solo-provisioner-new
sudo /tmp/solo-provisioner-new install --non-interactive

# ── 3. Bootstrap a single-node Kubernetes cluster ─────────────────────────
sudo solo-provisioner kube cluster install \
  --profile local \
  --node-type block \
  --non-interactive

# Verify:
sudo kubectl get nodes
# expected: NAME   STATUS   ROLES           AGE   VERSION
#           ...    Ready    control-plane   ...   v1.x.y

# ── 4. Create the orbit namespace ─────────────────────────────────────────
sudo kubectl create ns $ORBIT

# ── 5. hedera group / user + upgrade staging directory ────────────────────
sudo groupadd -g 2000 hedera 2>/dev/null || true
sudo useradd -r -u 2000 -g hedera -s /sbin/nologin hedera 2>/dev/null || true
sudo mkdir -p /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
sudo chown hedera:hedera \
  /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade \
  /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
sudo chmod 0775 /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current

# Add weaver to hedera group (re-login not needed; daemon exec picks it up)
sudo usermod -aG hedera weaver

# Verify write access:
sudo -u weaver touch /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current/.probe && echo OK
sudo -u weaver rm /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current/.probe

# ── 6. Apply NetworkUpgradeExecute CRD (required for TC-23 – TC-26) ───────
# Option A — from repo checkout:
#   sudo kubectl apply -f hack/crds/networkupgradeexecute.yaml
# Option B — inline (no repo checkout needed):
sudo kubectl apply -f - <<'CRDEOF'
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: networkupgradeexecutes.hedera.com
spec:
  group: hedera.com
  names:
    kind: NetworkUpgradeExecute
    listKind: NetworkUpgradeExecuteList
    plural: networkupgradeexecutes
    singular: networkupgradeexecute
    shortNames:
      - nue
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              required:
                - operationId
                - orbit
              properties:
                operationId:
                  type: string
                orbit:
                  type: string
                upgradeFileHash:
                  type: string
                upgradeFileUrl:
                  type: string
            status:
              type: object
              properties:
                phase:
                  type: string
                  enum:
                    - Pending
                    - ReadyForProvisionerDaemon
                    - InProgress
                    - Completed
                    - Failed
                message:
                  type: string
      additionalPrinterColumns:
        - name: Phase
          type: string
          jsonPath: .status.phase
        - name: OperationID
          type: string
          jsonPath: .spec.operationId
        - name: Age
          type: date
          jsonPath: .metadata.creationTimestamp
CRDEOF

# Verify:
sudo kubectl get crd networkupgradeexecutes.hedera.com
# expected: networkupgradeexecutes.hedera.com   <age>
```

Once all six steps complete, proceed to TC-01.

---

## Prerequisites

Before running any test case, confirm the following are in place:

- [ ] Linux host (x86-64 or arm64) with systemd
- [ ] `solo-provisioner` binary installed and on `$PATH`
- [ ] `solo-provisioner-daemon` binary available (either auto-downloaded during install or supplied via `--daemon-bin`)
- [ ] A reachable Kubernetes cluster (`kubectl cluster-info` succeeds with the default kubeconfig)
- [ ] `curl` available for HTTP control-plane tests
- [ ] Running as a user with `sudo` access (some steps require root for systemd and `/usr/lib/systemd/system/`)

### Orbit namespace

The `daemon service install` RBAC step creates a ServiceAccount inside the orbit namespace.
If the namespace does not exist the install will fail with `namespaces "$ORBIT" not found`.

Create it before running any install test case:
```bash
sudo kubectl create ns $ORBIT
# e.g.: sudo kubectl create ns consensus
```

### NetworkUpgradeExecute CRD (required for TC-23 – TC-26)

The upgrade monitor watches `NetworkUpgradeExecute` CRs. If the CRD is not installed the
monitor enters exponential backoff and TC-23 – TC-26 cannot run.

A minimal CRD is bundled at `hack/crds/networkupgradeexecute.yaml`. Apply it before running
TC-23 – TC-26:

```bash
# From the repo root:
sudo kubectl apply -f hack/crds/networkupgradeexecute.yaml

# Verify:
kubectl get crd networkupgradeexecutes.hedera.com
# expected: networkupgradeexecutes.hedera.com   <age>
```

If you are running on a fresh VM **without** a local repo checkout, apply the CRD inline:

```bash
sudo kubectl apply -f - <<'EOF'
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: networkupgradeexecutes.hedera.com
spec:
  group: hedera.com
  names:
    kind: NetworkUpgradeExecute
    listKind: NetworkUpgradeExecuteList
    plural: networkupgradeexecutes
    singular: networkupgradeexecute
    shortNames:
      - nue
  scope: Namespaced
  versions:
    - name: v1alpha1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              required:
                - operationId
                - orbit
              properties:
                operationId:
                  type: string
                orbit:
                  type: string
                upgradeFileHash:
                  type: string
                upgradeFileUrl:
                  type: string
            status:
              type: object
              properties:
                phase:
                  type: string
                  enum:
                    - Pending
                    - ReadyForProvisionerDaemon
                    - InProgress
                    - Completed
                    - Failed
                message:
                  type: string
      additionalPrinterColumns:
        - name: Phase
          type: string
          jsonPath: .status.phase
        - name: OperationID
          type: string
          jsonPath: .spec.operationId
        - name: Age
          type: date
          jsonPath: .metadata.creationTimestamp
EOF
```

Wait ~60 s for the upgrade monitor to pick up the CRD and log `reason=UpgradeMonitorWatchEstablished`
before running TC-23:

```bash
journalctl -u solo-provisioner-daemon.service -f | grep -m1 UpgradeMonitorWatchEstablished
```

### Consensus-node prerequisite: upgrade staging directory

The daemon's **startup probe** for the consensus-node component actively verifies the upgrade
staging directory before it signals `READY` to systemd. If these conditions are not met the
daemon will fail to start (systemd marks it failed after `TimeoutStartSec`).

**Required before running TC-01, TC-02, TC-03, TC-04, TC-12, TC-15, TC-18:**

1. The consensus node (`/opt/hgcapp`) must already be installed on the host.

2. The upgrade staging directory must exist with correct ownership:
   ```bash
   # Parent dir — owned hedera:hedera, mode 0755
   ls -la /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade
   # expected: drwxr-xr-x hedera hedera

   # Active staging dir — owned hedera:hedera, mode 0775 (g+rwx)
   ls -la /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
   # expected: drwxrwxr-x hedera hedera
   ```

3. The daemon's runtime user (`weaver`) must be a member of the `hedera` group:
   ```bash
   groups weaver
   # expected output includes: hedera
   ```
   If not, add it and log out/in (or reboot) to apply:
   ```bash
   sudo usermod -aG hedera weaver
   ```

4. Verify write access as the daemon user:
   ```bash
   sudo -u weaver touch /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current/.probe && echo OK
   sudo -u weaver rm /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current/.probe
   # expected: OK
   ```

> **TC-16 (block-node only)** is the only install test that does **not** need the above — it
> enables block-node without consensus-node, so the upgrade dir probe is never triggered.

### Notation

- `$ORBIT` — the Kubernetes namespace for consensus-node CRs (e.g. `hedera-network`)
- `$NODE_ID` — the numeric consensus-node identifier (e.g. `0`, `1`, `2`)
- `$SOCK` — daemon Unix socket: `/opt/solo/weaver/daemon/daemon.sock`
- Commands prefixed `#` require root / sudo.

---

## TC-01 — Fresh install (interactive prompts)

**Goal**: verify that `daemon service install` collects all required fields interactively, writes
`daemon.yaml`, provisions RBAC, writes a scoped kubeconfig, installs the binary, and starts the service.

### Steps

1. Confirm no prior install exists:
   ```bash
   systemctl is-active solo-provisioner-daemon.service
   # expected: inactive or "Unit solo-provisioner-daemon.service could not be found"
   ls /opt/solo/weaver/config/daemon.yaml 2>/dev/null && echo EXISTS || echo MISSING
   # expected: MISSING
   ```
2. Run the interactive install:
   ```bash
   sudo solo-provisioner daemon service install
   ```
3. When prompted, select `consensus-node` component (press Y). Enter:
   - **Consensus Node ID**: a numeric value such as `0`, `1`, or `2` (not an account ID like `0.0.3`)
   - **Orbit Namespace**: `$ORBIT`
   - **Upgrade Dir**: accept the default (`/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current`)
     or enter a custom path — **this directory must already exist and satisfy the ownership
     requirements in the Prerequisites section above, otherwise the daemon will fail to start**
4. Confirm the summary table and allow the workflow to run.

   > **Non-interactive equivalent** (skips all prompts — use this for scripted runs or to verify
   > the flags before running TC-02):
   > ```bash
   > sudo solo-provisioner daemon service install \
   >   --components consensus-node \
   >   --cn-node-id $NODE_ID \
   >   --cn-orbit $ORBIT \
   >   --daemon-bin $DAEMON_BIN \
   >   --non-interactive
   > ```

### Expected results

- [ ] Workflow completes without error; final line: `solo-provisioner-daemon service installed, enabled, and started`
- [ ] `daemon.yaml` written:
  ```bash
  cat /opt/solo/weaver/config/daemon.yaml
  # must contain schema_version: 1, node_id (numeric), orbit, kubeconfig fields
  ```
- [ ] Scoped kubeconfig written:
  ```bash
  ls -la /opt/solo/weaver/config/daemon-cn.kubeconfig
  # mode is 0640 (root:weaver) — readable only by root and the weaver service account
  ```
- [ ] Daemon binary placed at `/opt/solo/weaver/bin/solo-provisioner-daemon`:
  ```bash
  ls -la /opt/solo/weaver/bin/solo-provisioner-daemon
  ```
- [ ] Service is running:
  ```bash
  systemctl is-active solo-provisioner-daemon.service
  # expected: active
  ```
- [ ] Unit file symlink in place:
  ```bash
  ls -la /usr/lib/systemd/system/solo-provisioner-daemon.service
  # must be a symlink pointing into $HOME/sandbox/usr/lib/systemd/system/
  ```
- [ ] RBAC resources exist:
  ```bash
  kubectl get serviceaccount solo-provisioner-daemon-cn -n $ORBIT
  kubectl get clusterrole solo-provisioner-daemon-cn
  kubectl get clusterrolebinding solo-provisioner-daemon-cn
  ```

---

## TC-02 — Fresh install (non-interactive, flags only)

**Goal**: verify that all required fields can be supplied via CLI flags, bypassing prompts.

### Steps

1. Uninstall if already installed (see TC-07).
2. Run (fully non-interactive — no prompts):
   ```bash
   sudo solo-provisioner daemon service install \
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT \
     --daemon-bin $DAEMON_BIN \
     --non-interactive
   ```
   where `$NODE_ID` is a numeric value (e.g. `0`) and `$DAEMON_BIN` is the path to the
   `solo-provisioner-daemon` binary (see Quick-start section).

   > **Without a local binary** (lets the CLI auto-download from the catalog):
   > ```bash
   > sudo solo-provisioner daemon service install \
   >   --components consensus-node \
   >   --cn-node-id $NODE_ID \
   >   --cn-orbit $ORBIT \
   >   --non-interactive
   > ```
   > Note: auto-download requires a real GitHub release tag. Version `0.0.0` will fail with
   > resolution hints (see TC-21). Use `--daemon-bin` when testing against a dev build.

### Expected results

- [ ] No interactive prompts appear
- [ ] Same post-conditions as TC-01

---

## TC-03 — Install with `--from-config`

**Goal**: verify that a pre-built `daemon.yaml` is accepted as-is.

### Steps

1. Create `/tmp/daemon-test.yaml`:
   ```yaml
   schema_version: 1
   components:
     consensus_node:
       enabled: true
       kubeconfig: /opt/solo/weaver/config/daemon-cn.kubeconfig
       node_id: "$NODE_ID"
       orbit: "$ORBIT"
       monitors:
         upgrade: true
         migration: true
   ```
   (`$NODE_ID` must be a numeric string such as `"0"`.)
2. Uninstall if already installed (see TC-07).
3. Run:
   ```bash
   sudo solo-provisioner daemon service install --from-config /tmp/daemon-test.yaml
   ```

### Expected results

- [ ] `daemon.yaml` at `/opt/solo/weaver/config/daemon.yaml` matches the supplied file
- [ ] Service is running
- [ ] No prompt appeared

---

## TC-04 — Re-install with flag override

**Goal**: verify that running install on an existing `daemon.yaml` applies flag overrides and
updates the file without re-prompting.

### Steps

1. Ensure the daemon is already installed (TC-01 or TC-02).
2. Run:
   ```bash
   sudo solo-provisioner daemon service install --cn-orbit new-orbit
   ```

### Expected results

- [ ] No prompts appear
- [ ] `daemon.yaml` now has `orbit: new-orbit`:
  ```bash
  grep orbit /opt/solo/weaver/config/daemon.yaml
  # expected: orbit: new-orbit
  ```
- [ ] Service restarted and is active

---

## TC-05 — `daemon service check`

**Goal**: verify that the check command reports service health correctly and prints the full
`/status` JSON so the operator can see component state.

> **Note**: this command requires root because it connects to the Unix socket at
> `/opt/solo/weaver/daemon/daemon.sock`. Run it with `sudo`.

### Steps

1. Ensure the daemon is installed and running.
2. Run:
   ```bash
   sudo solo-provisioner daemon service check
   # SOCK is set automatically; the command connects to /opt/solo/weaver/daemon/daemon.sock
   ```

### Expected results

- [ ] Workflow steps pass: unit file present, symlink valid, service enabled, service active, Unix socket responding
- [ ] `/status` JSON is printed to stdout after the workflow completes, e.g.:
  ```json
  {
    "components": {
      "consensus-node": {
        "monitors": {
          "upgrade-monitor":   {"state": "running"},
          "migration-monitor": {"state": "running"}
        }
      }
    }
  }
  ```
- [ ] Exit code 0

### Negative check — service stopped

1. Stop the service: `sudo solo-provisioner daemon service stop`
2. Run `sudo solo-provisioner daemon service check`
3. Expected: reports service as **inactive**; exit code non-zero

### Negative check — run without sudo

1. Run without root: `solo-provisioner daemon service check`
2. Expected: exits early with a privileges error before attempting the socket; exit code non-zero

---

## TC-06 — `daemon service start` and `stop`

**Goal**: verify manual start/stop commands work independently of install/uninstall.

### Steps

1. Ensure daemon is installed.
2. Stop:
   ```bash
   sudo solo-provisioner daemon service stop
   systemctl is-active solo-provisioner-daemon.service   # expected: inactive
   ```
3. Start:
   ```bash
   sudo solo-provisioner daemon service start
   systemctl is-active solo-provisioner-daemon.service   # expected: active
   ```

### Expected results

- [ ] Stop command exits 0 and service becomes inactive
- [ ] Start command exits 0 and service becomes active

---

## TC-07 — Uninstall

**Goal**: verify that uninstall stops the service, removes the unit file, daemon binary, kubeconfig,
and K8s RBAC resources.

### Steps

1. Ensure daemon is installed and running.
2. Run:
   ```bash
   sudo solo-provisioner daemon service uninstall
   ```

### Expected results

- [ ] Service stopped and removed:
  ```bash
  systemctl is-active solo-provisioner-daemon.service
  # expected: inactive or not found
  ```
- [ ] Symlink removed: `ls /usr/lib/systemd/system/solo-provisioner-daemon.service` → not found
- [ ] Daemon binary removed:
  ```bash
  ls /opt/solo/weaver/bin/solo-provisioner-daemon
  # expected: No such file or directory
  ```
- [ ] Kubeconfig removed: `ls /opt/solo/weaver/config/daemon-cn.kubeconfig` → not found
- [ ] RBAC resources removed:
  ```bash
  kubectl get serviceaccount solo-provisioner-daemon-cn -n $ORBIT   # expected: NotFound
  kubectl get clusterrole solo-provisioner-daemon-cn                 # expected: NotFound
  kubectl get clusterrolebinding solo-provisioner-daemon-cn          # expected: NotFound
  ```

---

## TC-08 — HTTP control plane: `/health`

**Goal**: verify the daemon's Unix-socket HTTP server responds while alive.

### Steps

1. Ensure the daemon is running.
2. Run:
   ```bash
   sudo curl --unix-socket $SOCK http://localhost/health
   ```

### Expected result

- [ ] HTTP 200, body: `{"status":"ok"}`

---

## TC-09 — HTTP control plane: `/status`

**Goal**: verify that per-monitor runtime state is reported correctly.

### Steps

1. Ensure the daemon is running with consensus-node enabled.
2. Run:
   ```bash
   sudo curl --unix-socket $SOCK http://localhost/status | python3 -m json.tool
   ```

### Expected result

```json
{
  "components": {
    "consensus-node": {
      "monitors": {
        "upgrade-monitor":   {"state": "running"},
        "migration-monitor": {"state": "running"}
      }
    }
  }
}
```

- [ ] Both monitors show `"state": "running"`
- [ ] If block-node component is also enabled, a `"block-node"` key appears with `bn-upgrade-monitor: running`

---

## TC-10 — HTTP control plane: migration soak status

**Goal**: verify the soak status endpoint reflects the idle state before any soak is started.

### Steps

1. Ensure the daemon is running with `migration: true` in `daemon.yaml`.
2. Run:
   ```bash
   sudo curl --unix-socket $SOCK http://localhost/consensus_node/migration/soak/status
   ```

### Expected result

- [ ] HTTP 200, body is a JSON object describing idle soak state (no active soak)

---

## TC-11 — Migration soak start (idempotency)

**Goal**: verify that `POST /consensus_node/migration/soak/start` starts a soak and that a duplicate
call returns HTTP 409.

### Steps

1. Ensure the daemon is running with migration monitor enabled.
2. Start a soak (a JSON body with `node_id`, `cutover_timestamp`, and `migration_plan_path` is
   required — a bare POST with no body returns 400):
   ```bash
   sudo curl -s -o /dev/null -w "%{http_code}" \
     -X POST --unix-socket $SOCK \
     -H 'Content-Type: application/json' \
     -d '{"node_id":"0","cutover_timestamp":"2024-01-15T00:00:00Z","migration_plan_path":"/tmp/plan.yaml"}' \
     http://localhost/consensus_node/migration/soak/start
   # expected: 202
   ```
3. Immediately send a second request:
   ```bash
   sudo curl -s -o /dev/null -w "%{http_code}" \
     -X POST --unix-socket $SOCK \
     -H 'Content-Type: application/json' \
     -d '{"node_id":"0","cutover_timestamp":"2024-01-15T00:00:00Z","migration_plan_path":"/tmp/plan.yaml"}' \
     http://localhost/consensus_node/migration/soak/start
   # expected: 409
   ```
4. Check soak status:
   ```bash
   sudo curl --unix-socket $SOCK http://localhost/consensus_node/migration/soak/status
   ```

### Expected results

- [ ] First call: 2xx response, soak becomes active
- [ ] Second call: HTTP 409 (conflict — soak already active)
- [ ] Status endpoint shows soak as in-progress

---

## TC-12 — Daemon survives monitor crash (supervised restart)

**Goal**: verify that a crashing monitor is restarted by `supervisedMonitor` without taking down
the daemon process or the HTTP server.

> This test requires access to the daemon log.

### Steps

1. Ensure the daemon is running.
2. Confirm `/health` responds: `sudo curl --unix-socket $SOCK http://localhost/health`
3. Simulate a monitor crash by killing the process the monitor is watching, or by
   temporarily revoking RBAC permissions (delete the ClusterRoleBinding and wait for the
   watch to fail):
   ```bash
   kubectl delete clusterrolebinding solo-provisioner-daemon-cn
   ```
4. Wait ~10 seconds, then check the log:
   ```bash
   journalctl -u solo-provisioner-daemon.service -n 50
   # look for: reason=MonitorCrash, reason=MonitorStopped, or backoff entries
   ```
5. Check `/health` is still responding:
   ```bash
   sudo curl --unix-socket $SOCK http://localhost/health
   # expected: {"status":"ok"}
   ```
6. Restore the ClusterRoleBinding:
   ```bash
   sudo solo-provisioner daemon service install   # re-provisions RBAC idempotently
   ```

### Expected results

- [ ] Log shows `MonitorCrash` followed by `backoff:<duration>`
- [ ] Daemon process remains alive; `/health` keeps returning 200
- [ ] After RBAC is restored, monitor recovers and `/status` shows `running` again

---

## TC-13 — Config schema migration (v0 → v1)

**Goal**: verify that a `daemon.yaml` without `schema_version` (pre-versioning format) is loaded
without error and treated as version 1.

### Steps

1. Stop the daemon: `sudo solo-provisioner daemon service stop`
2. Edit `/opt/solo/weaver/config/daemon.yaml` and remove the `schema_version:` line entirely.
3. Start the daemon: `sudo solo-provisioner daemon service start`
4. Check: `sudo curl --unix-socket $SOCK http://localhost/health`

### Expected result

- [ ] Daemon starts successfully; `/health` returns 200
- [ ] Log does NOT contain any schema error

---

## TC-14 — Config from newer binary rejected

**Goal**: verify that a `daemon.yaml` stamped with a future `schema_version` is rejected with a
clear error rather than silently corrupting state.

### Steps

1. Stop the daemon.
2. Edit `/opt/solo/weaver/config/daemon.yaml`, change `schema_version:` to `99`.
3. Attempt to start the daemon:
   ```bash
   solo-provisioner-daemon --config /opt/solo/weaver/config/daemon.yaml
   ```

### Expected result

- [ ] Daemon exits immediately with an error containing:
  `written by a newer binary (schema_version 99 > supported 1)`
- [ ] Exit code is non-zero

---

## TC-15 — Block-node component stub (consensus-node + block-node together)

**Goal**: verify that enabling the block-node component at install time writes the `block_node`
section to `daemon.yaml` and starts the stub traffic-shaper monitor alongside consensus-node.

> **Note**: block-node kubeconfig and RBAC provisioning are deferred — the traffic-shaper
> monitor is currently stubbed and polls a remote API only (no K8s watch needed).

### Steps

1. Uninstall if already installed.
2. Run:
   ```bash
   sudo solo-provisioner daemon service install \
     --components consensus-node,block-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT
   ```

### Expected results

- [ ] Install completes without error
- [ ] `daemon.yaml` contains both `consensus_node` and `block_node` blocks
- [ ] `/status` shows `block-node` component:
  ```bash
  sudo curl --unix-socket $SOCK http://localhost/status | python3 -m json.tool
  # "block-node": { "monitors": { "bn-traffic-shaper-monitor": { "state": "running" } } }
  ```
- [ ] Log contains:
  `block-node traffic-shaper monitor not yet implemented — stub running`

---

## TC-16 — Block-node only (no consensus-node)

**Goal**: verify that a block-node-only config is valid and the daemon starts without a
consensus-node block.

> **Note**: `--bn-orbit` is no longer required while the traffic-shaper monitor is stubbed.

### Steps

1. Uninstall if already installed.
2. Run:
   ```bash
   sudo solo-provisioner daemon service install \
     --components block-node
   ```

### Expected results

- [ ] Install completes without error
- [ ] `daemon.yaml` has `block_node` block and **no** `consensus_node` block
- [ ] `/health` returns 200
- [ ] `/status` shows only `block-node` component

---

## TC-17 — Daemon restart on crash (systemd `Restart=always`)

**Goal**: verify systemd automatically restarts the daemon after an unexpected process exit.

### Steps

1. Ensure the daemon is running.
2. Force-kill the process:
   ```bash
   kill -9 $(systemctl show -p MainPID solo-provisioner-daemon.service | cut -d= -f2)
   ```
3. Wait 3–5 seconds, then check:
   ```bash
   systemctl is-active solo-provisioner-daemon.service
   ```

### Expected result

- [ ] Service transitions through `activating` back to `active` within a few seconds
- [ ] `journalctl -u solo-provisioner-daemon.service -n 20` shows a restart event

---

## TC-18 — Install blocked when daemon is already running

**Goal**: verify that `daemon service install` exits with a clear, actionable error when the
daemon service is already running, rather than attempting to overwrite an in-use binary.
This is intentional behaviour — `copyBinaryFile` uses `O_TRUNC` which fails with
"text file busy" on an active executable. The operator must stop the service first.

### Steps

1. Ensure daemon is installed and running:
   ```bash
   sudo solo-provisioner daemon service check
   ```
2. Attempt to re-run install without stopping first:
   ```bash
   sudo solo-provisioner daemon service install \
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT
   ```

### Expected results

- [ ] Command exits non-zero with a message containing "already running"
- [ ] Resolution hint directs operator to `daemon service stop` then `daemon service install`
- [ ] Service remains running and unmodified after the failed attempt

### Correct operator flow when re-install is needed

```bash
sudo solo-provisioner daemon service stop
sudo solo-provisioner daemon service install \
  --components consensus-node \
  --cn-node-id $NODE_ID \
  --cn-orbit $ORBIT
sudo solo-provisioner daemon service check
```

---

## TC-19 — Startup probe failure: missing or misconfigured upgrade directory

**Goal**: verify that when the consensus-node upgrade staging directory is absent or has wrong
ownership, the daemon fails to reach `READY`, the probe failure is clearly logged, the operator
can diagnose from logs, fix the issue, and recover by reinstalling.

> This test intentionally puts the node into a broken state. Read all steps before starting.

### Setup — introduce a bad state

1. If the daemon is currently installed and running, uninstall it first (TC-07).
2. Rename the upgrade directory to simulate it being absent:
   ```bash
   sudo mv /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current \
           /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current.bak
   ```
   Alternatively, to test wrong ownership instead of absence:
   ```bash
   sudo chown root:root /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
   ```

### Steps

1. Install the daemon (non-interactive):
   ```bash
   sudo solo-provisioner daemon service install \
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT
   ```
   The install workflow itself should succeed (it provisions RBAC and writes config before
   starting the service). The service starts but will fail its readiness probe.

2. Check service state — it should be stuck in `activating` or transition to `failed`:
   ```bash
   systemctl status solo-provisioner-daemon.service
   # expected: activating (start) or failed
   ```

3. Inspect the journal for the probe failure:
   ```bash
   journalctl -u solo-provisioner-daemon.service -n 60
   ```
   Look for log lines containing any of:
   - `ComponentProbeAborted` — the composite probe gave up
   - `DiskOwnershipProbe` / `DiskWriteTestProbe` — the specific failing leaf probe
   - `reason=ComponentProbeAborted` with `component=consensus-node`

4. Identify the fix from the log message (missing dir, wrong owner, no write access).

5. Fix the issue:
   ```bash
   # If you renamed the directory:
   sudo mv /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current.bak \
           /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current

   # If you changed ownership:
   sudo chown hedera:hedera /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
   sudo chmod 0775 /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
   ```

6. Reinstall to restart the daemon with the corrected environment:
   ```bash
   sudo solo-provisioner daemon service uninstall
   sudo solo-provisioner daemon service install \
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT
   ```

7. Confirm recovery:
   ```bash
   systemctl is-active solo-provisioner-daemon.service
   # expected: active

   sudo curl --unix-socket $SOCK http://localhost/health
   # expected: {"status":"ok"}

   sudo curl --unix-socket $SOCK http://localhost/status | python3 -m json.tool
   # expected: consensus-node / upgrade-monitor: "running"
   ```

### Expected results

- [ ] After bad install: service is **not** active; `journalctl` shows a probe failure log line
  with `reason=ComponentProbeAborted` and `component=consensus-node`
- [ ] The log message is specific enough to identify the root cause without external knowledge
  (wrong owner, missing dir, or no write access)
- [ ] After fix + reinstall: service reaches `active`, `/health` returns 200,
  `/status` shows `upgrade-monitor: running`
- [ ] No `MonitorDegraded` entries in the log after successful recovery

### Teardown

Ensure the upgrade directory is restored to correct state (owned `hedera:hedera`, mode `0775`)
before running further test cases.

---

## TC-20 — Install with local binary (`--daemon-bin`)

**Goal**: verify that `--daemon-bin` installs a locally-built binary instead of downloading from
the embedded catalog, including platform validation via `--version` execution.

### Prerequisites

- A locally-built `solo-provisioner-daemon` binary for the target OS/arch (e.g. cross-compiled on macOS,
  then copied to the Linux node via `scp`).
- The daemon is not currently installed.

### Steps

1. Uninstall if already installed (see TC-07).
2. Confirm no binary exists at `/opt/solo/weaver/bin/solo-provisioner-daemon`.
3. Run the install pointing at the local binary:
   ```bash
   sudo solo-provisioner daemon service install \
     --daemon-bin /path/to/solo-provisioner-daemon-linux-arm64 \
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT
   ```
4. Confirm the installed binary version:
   ```bash
   /opt/solo/weaver/bin/solo-provisioner-daemon --version
   ```

### Expected results

- [ ] Install workflow runs the supplied binary's `--version` for platform validation; no download step occurs
- [ ] Binary is placed at `/opt/solo/weaver/bin/solo-provisioner-daemon`
- [ ] Service starts successfully
- [ ] `daemon service check` exits 0 and prints `/status` JSON

### Negative check — wrong-arch binary

1. Supply a binary built for the wrong architecture (e.g. an amd64 binary on an arm64 host).
2. Expected: install fails with a clear error from the `--version` execution step (cannot execute binary);
   exit code non-zero; no binary is placed at `/opt/solo/weaver/bin/`.

---

## TC-21 — Auto-download failure: version not yet released (resolution hints)

**Goal**: verify that when the embedded catalog version does not yet exist on GitHub releases,
the error output includes actionable resolution hints with a direct link to the releases page
and manual install instructions.

> This test exercises the error path. No binary download will succeed because the catalog
> default version (`0.0.0`) is a placeholder that does not have a real GitHub release.

### Prerequisites

- No `--daemon-bin` flag is passed (so the auto-download path is taken).
- No binary exists at `/opt/solo/weaver/bin/solo-provisioner-daemon` (uninstall first if needed).
- Network connectivity to `github.com` (to receive the 404 from the download URL).

### Steps

1. Uninstall if already installed (see TC-07).
2. Attempt install without `--daemon-bin`:
   ```bash
   sudo solo-provisioner daemon service install \
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT
   ```

### Expected results

- [ ] Install fails with an error message that includes:
  - The specific version that was attempted (e.g. `0.0.0`)
  - A direct URL to the GitHub releases page for that version
  - A connectivity check suggestion (`curl -I https://github.com`)
  - Instructions to download manually and use `--daemon-bin`
- [ ] Error output resembles:
  ```
  Resolution:
    1. Verify the release exists: https://github.com/hashgraph/solo-weaver/releases/tag/daemon-v0.0.0
    2. Check network connectivity: curl -I https://github.com
    3. Download manually from: https://github.com/hashgraph/solo-weaver/releases/tag/daemon-v0.0.0
    4. Then install with: sudo solo-provisioner daemon service install --daemon-bin=<path-to-binary>
  ```
- [ ] No binary is placed at `/opt/solo/weaver/bin/solo-provisioner-daemon`
- [ ] Exit code is non-zero

---

## TC-22 — Uninstall then re-install triggers fresh download

**Goal**: verify that after uninstall, re-running install without `--daemon-bin` attempts a fresh
download rather than using a stale binary left over from a prior `--daemon-bin` install.

> This catches a regression where the binary's presence in `/opt/solo/weaver/bin/` after a
> `--daemon-bin` install caused subsequent auto-download installs to be silently skipped.

### Steps

1. Install with a local binary:
   ```bash
   sudo solo-provisioner daemon service install \
     --daemon-bin /path/to/solo-provisioner-daemon \
     --components consensus-node --cn-node-id $NODE_ID --cn-orbit $ORBIT
   ```
2. Confirm binary is present: `ls /opt/solo/weaver/bin/solo-provisioner-daemon`
3. Uninstall:
   ```bash
   sudo solo-provisioner daemon service uninstall
   ```
4. Confirm binary was removed: `ls /opt/solo/weaver/bin/solo-provisioner-daemon`
   (expected: `No such file or directory`)
5. Re-install without `--daemon-bin`:
   ```bash
   sudo solo-provisioner daemon service install \
     --components consensus-node --cn-node-id $NODE_ID --cn-orbit $ORBIT
   ```

### Expected results

- [ ] Step 4: binary is absent at `/opt/solo/weaver/bin/solo-provisioner-daemon`
- [ ] Step 5: install **attempts** to download from the catalog (does not skip the download);
  if the catalog version exists, it downloads and installs; if not (e.g. version `0.0.0`),
  it fails with the resolution hints from TC-21
- [ ] A "skipping download" log line does **not** appear in the output

---


---

## TC-23 — Upgrade monitor: `ReadyForProvisionerDaemon` CR triggers execute workflow

**Goal**: verify that when a `NetworkUpgradeExecute` CR transitions to
`status.phase = ReadyForProvisionerDaemon` the upgrade monitor logs the trigger reason
and starts the execute workflow (currently a stub).

### Prerequisites

- Daemon installed and running with `consensus-node` component enabled.
- The `NetworkUpgradeExecute` CRD is installed in the cluster:
  ```bash
  kubectl get crd networkupgradeexecutes.hedera.com
  ```

### Steps

1. Tail the daemon journal in a second terminal:
   ```bash
   journalctl -u solo-provisioner-daemon.service -f
   ```
2. Create a `NetworkUpgradeExecute` CR with the correct phase:
   ```bash
   cat <<'EOF' | kubectl apply -f -
   apiVersion: hedera.com/v1alpha1
   kind: NetworkUpgradeExecute
   metadata:
     name: test-upgrade-01
     namespace: $ORBIT
   spec:
     operationId: test-op-001
     orbit: $ORBIT
   status:
     phase: ReadyForProvisionerDaemon
   EOF
   ```
   > Note: some K8s deployments do not allow setting `status` via `apply`. If so,
   > create the CR without the status block, then patch it:
   > ```bash
   > kubectl patch networkupgradeexecute test-upgrade-01 -n $ORBIT \
   >   --type=merge --subresource=status \
   >   -p '{"status":{"phase":"ReadyForProvisionerDaemon"}}'
   > ```
3. Within a few seconds, check the journal output.

### Expected results

- [ ] Journal contains a line with `reason=ReadyForProvisionerDaemon` and
  `operation_id=test-op-001`:
  ```
  reason=ReadyForProvisionerDaemon operation_id=test-op-001 orbit=$ORBIT
  ```
- [ ] Journal contains a line with `reason=ExecuteWorkflowStarted`:
  ```
  reason=ExecuteWorkflowStarted operation_id=test-op-001
  ```

### Teardown

```bash
kubectl delete networkupgradeexecute test-upgrade-01 -n $ORBIT
```

---

## TC-24 — Upgrade monitor: duplicate event (same `operationId`) is deduplicated

**Goal**: verify that a second `ReadyForProvisionerDaemon` event carrying the same
`operationId` as an already-active execution is silently dropped, not double-processed.

### Steps

1. Tail the daemon journal.
2. Apply the CR from TC-23 (or re-use it if not deleted).
3. Immediately re-apply or re-patch the same CR to a different phase then back to
   `ReadyForProvisionerDaemon` to force a second MODIFIED event with the same `operationId`:
   ```bash
   kubectl patch networkupgradeexecute test-upgrade-01 -n $ORBIT \
     --type=merge --subresource=status \
     -p '{"status":{"phase":"Pending"}}'
   kubectl patch networkupgradeexecute test-upgrade-01 -n $ORBIT \
     --type=merge --subresource=status \
     -p '{"status":{"phase":"ReadyForProvisionerDaemon"}}'
   ```

### Expected results

- [ ] Journal contains only **one** `reason=ExecuteWorkflowStarted` line for `operationId=test-op-001`
- [ ] Journal contains `reason=UpgradeMonitorDuplicateEvent` for the second event:
  ```
  reason=UpgradeMonitorDuplicateEvent operation_id=test-op-001
  ```

> **Previously known bug — FIXED in `c4dd8a32`**: As of build `0.0.0/3695924b`, `UpgradeMonitorDuplicateEvent` was never
> emitted and `ExecuteWorkflowStarted` fired multiple times for the same `operationId`. This is fixed
> in commit `c4dd8a32` (branch `00499-feat-solo-provisioner-daemon-core`).

### Teardown

```bash
kubectl delete networkupgradeexecute test-upgrade-01 -n $ORBIT
```

---

## TC-25 — Upgrade monitor: RBAC revocation triggers auth-error backoff and client rebuild

**Goal**: verify that revoking the daemon's ClusterRoleBinding produces an
`UpgradeMonitorAuthError` log, and that restoring it allows the monitor to recover
without a daemon restart.

### Steps

1. Tail the daemon journal.
2. Revoke the ClusterRoleBinding:
   ```bash
   kubectl delete clusterrolebinding solo-provisioner-daemon-cn
   ```
3. Wait up to 30 seconds for the watch to fail and the monitor to retry.
4. Check the journal.
5. Restore RBAC:

   > **Note**: `daemon service install` is intentionally blocked while the daemon is running
   > (see TC-18). To restore RBAC without a service interruption, use `kubectl` directly:
   > ```bash
   > sudo kubectl create clusterrolebinding solo-provisioner-daemon-cn \
   >   --clusterrole=solo-provisioner-daemon-cn \
   >   --serviceaccount=$ORBIT:solo-provisioner-daemon-cn
   > ```
   >
   > To restore RBAC via the CLI (requires brief service interruption):
   > ```bash
   > sudo solo-provisioner daemon service stop
   > sudo solo-provisioner daemon service install \
   >   --components consensus-node \
   >   --cn-node-id $NODE_ID \
   >   --cn-orbit $ORBIT \
   >   --non-interactive
   > ```
6. Wait ~10 seconds, then check the journal again.

### Expected results

- [ ] After revocation: journal shows `reason=UpgradeMonitorAuthError` or `reason=UpgradeMonitorWatchError`
- [ ] Journal shows `reason=UpgradeMonitorClientRebuilt` or a retry/backoff line after the auth error
- [ ] After restoration: journal shows `reason=UpgradeMonitorWatchEstablished` — watch is re-established
- [ ] Daemon process remains alive throughout (`systemctl is-active solo-provisioner-daemon.service` → `active`)
- [ ] `/health` returns 200 throughout:
  ```bash
  sudo curl --unix-socket $SOCK http://localhost/health
  # expected: {"status":"ok"}
  ```

---

## TC-26 — Upgrade monitor: event log pruning on startup

**Goal**: verify that the upgrade monitor prunes stale `consensus-upgrade-*.jsonl` files
from the events directory when the daemon (re)starts.

### Setup

```bash
# Create synthetic old log files (dated >365 days ago).
EVENTSDIR=/opt/solo/weaver/daemon/events/consensus/upgrade
sudo mkdir -p $EVENTSDIR
sudo touch -t $(date -d '400 days ago' +%Y%m%d%H%M) \
  $EVENTSDIR/consensus-upgrade-20240101T000000Z.jsonl
sudo touch -t $(date -d '400 days ago' +%Y%m%d%H%M) \
  $EVENTSDIR/consensus-upgrade-20240201T000000Z.jsonl
ls $EVENTSDIR/consensus-upgrade-*.jsonl
```

### Steps

1. Restart the daemon service:
   ```bash
   sudo systemctl restart solo-provisioner-daemon.service
   ```
2. Check the events directory:
   ```bash
   ls $EVENTSDIR/consensus-upgrade-*.jsonl
   # expected: EVENTSDIR=/opt/solo/weaver/daemon/events/consensus/upgrade
   ```

### Expected results

- [ ] The two synthetic files from >365 days ago are **removed** after restart
- [ ] Any recent `consensus-upgrade-*.jsonl` files (within 365 days) are **preserved**
- [ ] Journal does **not** contain `reason=UpgradeEventLogPruneFailed`

---

## TC-27 — Migration monitor: soak start accepted

**Goal**: verify that `POST /consensus_node/migration/soak/start` activates the soak,
returns HTTP 202, and immediately reflects `active: true` in the status endpoint.

### Prerequisites

- Daemon installed and running with `consensus-node` component enabled and `migration: true` in `daemon.yaml`.
- No soak is currently active (`GET /consensus_node/migration/soak/status` returns `{"active":false}`).

### Steps

1. Confirm idle state:
   ```bash
   sudo curl -s --unix-socket $SOCK http://localhost/consensus_node/migration/soak/status
   # expected: {"active":false}
   ```
2. Start a soak:
   ```bash
   sudo curl -s -w "\nHTTP %{http_code}\n" \
     -X POST --unix-socket $SOCK \
     -H 'Content-Type: application/json' \
     -d '{"node_id":"$NODE_ID","cutover_timestamp":"2024-01-15T00:00:00Z","migration_plan_path":"/tmp/plan.yaml"}' \
     http://localhost/consensus_node/migration/soak/start
   # expected: {"accepted":true} followed by HTTP 202
   ```
3. Immediately check status:
   ```bash
   sudo curl -s --unix-socket $SOCK http://localhost/consensus_node/migration/soak/status | python3 -m json.tool
   ```
4. Check the migration events JSONL (not the journal — soak events go to the events file):
   ```bash
   sudo tail -5 /opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl
   ```

### Expected results

- [ ] `POST` returns HTTP 202, body `{"accepted":true}`
- [ ] Status immediately shows `"active": true` with the submitted request details:
  ```json
  {
    "active": true,
    "request": {
      "node_id": "$NODE_ID",
      "cutover_timestamp": "2024-01-15T00:00:00Z",
      "migration_plan_path": "/tmp/plan.yaml"
    }
  }
  ```
- [ ] Migration events JSONL contains `reason=SoakStarted` with the `node_id` and `cutover_timestamp`:
  ```json
  {"reason":"SoakStarted","msg":"Soak started for node 0; cutover at 2024-01-15T00:00:00Z", ...}
  ```
  > **Note**: the systemd journal logs `reason=SoakStartAccepted`; the canonical `SoakStarted` event
  > lives in the JSONL file at `/opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl`.

---

## TC-28 — Migration monitor: duplicate soak start returns 409

**Goal**: verify that `POST /consensus_node/migration/soak/start` returns HTTP 409 when
a soak is already active, preventing concurrent soak activations.

### Steps

1. Ensure a soak is already active (run TC-27 first, or start one now).
2. Send a second start request:
   ```bash
   sudo curl -s -w "\nHTTP %{http_code}\n" \
     -X POST --unix-socket $SOCK \
     -H 'Content-Type: application/json' \
     -d '{"node_id":"$NODE_ID","cutover_timestamp":"2024-02-01T00:00:00Z","migration_plan_path":"/tmp/plan2.yaml"}' \
     http://localhost/consensus_node/migration/soak/start
   # expected: HTTP 409
   ```
3. Check that the status still reflects the **first** soak's request (second is ignored):
   ```bash
   sudo curl -s --unix-socket $SOCK http://localhost/consensus_node/migration/soak/status
   ```

### Expected results

- [ ] Second `POST` returns HTTP 409
- [ ] Status still shows the original soak's `cutover_timestamp` (`2024-01-15T00:00:00Z`), not the second request's

---

## TC-29 — Migration monitor: soak resumes after daemon restart

**Goal**: verify that if the daemon is restarted while a soak is active, the soak watcher
resumes automatically from the persisted `cutover-state.jsonl` without losing elapsed time.

### Steps

1. Start a soak (TC-27 steps) and confirm `active: true`.
2. Note the `cutover_timestamp` from the status response.
3. Restart the daemon:
   ```bash
   sudo systemctl restart solo-provisioner-daemon.service
   sleep 5
   ```
4. Check status:
   ```bash
   sudo curl -s --unix-socket $SOCK http://localhost/consensus_node/migration/soak/status | python3 -m json.tool
   ```
5. Check the migration events JSONL for the resume event:
   ```bash
   sudo tail -5 /opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl
   ```

### Expected results

- [ ] After restart, status shows `"active": true` with the **original** `cutover_timestamp`
- [ ] Migration events JSONL contains `reason=SoakResumed` with `elapsed_hours` reflecting real elapsed time:
  ```json
  {"reason":"SoakResumed","msg":"Soak resumed after daemon restart for node 0; cutover at 2024-01-15T00:00:00Z; ...h elapsed", ...}
  ```
  > **Note**: the systemd journal logs `reason=SoakResuming`; the canonical `SoakResumed` event is
  > in the JSONL events file, not the journal.
- [ ] Journal does **not** contain `reason=SoakStateCorrupted`
- [ ] The state file is present on disk:
  ```bash
  sudo ls -la /opt/solo/weaver/daemon/events/consensus/migrate/cutover-state.jsonl
  ```

---

## TC-30 — Migration monitor: `SoakCheck` heartbeat emitted each poll tick

**Goal**: verify that the migration monitor emits a `SoakCheck` JSONL event on every
poll interval — the absence of this event is the failure signal for external monitoring.

> The production poll interval is 15 minutes. For manual testing, the daemon must be
> built with a shorter poll interval override (e.g. 60 seconds) or the tester must
> wait the full interval.

### Steps

1. Start a soak (TC-27 steps).
2. Note the current time.
3. Wait one poll interval (default 15 min; use a test build with a short interval if available).
4. Check the migration events log (not journald — this goes to the JSONL event file):
   ```bash
   # The migrate events JSONL lives at:
   MIGRATE_EVENTS=/opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl
   sudo ls /opt/solo/weaver/daemon/events/consensus/migrate/
   sudo cat $MIGRATE_EVENTS | python3 -m json.tool
   ```

### Expected results

- [ ] At least one line with `reason=SoakCheck` appears in the events JSONL after the poll interval
- [ ] The `SoakCheck` entry's `msg` field includes:
  - `elapsed` (hours since cutover)
  - `uploader_backlog_cleared`
  - `pod_restarts_since_cutover`
  - `fleet_nodes_migrated`
  - `next_check_in_seconds`
- [ ] `SoakCheck` events appear at approximately the configured poll interval

---

## TC-31 — Migration monitor: `FleetThresholdReached` event when flag file is created

**Goal**: verify that creating the fleet threshold flag file at
`/opt/solo/weaver/migration/fleet-threshold-reached` causes the monitor to emit
a `FleetThresholdReached` event on the next poll tick.

### Prerequisites

- An active soak (TC-27).
- Flag file does **not** already exist:
  ```bash
  ls /opt/solo/weaver/migration/fleet-threshold-reached
  # expected: No such file or directory
  ```

### Steps

1. Create the flag file:
   ```bash
   sudo mkdir -p /opt/solo/weaver/migration
   sudo touch /opt/solo/weaver/migration/fleet-threshold-reached
   ```
2. Wait one poll interval.
3. Check the migration events JSONL:
   ```bash
   sudo cat /opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl | python3 -m json.tool
   ```

### Expected results

- [ ] Events JSONL contains exactly **one** line with `reason=FleetThresholdReached`
  (it is emitted on the first tick that sees the flag, never again for the same soak)
- [ ] Subsequent `SoakCheck` events show `fleet_nodes_migrated=1`
- [ ] A second `FleetThresholdReached` line does **not** appear on subsequent ticks

### Teardown

```bash
sudo rm /opt/solo/weaver/migration/fleet-threshold-reached
```

---

## TC-32 — Migration monitor: corrupted state file handled gracefully on restart

**Goal**: verify that if `cutover-state.jsonl` is malformed on disk when the daemon
restarts, the monitor logs `SoakStateCorrupted`, deletes the bad file, and starts idle
— without crashing the daemon.

### Steps

1. Start a soak (TC-27) and confirm the state file is written:
   ```bash
   sudo ls /opt/solo/weaver/daemon/events/consensus/migrate/cutover-state.jsonl
   # expected: file present
   ```
2. Stop the daemon:
   ```bash
   sudo systemctl stop solo-provisioner-daemon.service
   ```
3. Corrupt the state file:
   ```bash
   echo 'not valid json {{{' | sudo tee /opt/solo/weaver/daemon/events/consensus/migrate/cutover-state.jsonl
   ```
4. Start the daemon:
   ```bash
   sudo systemctl start solo-provisioner-daemon.service
   sleep 5
   ```
5. Check status:
   ```bash
   sudo curl -s --unix-socket $SOCK http://localhost/consensus_node/migration/soak/status
   ```
6. Check journal:
   ```bash
   journalctl -u solo-provisioner-daemon.service -n 30
   ```
7. Check the events JSONL for the `SoakStateCorrupted` event:
   ```bash
   sudo cat /opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl | python3 -m json.tool
   ```

### Expected results

- [ ] Service starts successfully; `systemctl is-active` → `active`
- [ ] Status returns `{"active": false}` — no resume attempted
- [ ] Events JSONL contains a line with `reason=SoakStateCorrupted`
- [ ] The corrupted state file is deleted:
  ```bash
  sudo ls /opt/solo/weaver/daemon/events/consensus/migrate/cutover-state.jsonl
  # expected: No such file or directory
  ```
- [ ] Journal does **not** contain any panic or fatal error

---

## TC-33 — Migration monitor: `CriterionMet` emitted once per criterion (false→true edge)

**Goal**: verify that when a soak criterion transitions from not-green to green, the monitor
emits exactly one `CriterionMet` event for that criterion — not one per tick.

> `SoakDuration` is the only criterion that can be verified without external stubs,
> because it transitions to green after a fixed time period. Use a test daemon build
> with a short soak period (e.g. 30 seconds) and a short poll interval (e.g. 10 seconds)
> to make this test tractable in a manual session.

### Steps (using a short-soak test build)

1. Start a soak with a `cutover_timestamp` 60 seconds in the past:
   ```bash
   CUTOVER=$(date -u -d '60 seconds ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || \
             date -u -v-60S +%Y-%m-%dT%H:%M:%SZ)
   sudo curl -s -X POST --unix-socket $SOCK \
     -H 'Content-Type: application/json' \
     -d "{\"node_id\":\"$NODE_ID\",\"cutover_timestamp\":\"$CUTOVER\",\"migration_plan_path\":\"/tmp/plan.yaml\"}" \
     http://localhost/consensus_node/migration/soak/start
   ```
2. Wait two poll intervals.
3. Check the migration events JSONL:
   ```bash
   sudo grep '"reason":"CriterionMet"' /opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl | wc -l
   ```

### Expected results

- [ ] Exactly **one** `reason=CriterionMet` line appears for `SoakDuration` — not repeated on subsequent ticks
- [ ] The event `msg` contains `Soak criterion met: SoakDuration`

---

## TC-34 — Migration monitor: invalid soak start payload rejected (400)

**Goal**: verify that `POST /consensus_node/migration/soak/start` with a missing required
field returns HTTP 400 and does not activate a soak.

### Steps

1. Confirm no soak is active.
2. Send a request missing `cutover_timestamp`:
   ```bash
   sudo curl -s -w "\nHTTP %{http_code}\n" \
     -X POST --unix-socket $SOCK \
     -H 'Content-Type: application/json' \
     -d '{"node_id":"$NODE_ID","migration_plan_path":"/tmp/plan.yaml"}' \
     http://localhost/consensus_node/migration/soak/start
   # expected: HTTP 400
   ```
3. Check status:
   ```bash
   sudo curl -s --unix-socket $SOCK http://localhost/consensus_node/migration/soak/status
   # expected: {"active":false}
   ```

### Expected results

- [ ] Response is HTTP 400 with a JSON error body mentioning the missing field
- [ ] Soak remains inactive

---

## Reporting Results

For each test case, record:

| Field | Value |
|---|---|
| TC ID | e.g. TC-01 |
| Date | |
| Build / commit | `solo-provisioner --version` |
| Node OS & arch | e.g. Ubuntu 22.04 / x86-64 |
| K8s version | `kubectl version --short` |
| Result | PASS / FAIL / BLOCKED |
| Notes | Any deviation from expected output |
| Log snippet | Paste relevant `journalctl` lines on FAIL |

File a GitHub issue against `hashgraph/solo-weaver` for each FAIL, tagged `bug` and `daemon`.

---

## TC-UNT-01 — LoadDaemonConfig: schema_version newer than binary

**Type:** Unit test (automated)
**Command:**
```bash
go test -tags='!integration' -run TestLoadDaemonConfig_NewerSchemaVersion ./internal/daemon/
```

**Scenario:** `daemon.yaml` contains `schema_version: 99` (future binary). The daemon must reject the file with a human-readable error before any strict-decode runs on unknown keys.

**Expected results:**
- [ ] Error type is `ErrConfigMalformed`
- [ ] Error message contains `"newer binary"` and `"99"`
- [ ] Error message does NOT contain `"invalid keys"` (no raw decode error)

---

## TC-UNT-02 — LoadDaemonConfig: valid v1 config

**Type:** Unit test (automated)
**Command:**
```bash
go test -tags='!integration' -run TestLoadDaemonConfig_ValidV1 ./internal/daemon/
```

**Expected results:**
- [ ] Returns populated `DaemonConfig` with no error
- [ ] `ConsensusNode` fields match YAML values

---

## TC-UNT-03 — LoadDaemonConfig: missing file

**Type:** Unit test (automated)
**Command:**
```bash
go test -tags='!integration' -run TestLoadDaemonConfig_MissingFile ./internal/daemon/
```

**Expected results:**
- [ ] Error type is `ErrConfigNotFound`

---

## TC-UNT-04 — LoadDaemonConfig: no schema_version treated as v1

**Type:** Unit test (automated)
**Command:**
```bash
go test -tags='!integration' -run TestLoadDaemonConfig_NoSchemaVersionTreatedAsV1 ./internal/daemon/
```

**Expected results:**
- [ ] Returns populated `DaemonConfig` with no error
- [ ] `SchemaVersion` is set to `CurrentSchemaVersion`
