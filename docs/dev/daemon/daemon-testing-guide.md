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
sudo solo-provisioner kube cluster uninstall --non-interactive 2>/dev/null || true

# Restore upgrade staging dir if it was renamed by TC-19
sudo mv /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current.bak \
        /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current 2>/dev/null || true

echo "=== Reset complete ==="

systemctl is-active solo-provisioner-daemon.service 2>&1       # expected: inactive
rm ~/solo-provisioner-*   2>/dev/null || true # remove old installer binaries from home dir
ls /opt/solo/weaver/config/daemon.yaml 2>/dev/null || echo "daemon.yaml absent (good)"
```

---

## Quick-start: full session setup

Copy-paste the block below on a **fresh Linux VM** to prepare everything the test suite needs.
Run it once before executing any test case. Skip steps you have already completed.

```bash
# ── 0. Build (on your dev machine, not the VM) ─────────────────────────────
task build:cli   GOOS=linux GOARCH=amd64
task build:daemon GOOS=linux GOARCH=amd64

# Copy both binaries to the VM.  Wait for scp to finish before continuing.
export VM_NAME=your-vm-name-or-ip
gcloud compute scp bin/solo-provisioner-linux-amd64 \
                    bin/solo-provisioner-daemon-linux-amd64 "${VM_NAME}":~/

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

## TC-01 — Fresh install (interactive prompts)

**Goal**: verify that `daemon service install` collects all required fields interactively, writes
`daemon.yaml`, provisions RBAC, writes a scoped kubeconfig, installs the binary, and starts the service.

**This only works if there is a valid daemon binary release on Github. Otherwise, use TC-02 to test with a local binary
build.

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

### Negative check — auto-download failure (version not yet released)

When the embedded catalog version does not exist on GitHub releases, the error output must
include actionable resolution hints. Use this check to validate the error path without needing
a real GitHub release.

> Requires network connectivity to `github.com` to receive the 404 from the download URL.
> The default catalog version (`0.0.0`) is a placeholder with no real release.

1. Uninstall if already installed (see TC-07), ensure no binary at `/opt/solo/weaver/bin/solo-provisioner-daemon`.
2. Attempt install without `--daemon-bin`:
   ```bash
   sudo solo-provisioner daemon service install \
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT
   ```
3. Expected:
   - [ ] Install fails (exit non-zero); no binary placed at `/opt/solo/weaver/bin/`
   - [ ] Error output includes the attempted version, a direct URL to the releases page,
     a connectivity check suggestion, and instructions to use `--daemon-bin`:
     ```
     Resolution:
       1. Verify the release exists: https://github.com/hashgraph/solo-weaver/releases/tag/daemon-v0.0.0
       2. Check network connectivity: curl -I https://github.com
       3. Download manually from: https://github.com/hashgraph/solo-weaver/releases/tag/daemon-v0.0.0
       4. Then install with: sudo solo-provisioner daemon service install --daemon-bin=<path-to-binary>
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

### Negative check — wrong-arch binary

1. Supply a binary built for the wrong architecture (e.g. an `amd64` binary on an `arm64` host).
2. Run:
   ```bash
   sudo solo-provisioner daemon service install \
     --daemon-bin /path/to/wrong-arch-binary \
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT \
     --non-interactive
   ```
3. Expected:
   - [ ] Install fails with a clear error from the `--version` execution step (cannot execute binary)
   - [ ] Exit code non-zero; no binary placed at `/opt/solo/weaver/bin/`

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
   sudo solo-provisioner daemon service install --from-config /tmp/daemon-test.yaml --daemon-bin $DAEMON_BIN
   ```

### Expected results

- [ ] `daemon.yaml` at `/opt/solo/weaver/config/daemon.yaml` matches the supplied file
- [ ] Service is running
- [ ] No prompt appeared

---

## TC-04 — Re-install with flag override

**Goal**: verify that flag overrides are applied to `daemon.yaml` and the service starts cleanly
when `install` is re-run against an existing (stopped) installation.

> **Note**: `daemon service install` is intentionally blocked while the daemon is running —
> see TC-18. The service must be stopped before re-installing so that neither `daemon.yaml`
> nor the binary are touched while the service holds them open.

### Steps

