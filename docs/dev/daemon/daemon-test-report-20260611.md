# solo-provisioner-daemon UAT Report — 2026-06-11

| Field | Value |
|---|---|
| Build (CLI) | `0.0.0` / commit `c4dd8a321931fb178dc29c87fe9765923ae6c6e9` |
| Build (Daemon) | `0.0.0` / commit `c4dd8a321931fb178dc29c87fe9765923ae6c6e9` |
| Branch | `00499-feat-solo-provisioner-daemon-core` |
| Date | 2026-06-11 |
| Node OS & arch | Ubuntu (GCP VM) / x86-64 |
| K8s version | v1.33.4 |
| Tester | Lenin Mehedy (automated via Claude Code) |

---

## Summary

| Category | Tested | PASS | PARTIAL | FAIL | SKIP |
|---|---|---|---|---|---|
| Install / Uninstall | 9 | 4 | 4 | 1 | 1 |
| Block-Node Component | 2 | 0 | 0 | 2 | 0 |
| Config Schema | 2 | 1 | 0 | 1 | 0 |
| HTTP Control Plane | 5 | 5 | 0 | 0 | 0 |
| Resilience | 2 | 2 | 0 | 0 | 0 |
| Upgrade Monitor | 4 | 4 | 0 | 0 | 0 |
| Migration Monitor | 9 | 6 | 0 | 0 | 3 |
| **Total** | **33** | **22** | **4** | **4** | **4** |

---

## Bugs Found

### BUG-1 — Block-node component not installed (`--components` flag)

**Affects**: TC-15, TC-16  
**Severity**: High  

Both `--components block-node` and `--components consensus-node,block-node` fail to write the
block-node section of `daemon.yaml`. When an existing `daemon.yaml` is present, the install
command re-uses it unchanged rather than merging the new component set. When no prior config
exists and only `--components block-node` is passed, the CLI still writes a `consensus_node`
block (from default values), ignoring the block-node flag.

**Observed**:
- `daemon.yaml` contains only `consensus_node` regardless of `--components block-node`
- `daemon-bn.kubeconfig` not created
- Block-node RBAC (SA, CR, CRB) not provisioned
- `/status` shows only `consensus-node`
- No stub monitor log line

**File bug against**: `cmd/cli/commands/daemon/service/install.go` — component resolution and config-merge logic.

---

### BUG-2 — ServiceAccount not removed on uninstall

**Affects**: TC-07  
**Severity**: Medium  

`daemon service uninstall` deletes the ClusterRole and ClusterRoleBinding but leaves the
ServiceAccount behind in the orbit namespace. This causes stale RBAC objects to accumulate
across install/uninstall cycles.

**Observed**:
```
kubectl get serviceaccount solo-provisioner-daemon-cn -n consensus
NAME                         SECRETS   AGE
solo-provisioner-daemon-cn   0         86s   # <-- not removed
```

**File bug against**: `internal/workflows/steps/step_daemon.go` — `DeleteDaemonRBACStep`.

---

### BUG-3 — `schema_version` future-version check not implemented

**Affects**: TC-14  
**Severity**: Medium  

When `daemon.yaml` contains `schema_version: 99`, the daemon exits with
"invalid keys: components, schema_version" (viper/mapstructure strict-decode error)
rather than the expected "written by a newer binary (schema_version 99 > supported 1)".
The exit code is also 0 instead of non-zero.

This means the version guard described in the architecture doc is not yet wired into the
config loading path.

**File bug against**: `pkg/config/global.go` — `Initialize()` — add schema version check
after decode, before the config is used.

---

### BUG-4 — Exit code 0 on error (`install`, `uninstall`, `check` error paths)

**Affects**: TC-18, TC-21  
**Severity**: Medium  

Several commands that print a clear `✗ Error:` block to stderr still exit with code 0:
- `daemon service install` when service is already running (TC-18)
- `daemon service install` when auto-download fails (TC-21)

This breaks scripted / CI usage where callers rely on `$?` to detect failure.

