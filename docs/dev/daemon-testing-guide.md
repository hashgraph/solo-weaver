# solo-provisioner-daemon Testing Guide

> **Audience**: human testers running manual acceptance tests on a Linux node (UTM VM or real machine).
> For developer architecture details see [daemon-architecture.md](daemon-architecture.md).

## Prerequisites

Before running any test case, confirm the following are in place:

- [ ] Linux host (x86-64 or arm64) with systemd
- [ ] `solo-provisioner` binary installed and on `$PATH`
- [ ] `solo-provisioner-daemon` binary available (either auto-downloaded during install or supplied via `--daemon-bin`)
- [ ] A reachable Kubernetes cluster (`kubectl cluster-info` succeeds with the default kubeconfig)
- [ ] `curl` available for HTTP control-plane tests
- [ ] Running as a user with `sudo` access (some steps require root for systemd and `/usr/lib/systemd/system/`)

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
- `$NODE_ID` — the Hedera node identifier (e.g. `0.0.3`)
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
   solo-provisioner daemon service install
   ```
3. When prompted, select `consensus-node` component (press Y). Enter:
   - **Consensus Node ID**: `$NODE_ID`
   - **Orbit Namespace**: `$ORBIT`
   - **Upgrade Dir**: accept the default (`/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current`)
     or enter a custom path — **this directory must already exist and satisfy the ownership
     requirements in the Prerequisites section above, otherwise the daemon will fail to start**
4. Confirm the summary table and allow the workflow to run.

### Expected results

- [ ] Workflow completes without error; final line: `solo-provisioner-daemon service installed, enabled, and started`
- [ ] `daemon.yaml` written:
  ```bash
  cat /opt/solo/weaver/config/daemon.yaml
  # must contain schema_version: 1, node_id, orbit, kubeconfig fields
  ```
- [ ] Scoped kubeconfig written:
  ```bash
  ls -la /opt/solo/weaver/config/daemon-cn.kubeconfig
  # mode must be 0600
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
2. Run:
   ```bash
   solo-provisioner daemon service install \
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT
   ```

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
2. Uninstall if already installed (see TC-07).
3. Run:
   ```bash
   solo-provisioner daemon service install --from-config /tmp/daemon-test.yaml
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
   solo-provisioner daemon service install --cn-orbit new-orbit
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

**Goal**: verify that the check command reports service health correctly.

### Steps

1. Ensure the daemon is installed and running.
2. Run:
   ```bash
   solo-provisioner daemon service check
   ```

### Expected results

- [ ] Output shows: unit file present, symlink valid, service enabled, service active
- [ ] Exit code 0

### Negative check — service stopped

1. Stop the service: `solo-provisioner daemon service stop`
2. Run `solo-provisioner daemon service check`
3. Expected: reports service as **inactive**; exit code non-zero

---

## TC-06 — `daemon service start` and `stop`

**Goal**: verify manual start/stop commands work independently of install/uninstall.

### Steps

1. Ensure daemon is installed.
2. Stop:
   ```bash
   solo-provisioner daemon service stop
   systemctl is-active solo-provisioner-daemon.service   # expected: inactive
   ```
3. Start:
   ```bash
   solo-provisioner daemon service start
   systemctl is-active solo-provisioner-daemon.service   # expected: active
   ```

### Expected results

- [ ] Stop command exits 0 and service becomes inactive
- [ ] Start command exits 0 and service becomes active

---

## TC-07 — Uninstall

**Goal**: verify that uninstall stops the service, removes the unit file, kubeconfig, and K8s RBAC resources.

### Steps

1. Ensure daemon is installed and running.
2. Run:
   ```bash
   solo-provisioner daemon service uninstall
   ```

### Expected results

- [ ] Service stopped and removed:
  ```bash
  systemctl is-active solo-provisioner-daemon.service
  # expected: inactive or not found
  ```
- [ ] Symlink removed: `ls /usr/lib/systemd/system/solo-provisioner-daemon.service` → not found
- [ ] `daemon.yaml` removed or left in place (check project behaviour)
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
   curl --unix-socket $SOCK http://localhost/health
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
   curl --unix-socket $SOCK http://localhost/status | python3 -m json.tool
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
   curl --unix-socket $SOCK http://localhost/consensus_node/migration/soak/status
   ```

### Expected result