1. Ensure the daemon is installed and running (TC-01 or TC-02).
2. Stop the service:
   ```bash
   sudo solo-provisioner daemon service stop
   ```
3. Re-run install with the new flag value:
   ```bash
   sudo solo-provisioner daemon service install --cn-node-id 99 --daemon-bin $DAEMON_BIN
   ```
4. Verify:
   ```bash
   sudo solo-provisioner daemon service check
   ```
5. Verify: `daemon.yaml` has the new node ID and the old orbit value is unchanged (flags only override
   the fields they set, not the entire config):
   ```bash
   grep node_id /opt/solo/weaver/config/daemon.yaml
   # expected: node_id: "99"

   grep orbit /opt/solo/weaver/config/daemon.yaml
   # expected: orbit: $ORBIT (same as before, unchanged by this install)
   ```

### Expected results

- [ ] `daemon service stop` exits 0 and service is inactive
- [ ] `daemon service install` exits 0 with no prompts
- [ ] `daemon.yaml` now has `node-id: 99` but the same `orbit` value as before (flags only override what they set, not the whole file)
- [ ] Service is active after re-install

### Negative check — attempt install while running

1. Ensure daemon is running: `sudo solo-provisioner daemon service check`
2. Run install without stopping first:
   ```bash
   sudo solo-provisioner daemon service install --cn-node-id 199 
   ```
3. Expected: command exits non-zero with "already running" message; `daemon.yaml` is **unchanged**

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

1. Ensure daemon is installed and running (TC - 02).
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

1. Ensure the daemon is running (TC - 02).
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

1. Ensure the daemon is running with consensus-node enabled (TC - 02).
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
        "upgrade-monitor": {
          "state": "running"
        },
        "migration-monitor": {
          "state": "running"
        }
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
   sudo solo-provisioner daemon service stop # stop first
   # re-provisions RBAC idempotently
   sudo solo-provisioner daemon service install \                                                                                                                                                                                          ─╯ 
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT \
     --daemon-bin $DAEMON_BIN \
     --non-interactive
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

1. Stop the daemon: `sudo solo-provisioner daemon service stop`
2. Edit `/tmp/daemon-test.yaml`, change `schema_version:` to `99`.
3. Attempt to start the daemon with invalid config:
   ```bash
   /opt/solo/weaver/bin/solo-provisioner-daemon --config /tmp/daemon-test.yaml
   ```
4. Attempt to use incorrect config with the install command (should also fail with the same error):
```bash
sudo solo-provisioner daemon service install \                                                                                                                                                                                          ─╯
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT \
     --daemon-bin $DAEMON_BIN \
     --non-interactive \
     --from-config /tmp/daemon-test.yaml
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

1. Uninstall if already installed `sudo solo-provisioner daemon service uninstall`.
2. Run:
   ```bash
   sudo solo-provisioner daemon service install \
     --components consensus-node,block-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT \
     --daemon-bin $DAEMON_BIN 
   ```

### Expected results

- [ ] Install completes without error
- [ ] `daemon.yaml` contains both `consensus_node` and `block_node` blocks
- [ ] `/status` shows `block-node` component:
  ```bash
  sudo curl --unix-socket $SOCK http://localhost/status | python3 -m json.tool
  # "block-node": { "monitors": { "bn-traffic-shaper-monitor": { "state": "running" } } }
  ```
- [] Run `sudo solo-provisioner daemon service check ` and output JSON should show 
  # "block-node": { "monitors": { "bn-traffic-shaper-monitor": { "state": "running" } } }
- [ ] Log contains:
  `block-node traffic-shaper monitor not yet implemented — stub running`

---

## TC-16 — Block-node only (no consensus-node)

**Goal**: verify that a block-node-only config is valid and the daemon starts without a
consensus-node block.

> **Note**: `--bn-orbit` is no longer required while the traffic-shaper monitor is stubbed.

### Steps

1. Uninstall if already installed `sudo solo-provisioner daemon service uninstall`.
2. Run:
   ```bash
   sudo solo-provisioner daemon service install \
     --components block-node \
     --daemon-bin $DAEMON_BIN \
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
daemon service is already running, leaving both the service and `daemon.yaml` completely
unchanged. This is intentional behaviour — the guard runs before any files are touched to
prevent partial-update state (config changed on disk while the service is still running with
old in-memory config). The operator must stop the service first.

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
- [ ] `daemon.yaml` is unchanged (no flag overrides were applied)

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