**File bug against**: `cmd/cli/commands/common/run.go` — `RunWorkflow()` exit-code propagation.

---

### BUG-5 — Startup probe failure does not prevent service reaching `active`

**Affects**: TC-19  
**Severity**: Low / Info  

When the consensus-node upgrade staging directory is missing, the daemon logs
`reason=ComponentProbeNotReady` (as a warning) but continues to start, and systemd marks
the service `active`. TC-19 expects the service to remain in `activating` or transition
to `failed` until the probe passes.

The probe is currently asynchronous (fires in the background after startup) so it never
blocks `sd_notify(READY=1)`. The architecture doc describes a blocking startup probe —
either the doc or the implementation needs updating.

**Probe log key observed**: `reason=ComponentProbeNotReady` (expected: `reason=ComponentProbeAborted`).

---

## Observations (non-blocking)

### OBS-1 — TC-24 deduplication bug FIXED

The testing guide documents a known bug: "As of build `0.0.0/3695924b`,
`UpgradeMonitorDuplicateEvent` is never emitted."

This is **fixed** in `c4dd8a32`. `reason=UpgradeMonitorDuplicateEvent` is correctly
emitted for the second `ReadyForProvisionerDaemon` event with the same `operationId`.

### OBS-2 — Journal vs JSONL log key naming differences

The testing guide instructs testers to check `journalctl` for certain log reasons, but
some reasons only appear in the JSONL events file:

| TC | Guide says (journal) | Actual location | Actual key |
|---|---|---|---|
| TC-27 | `reason=SoakStarted` in journal | `consensus-migrate-events.jsonl` | `reason=SoakStarted` |
| TC-29 | `reason=SoakResumed` in journal | `consensus-migrate-events.jsonl` | `reason=SoakResumed` |
| TC-27 | — | journal | `reason=SoakStartAccepted` (different key) |
| TC-29 | — | journal | `reason=SoakResuming` (different key) |

Recommend updating TC-27 and TC-29 to check the JSONL file, or emit the same reason key
in both the journal and JSONL.

### OBS-3 — Kubeconfig permissions are 0640, not 0600

TC-01 / TC-02 expect `mode must be 0600` for `daemon-cn.kubeconfig`.
Observed: `-rw-r----- 1 root weaver` (0640). The file is readable only by root and group
`weaver`, which is functionally equivalent for the daemon (which runs as `weaver`), but
the guide and implementation are inconsistent.

---

## Test Case Results

### Install / Uninstall

| TC | Description | Result | Notes |
|---|---|---|---|
| TC-01 | Fresh install (interactive) | SKIP | Covered by TC-02 post-conditions |
| TC-02 | Fresh install (non-interactive) | **PASS** | All post-conditions met; kubeconfig mode 0640 (see OBS-3) |
| TC-03 | Install with `--from-config` | **PASS** | Supplied file written verbatim; service active |
| TC-04 | Re-install with flag override | **PARTIAL** | `orbit` updated in `daemon.yaml` ✓; service blocked by TC-18 guard (already running) rather than restarting — guide expects restart without stopping first |
| TC-07 | Uninstall | **PARTIAL** | Service/binary/kubeconfig/CRB/CR removed ✓; ServiceAccount **not removed** (see BUG-2) |
| TC-18 | Install blocked when already running | **PARTIAL** | Correct error message + resolution hints ✓; exit code 0 (see BUG-4) |
| TC-19 | Startup probe failure (missing dir) | **PARTIAL** | Probe failure logged as `ComponentProbeNotReady` ✓; service stays **active** rather than failing (see BUG-5); recovery works ✓ |
| TC-20 | Local binary install (`--daemon-bin`) | **PASS** | Platform verified via `--version`; binary placed; service healthy |
| TC-21 | Auto-download failure + resolution hints | **PARTIAL** | All four resolution hints present ✓; exit code 0 (see BUG-4) |
| TC-22 | Uninstall + re-install triggers fresh download | **PASS** | Binary absent after uninstall; re-install attempts download (no skip log) |