- [ ] HTTP 200, body is a JSON object describing idle soak state (no active soak)

---

## TC-11 — Migration soak start (idempotency)

**Goal**: verify that `POST /consensus_node/migration/soak/start` starts a soak and that a duplicate
call returns HTTP 409.

### Steps

1. Ensure the daemon is running with migration monitor enabled.
2. Start a soak:
   ```bash
   curl -s -o /dev/null -w "%{http_code}" \
     -X POST --unix-socket $SOCK http://localhost/consensus_node/migration/soak/start
   # expected: 200 or 202
   ```
3. Immediately send a second request:
   ```bash
   curl -s -o /dev/null -w "%{http_code}" \
     -X POST --unix-socket $SOCK http://localhost/consensus_node/migration/soak/start
   # expected: 409
   ```
4. Check soak status:
   ```bash
   curl --unix-socket $SOCK http://localhost/consensus_node/migration/soak/status
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
2. Confirm `/health` responds: `curl --unix-socket $SOCK http://localhost/health`
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
   curl --unix-socket $SOCK http://localhost/health
   # expected: {"status":"ok"}
   ```
6. Restore the ClusterRoleBinding:
   ```bash
   solo-provisioner daemon service install   # re-provisions RBAC idempotently
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

1. Stop the daemon: `solo-provisioner daemon service stop`
2. Edit `/opt/solo/weaver/config/daemon.yaml` and remove the `schema_version:` line entirely.
3. Start the daemon: `solo-provisioner daemon service start`
4. Check: `curl --unix-socket $SOCK http://localhost/health`

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

**Goal**: verify that enabling the block-node component at install time creates its kubeconfig,
RBAC, and starts stub monitors without crashing the daemon.

### Steps

1. Uninstall if already installed.
2. Run:
   ```bash
   solo-provisioner daemon service install \
     --components consensus-node,block-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT \
     --bn-orbit hedera-block-node
   ```

### Expected results

- [ ] Install completes without error
- [ ] `daemon.yaml` contains both `consensus_node` and `block_node` blocks
- [ ] Block-node kubeconfig written: `ls /opt/solo/weaver/config/daemon-bn.kubeconfig`
- [ ] Block-node RBAC exists:
  ```bash
  kubectl get serviceaccount solo-provisioner-daemon-bn -n hedera-block-node
  kubectl get clusterrole solo-provisioner-daemon-bn
  ```
- [ ] `/status` shows `block-node` component:
  ```bash
  curl --unix-socket $SOCK http://localhost/status | python3 -m json.tool
  # "block-node": { "monitors": { "bn-upgrade-monitor": { "state": "running" } } }
  ```
- [ ] Log contains:
  `block-node upgrade monitor not yet implemented — stub running`

---

## TC-16 — Block-node only (no consensus-node)

**Goal**: verify that a block-node-only config is valid and the daemon starts without a
consensus-node block.

### Steps

1. Uninstall if already installed.
2. Run:
   ```bash
   solo-provisioner daemon service install \
     --components block-node \
     --bn-orbit hedera-block-node
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

## TC-18 — Idempotent install (re-run without changes)

**Goal**: verify that running `daemon service install` on a node where everything is already
correctly installed completes without error and does not modify any files.

### Steps

1. Ensure daemon is installed and running.
2. Record the modification time of `daemon.yaml`:
   ```bash
   stat /opt/solo/weaver/config/daemon.yaml
   ```
3. Re-run install with the same parameters:
   ```bash
   solo-provisioner daemon service install \
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT
   ```
4. Check that K8s RBAC resources were not re-created (check `kubectl describe` for creation timestamp).

### Expected results

- [ ] Install completes without error
- [ ] Service remains active throughout
- [ ] RBAC resources have the **same** creation timestamp as before (idempotent — no duplicate create)

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
   solo-provisioner daemon service install \
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
   solo-provisioner daemon service uninstall
   solo-provisioner daemon service install \
     --components consensus-node \
     --cn-node-id $NODE_ID \
     --cn-orbit $ORBIT
   ```

7. Confirm recovery:
   ```bash
   systemctl is-active solo-provisioner-daemon.service
   # expected: active

   curl --unix-socket $SOCK http://localhost/health
   # expected: {"status":"ok"}

   curl --unix-socket $SOCK http://localhost/status | python3 -m json.tool
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