## TC-19 — Install-time probe failure: missing or misconfigured upgrade directory

**Goal**: verify that when the consensus-node upgrade staging directory is absent or has wrong
ownership, `daemon service install` exits non-zero with a clear probe error, the daemon service
IS running (socket up), and the operator can diagnose, fix, and recover by reinstalling.

> **Design note**: the install workflow ends with a synchronous health-check step that queries
> the daemon's `/status` endpoint. If any component probe is failing at that point, install
> reports "Completed with errors" and exits non-zero — the probe error is shown inline in the
> install output. The daemon IS running because `READY=1` was sent when the HTTP socket came up;
> only the post-install prerequisite check fails.

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
     --cn-orbit $ORBIT \
     --daemon-bin $DAEMON_BIN \
     --non-interactive
   ```
   Expected: install reports **"Completed with errors"**, exits non-zero, and the inline output
   shows the probe error, for example:
   ```
   [NOT READY] consensus-node: disk write-test probe: cannot write to
   /opt/hgcapp/.../upgrade/current: open .../.probe-...: no such file or directory
   ```

2. Confirm the daemon service is running despite the install error:
   ```bash
   systemctl status solo-provisioner-daemon.service
   # expected: active (running)
   ```

3. Confirm the probe failure is visible via the HTTP API:
   ```bash
   SOCK=/opt/solo/weaver/daemon/daemon.sock
   sudo curl --unix-socket $SOCK http://localhost/status | python3 -m json.tool
   ```
   The response should show a non-empty `probe_errors` field for `consensus-node`.

4. Inspect the journal for additional probe detail:
   ```bash
   journalctl -u solo-provisioner-daemon.service -n 60
   ```
   Look for log lines containing `disk write-test probe:`.

5. Fix the issue:
   ```bash
   # If you renamed the directory:
   sudo mv /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current.bak \
           /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current

   # If you changed ownership:
   sudo chown hedera:hedera /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
   sudo chmod 0775 /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
   ```

6. Confirm recovery:
   ```bash
   systemctl is-active solo-provisioner-daemon.service
   # expected: active

   sudo curl --unix-socket $SOCK http://localhost/health
   # expected: {"status":"ok"}

   sudo curl --unix-socket $SOCK http://localhost/status | python3 -m json.tool
   # expected: consensus-node / upgrade-monitor: "running", probe_errors absent or empty
   ```

### Expected results

- [ ] Install exits non-zero with "Completed with errors"; the inline output names the failing
  probe and the specific path that could not be written
- [ ] Despite the non-zero exit, `systemctl status` shows the daemon is **active (running)**
- [ ] `GET /status` shows a non-empty `probe_errors` entry for `consensus-node`
- [ ] After fix + reinstall: install exits zero with "Completed successfully"; `/health` returns
  200; `/status` shows `upgrade-monitor: running` and no `probe_errors`
- [ ] No `MonitorDegraded` entries in the log after successful recovery

### Teardown

Ensure the upgrade directory is restored to correct state (owned `hedera:hedera`, mode `0775`)
before running further test cases.

---

## TC-20 — Uninstall then re-install triggers fresh download

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
2. Create a `NetworkUpgradeExecute` CR and set its status via the status subresource:
   ```bash
   kubectl apply -f - <<'EOF'
   apiVersion: hedera.com/v1alpha1
   kind: NetworkUpgradeExecute
   metadata:
     name: test-upgrade-01
     namespace: ${ORBIT}
   spec:
     operationId: test-op-001
     orbit: ${ORBIT}
   EOF

   kubectl patch networkupgradeexecute test-upgrade-01 -n $ORBIT \
     --subresource=status --type=merge \
     -p '{"status":{"phase":"ReadyForProvisionerDaemon"}}'
   ```
   > **Why two steps**: `kubectl apply` with an inline `status:` block is silently ignored
   > on all standard clusters — `status` is a subresource and requires a separate
   > `--subresource=status` patch to write through to the live object.
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

1. Capture a start timestamp — all journal checks below use this to ignore prior runs:
   ```bash
   TEST_START=$(date --utc +"%Y-%m-%d %H:%M:%S")
   echo "Test window starts at: $TEST_START"
   ```

2. Ensure a fresh CR exists with `status.phase = ReadyForProvisionerDaemon` (creates test-upgrade-01
   if not already present from TC-23):
   ```bash
   kubectl apply -f - <<'EOF'
   apiVersion: hedera.com/v1alpha1
   kind: NetworkUpgradeExecute
   metadata:
     name: test-upgrade-01
     namespace: ${ORBIT}
   spec:
     operationId: test-op-001
     orbit: ${ORBIT}
   EOF

   kubectl patch networkupgradeexecute test-upgrade-01 -n $ORBIT \
     --subresource=status --type=merge \
     -p '{"status":{"phase":"ReadyForProvisionerDaemon"}}'
   ```
   Wait for the first trigger to appear (confirms the daemon has processed this operationId once):
   ```bash
   journalctl -u solo-provisioner-daemon.service --since "$TEST_START" --no-pager \
     | grep "reason=ExecuteWorkflowStarted" | grep "test-op-001"
   ```

3. Force a second `ReadyForProvisionerDaemon` event with the same `operationId` by cycling the phase:
   ```bash
   kubectl patch networkupgradeexecute test-upgrade-01 -n $ORBIT \
     --subresource=status --type=merge \
     -p '{"status":{"phase":"Pending"}}'
   kubectl patch networkupgradeexecute test-upgrade-01 -n $ORBIT \
     --subresource=status --type=merge \
     -p '{"status":{"phase":"ReadyForProvisionerDaemon"}}'
   ```
   Wait ~5 seconds, then verify:
   ```bash
   # Should show exactly one ExecuteWorkflowStarted for test-op-001 since test start
   journalctl -u solo-provisioner-daemon.service --since "$TEST_START" --no-pager \
     | grep "reason=ExecuteWorkflowStarted" | grep "test-op-001"

   # Should show at least one DuplicateEvent for test-op-001 since test start
   journalctl -u solo-provisioner-daemon.service --since "$TEST_START" --no-pager \
     | grep "reason=UpgradeMonitorDuplicateEvent" | grep "test-op-001"
   ```

### Expected results

- [ ] First `journalctl` check (step 2): exactly one `reason=ExecuteWorkflowStarted` line
  with `operation_id=test-op-001`
- [ ] Second `journalctl` check (step 3): still exactly one `reason=ExecuteWorkflowStarted`
  line — the cycle did **not** produce a second trigger
- [ ] `reason=UpgradeMonitorDuplicateEvent` appears for the second event:
  ```
  DBG Ignoring ReadyForProvisionerDaemon event — operationId already completed in this session operation_id=test-op-001 reason=UpgradeMonitorDuplicateEvent
  ```

### Teardown

```bash
kubectl delete networkupgradeexecute test-upgrade-01 -n $ORBIT
```

---

## TC-25 — Upgrade monitor: RBAC revocation triggers list-error backoff and recovery

**Goal**: verify that revoking the daemon's ClusterRoleBinding produces a `UpgradeMonitorListError`
log with backoff, the daemon stays alive, and restoring RBAC allows the monitor to recover
without a daemon restart.

> **How errors surface**: when RBAC is revoked the upgrade monitor's `listAndSeed` call fails
> first (`UpgradeMonitorListError`). `UpgradeMonitorAuthError` fires only if the watch stream
> itself returns a 403 after a successful list — this is less likely to be observed when the
> ClusterRoleBinding is deleted outright. Both paths lead to the same backoff/retry behaviour.
>
> **`/status` during RBAC failure**: `daemon service check` still reports
> `upgrade-monitor: running` because the goroutine is alive in its retry loop. The watch
> connectivity error is only visible in the journal — this is a known limitation.

### Steps

1. Capture a start timestamp:
   ```bash
   TEST_START=$(date --utc +"%Y-%m-%d %H:%M:%S")
   SOCK=/opt/solo/weaver/daemon/daemon.sock
   ```

2. Revoke the ClusterRoleBinding:
   ```bash
   kubectl delete clusterrolebinding solo-provisioner-daemon-cn
   ```

3. Wait ~10 seconds, then confirm the list error is logged:
   ```bash
   journalctl -u solo-provisioner-daemon.service --since "$TEST_START" --no-pager \
     | grep "reason=UpgradeMonitorListError"
   ```
   Expected output (truncated):
   ```
   WRN List error — retrying error="...networkupgradeexecutes.hedera.com is forbidden..." reason=UpgradeMonitorListError
   ```

4. Confirm the daemon is still alive and `/health` still responds:
   ```bash
   systemctl is-active solo-provisioner-daemon.service
   # expected: active

   sudo curl --unix-socket $SOCK http://localhost/health
   # expected: {"status":"ok"}
   ```
   > `daemon service check` will show `upgrade-monitor: running` — this is correct; the goroutine
   > is alive. The RBAC error only appears in the journal, not in `/status`.

5. Restore RBAC without restarting the daemon:
   ```bash
   kubectl create clusterrolebinding solo-provisioner-daemon-cn \
     --clusterrole=solo-provisioner-daemon-cn \
     --serviceaccount=$ORBIT:solo-provisioner-daemon-cn
   ```
   > To restore via the CLI instead (requires brief service interruption):
   > ```bash
   > sudo solo-provisioner daemon service stop
   > sudo solo-provisioner daemon service install \
   >   --components consensus-node \
   >   --cn-node-id $NODE_ID \
   >   --cn-orbit $ORBIT \
   >   --non-interactive
   > ```

6. After restoring RBAC, wait for the watch to recover. Recovery is delayed by the current
   backoff period (doubles on each failure: 2 s → 4 s → 8 s … up to **5 minutes** maximum).
   If the daemon had already reached a long backoff before RBAC was restored, you may need to
   wait up to 5 minutes. To skip the wait, restart the daemon:
   ```bash
   sudo systemctl restart solo-provisioner-daemon.service
   TEST_START=$(date --utc +"%Y-%m-%d %H:%M:%S")   # reset window after restart
   ```
   Then confirm recovery:
   ```bash
   journalctl -u solo-provisioner-daemon.service --since "$TEST_START" --no-pager \
     | grep "reason=UpgradeMonitorWatchEstablished"
   ```
   Expected output:
   ```
   DBG Watch established on NetworkUpgradeExecute CRs namespace=<orbit> reason=UpgradeMonitorWatchEstablished
   ```
   `UpgradeMonitorWatchEstablished` is the definitive recovery signal — it fires only after
   `listAndSeed` succeeds **and** the watch stream is open. If it does not appear, check for
   continued errors:
   ```bash
   journalctl -u solo-provisioner-daemon.service --since "$TEST_START" --no-pager \
     | grep -E "reason=UpgradeMonitorListError|reason=UpgradeMonitorWatchEstablished" \
     | tail -10
   ```

### Expected results

- [ ] Step 3: `reason=UpgradeMonitorListError` appears with the `forbidden` error message within ~10s of revocation
- [ ] Step 4: daemon remains `active`; `/health` returns `{"status":"ok"}`; `/status` shows `upgrade-monitor: running`
- [ ] Step 6: `reason=UpgradeMonitorWatchEstablished` appears after RBAC is restored (no further `ListError` lines follow)
- [ ] Daemon process never restarted (same PID throughout — check `systemctl status solo-provisioner-daemon.service`)

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

## Short-poll daemon for UAT

TC-27, TC-28, and TC-30 require the soak watcher to fire within seconds rather than the
production default of 15 minutes. The daemon reads `SOLO_SOAK_POLL_INTERVAL` at startup;
set it to a short duration before starting (or restarting) the service.

**Recommended values per test case:**

| Test                              | Recommended interval |
|-----------------------------------|----------------------|
| TC-27 (SoakCheck heartbeat)       | `30s`                |
| TC-28 (FleetThresholdReached)     | `30s`                |
| TC-30 (CriterionMet edge trigger) | `10s`                |

**How to set the env var for a systemd-managed daemon:**

```bash
# Create an override drop-in (do NOT edit the main unit file):
sudo systemctl edit solo-provisioner-daemon.service
```

Add the following in the editor that opens and save:

```ini
[Service]
Environment="SOLO_SOAK_POLL_INTERVAL=30s"
```

Then reload and restart:

```bash
sudo systemctl daemon-reload
sudo systemctl restart solo-provisioner-daemon.service
# Verify it took effect:
sudo systemctl show solo-provisioner-daemon.service | grep SOLO_SOAK
```

**Restore production defaults** when UAT is complete:

```bash
sudo systemctl revert solo-provisioner-daemon.service
sudo systemctl daemon-reload
sudo systemctl restart solo-provisioner-daemon.service
```

> If the value of `SOLO_SOAK_POLL_INTERVAL` cannot be parsed as a Go duration (e.g. a
> typo), the daemon logs a warning (`reason=InvalidSoakPollInterval`) and falls back to
> the 900 s production default.

---

## TC-27 — Migration monitor: `SoakCheck` heartbeat emitted each poll tick

**Goal**: verify that the migration monitor emits a `SoakCheck` JSONL event on every
poll interval — the absence of this event is the failure signal for external monitoring.

> The production poll interval is 15 minutes. Set `SOLO_SOAK_POLL_INTERVAL=30s` when
> starting the daemon to shorten the interval for this test (see
> [Short-poll daemon for UAT](#short-poll-daemon-for-uat) below).

### Prerequisites

- The daemon is running with `SOLO_SOAK_POLL_INTERVAL=30s` (see
  [Short-poll daemon for UAT](#short-poll-daemon-for-uat)).

### Steps

1. Start a soak (TC-32 steps).
2. Note the current time.
3. Wait one poll interval (30 s with the env var set).
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

## TC-28 — Migration monitor: `FleetThresholdReached` event when flag file is created

**Goal**: verify that creating the fleet threshold flag file at
`/opt/solo/weaver/migration/fleet-threshold-reached` causes the monitor to emit
a `FleetThresholdReached` event on the next poll tick.

### Prerequisites

- The daemon is running with `SOLO_SOAK_POLL_INTERVAL=30s` (see
  [Short-poll daemon for UAT](#short-poll-daemon-for-uat)).
- An active soak (TC-32).
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
2. Wait one poll interval (30 s with `SOLO_SOAK_POLL_INTERVAL=30s`).
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

## TC-29 — Migration monitor: corrupted state file handled gracefully on restart

**Goal**: verify that if `cutover-state.jsonl` is malformed on disk when the daemon
restarts, the monitor logs `SoakStateCorrupted`, deletes the bad file, and starts idle
— without crashing the daemon.

### Steps

1. Start a soak (TC-32) and confirm the state file is written:
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

## TC-30 — Migration monitor: `CriterionMet` emitted once per criterion (false→true edge)

**Goal**: verify that when a soak criterion transitions from not-green to green, the monitor
emits exactly one `CriterionMet` event for that criterion — not one per tick.

> `SoakDuration` is the only criterion that can be verified without external stubs,
> because it transitions to green after a fixed time period. Set
> `SOLO_SOAK_POLL_INTERVAL=10s` when starting the daemon and use a `cutover_timestamp`
> in the past so `SoakDuration` fires within the first few ticks.

### Prerequisites

- The daemon is running with `SOLO_SOAK_POLL_INTERVAL=10s` (see
  [Short-poll daemon for UAT](#short-poll-daemon-for-uat)).

### Steps

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

## TC-31 — Migration monitor: invalid soak start payload rejected (400)

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

---

## TC-32 — `consensus migration soak start` CLI command

**Goal**: verify that the CLI `soak start` command starts the soak watcher via the daemon HTTP API,
shows TUI step output, and exits 0.

### Prerequisites

- Daemon installed and running with `migration: true`.
- No soak currently active.

### Steps

1. Confirm no soak is active:
   ```bash
   sudo solo-provisioner consensus migration soak status
   # expected: {"active":false}
   ```
2. Start the soak:
   ```bash
   sudo solo-provisioner consensus migration soak start \
     --node-id $NODE_ID \
     --cutover-ts 2024-01-15T00:00:00Z \
     --migration-plan /tmp/plan.yaml
   ```
3. Confirm status immediately reflects the new soak:
   ```bash
   sudo solo-provisioner consensus migration soak status
   ```
   Expected JSON:
   ```json
   {
     "active": true,
     "request": {
       "node_id": "...",
       "cutover_timestamp": "2024-01-15T00:00:00Z",
       "migration_plan_path": "/tmp/plan.yaml"
     }
   }
   ```
4. Check the migration events file (soak events go here, not only to the journal):
   ```bash
   sudo tail -5 /opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl
   ```
5. Check the journal:
   ```bash
   journalctl -u solo-provisioner-daemon.service -g SoakStarted -n 5
   # expected line contains: reason=SoakStarted node_id=... cutover_ts=... poll_interval=15m0s
   ```
   > The journal also logs `reason=SoakStartAccepted` when the HTTP request is first received
   > (before the watcher goroutine starts). `SoakStarted` appears slightly later, once the
   > watcher is running.

### Expected results

- [ ] Command exits 0
- [ ] TUI shows a step completion line: `Consensus-node migration soak watcher started (node_id=...)`
  (or in `--non-interactive` mode: structured log line with the same message)
- [ ] `soak status` shows `"active": true` with the submitted request details
- [ ] Events file contains `reason=SoakStarted`:
  ```json
  {"reason":"SoakStarted","msg":"Soak started for node 0; cutover at 2024-01-15T00:00:00Z", ...}
  ```
- [ ] Journal contains `reason=SoakStarted` with `poll_interval=15m0s`

### Negative check — missing required flag

```bash
sudo solo-provisioner consensus migration soak start --node-id $NODE_ID --cutover-ts 2024-01-15T00:00:00Z
# missing --migration-plan
```

Expected: exits non-zero with a Cobra "required flag" error; no HTTP call is made.

### Negative check — duplicate start (soak already active)

With the soak from step 2 still running:

```bash
sudo solo-provisioner consensus migration soak start \
  --node-id $NODE_ID \
  --cutover-ts 2024-02-01T00:00:00Z \
  --migration-plan /tmp/plan2.yaml