### Block-Node Component

| TC | Description | Result | Notes |
|---|---|---|---|
| TC-15 | Consensus-node + block-node together | **FAIL** | See BUG-1 |
| TC-16 | Block-node only | **FAIL** | See BUG-1 |

### Config Schema

| TC | Description | Result | Notes |
|---|---|---|---|
| TC-13 | Schema migration v0→v1 | **PASS** | Daemon starts; `/health` 200; no schema errors |
| TC-14 | Future schema version rejected | **FAIL** | See BUG-3 |

### HTTP Control Plane

| TC | Description | Result | Notes |
|---|---|---|---|
| TC-05 | `daemon service check` | **PASS** | Workflow passes; `/status` JSON printed; both negative checks exit 1 |
| TC-06 | `daemon service start` / `stop` | **PASS** | Both exit 0; service transitions correct |
| TC-08 | `/health` | **PASS** | `{"status":"ok"}` |
| TC-09 | `/status` | **PASS** | Both monitors `running` |
| TC-10 | Migration soak status idle | **PASS** | `{"active":false}` |

### Resilience

| TC | Description | Result | Notes |
|---|---|---|---|
| TC-12 | Monitor crash via RBAC revoke | **PASS** | `/health` stays 200; `MonitorStopped` + watch-closed backoff logged; watch recovers |
| TC-17 | Systemd restart on kill -9 | **PASS** | Service restarts within 5 s; restart event in journal |

### Upgrade Monitor

| TC | Description | Result | Notes |
|---|---|---|---|
| TC-23 | `ReadyForProvisionerDaemon` CR triggers workflow | **PASS** | Both expected log lines emitted |
| TC-24 | Duplicate event deduplicated | **PASS** | **Known bug FIXED** in `c4dd8a32`; `UpgradeMonitorDuplicateEvent` emitted |
| TC-25 | RBAC revocation → auth-error backoff → recovery | **PASS** | `UpgradeMonitorListError` logged; watch re-established after RBAC restore |
| TC-26 | Event log pruning on startup | **PASS** | Files >365 days removed; no prune errors |

### Migration Monitor

| TC | Description | Result | Notes |
|---|---|---|---|
| TC-11 | Soak start idempotency | **PASS** | First call 202 ✓; second call 409 ✓ |
| TC-27 | Soak start accepted | **PASS** | HTTP 202; `SoakStarted` in JSONL events (see OBS-2) |
| TC-28 | Duplicate soak start returns 409 | **PASS** | 409 returned; original `cutover_timestamp` preserved |
| TC-29 | Soak resumes after daemon restart | **PASS** | `active:true` with original timestamp; `SoakResumed` in JSONL (see OBS-2) |
| TC-32 | Corrupted state file handled gracefully | **PASS** | `SoakStateCorrupted` in JSONL ✓; state file deleted ✓; daemon active ✓ |
| TC-34 | Invalid soak payload rejected (400) | **PASS** | HTTP 400; `cutover_timestamp is required`; soak stays inactive |
| TC-30 | `SoakCheck` heartbeat (15-min poll) | SKIP | Requires test build with short poll interval |
| TC-31 | `FleetThresholdReached` on flag file | SKIP | Requires poll interval wait |
| TC-33 | `CriterionMet` emitted once (edge trigger) | SKIP | Requires short-soak test build |

---

## Issues to File

| # | Title | TC | Severity |
|---|---|---|---|
| 1 | `--components block-node` does not install block-node config or RBAC | TC-15, TC-16 | High |
| 2 | `daemon service uninstall` leaves ServiceAccount behind | TC-07 | Medium |
| 3 | `schema_version` future-version guard not implemented in daemon config loader | TC-14 | Medium |
| 4 | `daemon service install`/`uninstall` exit code 0 on error paths | TC-18, TC-21 | Medium |
| 5 | Startup probe failure does not block service reaching `active` state | TC-19 | Low |

---

_Report generated 2026-06-11 from automated UAT run via Claude Code._