```

Expected:
- [ ] Command exits non-zero; error indicates a soak is already active
- [ ] `soak status` still shows the **original** `cutover_timestamp` (`2024-01-15T00:00:00Z`), not the second request's

---

## TC-33 — `consensus migration soak stop` CLI command (default: delete state)

**Goal**: verify that `soak stop` stops the watcher, deletes `cutover-state.jsonl`, and exits 0.

### Prerequisites

- Active soak (run TC-32 first).

### Steps

1. Confirm soak is active:
   ```bash
   sudo solo-provisioner consensus migration soak status
   # expected: "active": true
   ```
2. Stop without `--keep-state`:
   ```bash
   sudo solo-provisioner consensus migration soak stop
   ```
3. Confirm soak is stopped:
   ```bash
   sudo solo-provisioner consensus migration soak status
   # expected: "active": false
   ```
4. Confirm state file is deleted:
   ```bash
   sudo ls /opt/solo/weaver/daemon/events/consensus/migrate/cutover-state.jsonl
   # expected: No such file or directory
   ```

### Expected results

- [ ] Command exits 0
- [ ] TUI shows:`Consensus-node migration soak watcher stopped (state deleted — daemon will NOT resume on next restart)`
- [ ] `soak status` returns `"active": false`
- [ ] `cutover-state.jsonl` is absent — daemon will NOT auto-resume on next restart

---

## TC-34 — `consensus migration soak stop --keep-state`

**Goal**: verify that `soak stop --keep-state` stops the watcher but preserves
`cutover-state.jsonl`, so the daemon resumes the soak on the next restart.

### Prerequisites

- Active soak.

### Steps

1. Stop with `--keep-state`:
   ```bash
   sudo solo-provisioner consensus migration soak stop --keep-state
   ```
2. Confirm state file is present:
   ```bash
   sudo ls -la /opt/solo/weaver/daemon/events/consensus/migrate/cutover-state.jsonl
   # expected: file present
   ```
3. Restart the daemon:
   ```bash
   sudo systemctl restart solo-provisioner-daemon.service
   sleep 5
   ```
4. Check status:
   ```bash
   sudo solo-provisioner consensus migration soak status
   ```

### Expected results

- [ ] Stop exits 0; TUI shows: `...state preserved — daemon WILL resume on next restart`
- [ ] `cutover-state.jsonl` is present after stop
- [ ] After restart, `soak status` shows `"active": true` with the original `cutover_timestamp`
- [ ] Journal contains `reason=SoakResumed` with `poll_interval`

---

## TC-35 — `consensus migration soak status` CLI command

**Goal**: verify that `soak status` prints the current soak state as indented JSON and exits 0.

### Steps

1. With no active soak:
   ```bash
   sudo solo-provisioner consensus migration soak status
   ```
   Expected: JSON with `"active": false`; exit 0.

2. Start a soak (TC-32) then re-run:
   ```bash
   sudo solo-provisioner consensus migration soak status
   ```
   Expected: JSON with `"active": true` and the `request` block.

### Negative check — daemon not running

1. Stop the daemon: `sudo systemctl stop solo-provisioner-daemon.service`
2. Run: `sudo solo-provisioner consensus migration soak status`
3. Expected: exits non-zero with resolution hints including:
    - `Verify daemon is running: sudo systemctl status solo-provisioner-daemon`
    - `Check daemon journal: sudo journalctl -u solo-provisioner-daemon -n 20 --no-pager`
    - `If not installed: sudo solo-provisioner daemon service install`

---

## TC-36 — `DELETE /consensus_node/migration/soak` API: 409 when no soak active

**Goal**: verify that the stop endpoint returns HTTP 409 when no soak watcher is running.

### Prerequisites

- Daemon running; no active soak (`soak status` returns `"active": false`).

### Steps

```bash
sudo curl -s -w "\nHTTP %{http_code}\n" \
  -X DELETE --unix-socket $SOCK \
  http://localhost/consensus_node/migration/soak
# expected: HTTP 409 with body {"error":"no soak watcher is currently active"}
```

### Expected result

- [ ] HTTP 409; body `{"error":"no soak watcher is currently active"}`

---

## TC-40 — Reconfigure poll interval without data loss (full flow)

**Goal**: verify the operator flow for changing `SOLO_SOAK_POLL_INTERVAL` while a soak is in
progress, without losing elapsed soak time.

> This test combines TC-34 (stop --keep-state) with poll-interval reconfiguration.

### Steps

1. Start a soak with the default interval (TC-32).
2. Confirm `poll_interval=15m0s` in the journal:
   ```bash
   journalctl -u solo-provisioner-daemon.service -g SoakStarted -n 3
   ```
3. Stop the watcher but preserve state:
   ```bash
   sudo solo-provisioner consensus migration soak stop --keep-state
   ```
4. Set a short poll interval via systemd override:
   ```bash
   sudo systemctl edit solo-provisioner-daemon.service
   # Add: [Service]
   #      Environment="SOLO_SOAK_POLL_INTERVAL=30s"
   sudo systemctl daemon-reload
   ```
5. Restart the daemon:
   ```bash
   sudo systemctl restart solo-provisioner-daemon.service
   sleep 5
   ```
6. Confirm resumed with new interval:
   ```bash
   journalctl -u solo-provisioner-daemon.service -g SoakResumed -n 3
   # expected: poll_interval=30s
   ```
7. Confirm status shows active soak with original `cutover_timestamp`:
   ```bash
   sudo solo-provisioner consensus migration soak status
   ```

### Expected results

- [ ] Step 3: stop exits 0; state file preserved
- [ ] Step 6: journal shows `reason=SoakResumed` with `poll_interval=30s`
- [ ] Step 7: `"active": true`; original `cutover_timestamp` unchanged

### Teardown

```bash
sudo systemctl revert solo-provisioner-daemon.service
sudo systemctl daemon-reload
sudo solo-provisioner consensus migration soak stop
```

---

## Reporting Results

For each test case, record:

| Field          | Value                                     |
|----------------|-------------------------------------------|
| TC ID          | e.g. TC-01                                |
| Date           |                                           |
| Build / commit | `solo-provisioner --version`              |
| Node OS & arch | e.g. Ubuntu 22.04 / x86-64                |
| K8s version    | `kubectl version --short`                 |
| Result         | PASS / FAIL / BLOCKED                     |
| Notes          | Any deviation from expected output        |
| Log snippet    | Paste relevant `journalctl` lines on FAIL |

File a GitHub issue against `hashgraph/solo-weaver` for each FAIL, tagged `bug` and `daemon`.

---

## TC-UNT-01 — LoadDaemonConfig: schema_version newer than binary

**Type:** Unit test (automated)
**Command:**

```bash
go test -tags='!integration' -run TestLoadDaemonConfig_NewerSchemaVersion ./internal/daemon/
```

**Scenario:** `daemon.yaml` contains `schema_version: 99` (future binary). The daemon must reject the file with a
human-readable error before any strict-decode runs on unknown keys.

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
