# #519 — Implement UpgradeMonitor goroutine

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/519
> **Epic:** #499 — solo-provisioner-daemon Core
> **Story branch:** `00519-implement-upgrade-monitor-goroutine`
> **PR base:** `00499-feat-solo-provisioner-daemon-core`
> **PR closes:** #519

## Summary

Implement the `UpgradeMonitor` goroutine inside the solo-provisioner-daemon. It watches
`NetworkUpgradeExecute` CRs (`hedera.com/v1alpha1`) in the orbit namespace and triggers
the execute-phase workflow when a CR transitions to `ReadyForProvisionerDaemon`. The
goroutine is self-healing: all watch errors retry with exponential backoff; auth errors
additionally rebuild the Kubernetes dynamic client from the kubeconfig on disk.

## Problem

`internal/daemon/consensus/upgrade_monitor.go` existed as a stub (`Run` returned nil
immediately). There was no daemon config file, no kubeconfig path, no orbit namespace,
and no watch loop. The daemon could not observe or react to upgrade CRs.

## Key Design Decisions

| Question | Decision |
|----------|----------|
| K8s client type | `k8s.io/client-go/dynamic` — CRD is unstructured, no generated types needed |
| Scope of watch | Single namespace (`cfg.Namespace`) — one daemon manages one CN |
| Daemon configuration | `daemon.yaml` at `DaemonConfigPath` (`$home/config/daemon.yaml`) written by solo-provisioner at install — **not CLI flags**. Rationale: (1) the service file is identical on every node; node-specific values live in `daemon.yaml` alone so `ExecStart` has no arguments; (2) config changes (e.g. kubeconfig rotation) only require rewriting the file + `systemctl restart` — no `daemon-reload` or unit file edit; (3) the service file and config file have different owners and lifecycles (`provisioner daemon install` writes both once; only the yaml is rewritten on credential rotation); (4) kubeconfig path does not appear in `ps aux`/`/proc/<pid>/cmdline`; (5) adding new fields costs one line in yaml vs a new cobra flag + `ExecStart` argument on every node. |
| Missing daemon.yaml | `New()` fails fast; systemd `Restart=always` retries after provisioner installs it |
| Kubeconfig re-read | Only on auth errors (401/403), not on every reconnect |
| Backoff | 2 s initial, x2 factor, 5 min cap — matches HIP requirement for robust K8s API reconnect |
| Watch timeout | `TimeoutSeconds: 300` on every Watch call; bounds the reconnect cycle and prevents unbounded server-side streams |
| Clean close handling | Channel close (server-side expiry or silent proxy drop) sleeps `backoffInitial` before reconnecting — prevents hot-loop on transient drops |
| Concurrency invariant | **Single-slot execution**: at most one `handleExecute` runs at any time, regardless of operationId. A second distinct operationId arriving while one is active is rejected with `UpgradeMonitorBusy` — concurrent upgrades on the same node would corrupt InfraConfig files and K8s CRs. |
| Deduplication | Mutex-protected `activeOpID string`; same-operationId duplicate events are ignored; any event while `activeOpID != ""` is rejected |
| Config error types | `ErrConfigNotFound` (with `errorx.NotFound()` trait) for missing file; `ErrConfigMalformed` for parse/validation errors — enables accurate doctor output and exit codes |
| `handleExecute` | Stub — full implementation in subsequent stories (InfraConfig, ConsensusConfig, status patch) |
| Test client injection | Exported `NewUpgradeMonitorWithClient` takes `dynamic.Interface` — no kubeconfig on disk needed in tests |

## Architecture

```
Daemon.Run(ctx)
  +-- Server.Start(ctx)          -- HTTP control plane (unchanged)
  +-- UpgradeMonitor.Run(ctx)    -- NEW: watch loop with backoff
  |     +-- runWatch(ctx)        -- single watch cycle; returns err on disconnect
  |           +-- handleEvent()  -- single-slot guard; spawns goroutine
  |                 +-- handleExecute() -- stub; runs the automa workflow
  +-- MigrationMonitor.Run(ctx)  -- unchanged
```

The daemon reads `daemon.yaml` at startup:

```yaml
node_id:     0.0.3                                              # required; Hedera node identifier; used as nodeId in JSONL events
kubeconfig:  /opt/solo/weaver/sandbox/etc/weaver/kubeconfig    # required; scoped kubeconfig written by solo-provisioner
orbit:       hedera-network                                     # required; CN namespace for CR watch
upgrade_dir: /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current   # optional; this is the default
```

## Changed Files

| File | Change |
|------|--------|
| `internal/daemon/consensus/upgrade_monitor.go` | Full implementation replacing stub |
| `internal/daemon/consensus/upgrade_monitor_test.go` | New: 6 unit tests via fake dynamic client |
| `internal/daemon/consensus/errors.go` | New: `ErrK8sClient`, `ErrWatchFailed` |
| `internal/daemon/consensus/export_test.go` | New: exposes `isAuthError` and `SetOnExecute` for white-box tests |
| `internal/daemon/config.go` | New: `DaemonConfig` + `LoadDaemonConfig` |
| `internal/daemon/errors.go` | New: `ErrConfig`, `ErrConfigNotFound`, `ErrConfigMalformed` |
| `internal/daemon/daemon.go` | `New()` reads config, constructs `UpgradeMonitor` |
| `internal/daemon/export_test.go` | New: `NewWithComponents` test helper (test-only) |
| `pkg/models/weaver_paths.go` | Added `DaemonConfigPath` field |
| `cmd/daemon/main.go` | Returns `daemon.New` errors directly (no double-wrap in `errorx.InternalError`) |

## Out of Scope

- InfraConfig file placement (story after #519)
- Infra upgrade conditional (PendingInfraUpgrade path)
- ConsensusConfig CR creation and reconciliation wait
- Patching `NetworkUpgradeExecute` status to `PendingNodeUpgrade` + `DaemonResult=Succeeded`
- RBAC manifest for the daemon ServiceAccount (goes in solo-operator / helm chart)
- NetworkUpgradeExecute CR retention/TTL policy (HIP amendment pending)
- Upgrade event log (`/opt/solo/weaver/daemon/events/`) — deferred to the story that implements `handleExecute`; event names, payload shape, and append semantics are undefined until the actual upgrade steps are known

## Test Plan

- [ ] Unit: `task vm:test:unit` (UTM VM -- Linux packages required)
- [ ] Targeted: `task test:coverage TEST_PATHS=./internal/daemon/... TEST_REGEX="."`
- [ ] Lint: `task lint`
- [ ] Manual: build daemon, create `daemon.yaml`, confirm `Upgrade monitor started` in logs
- [ ] Manual: create `NetworkUpgradeExecute` CR in phase `ReadyForProvisionerDaemon`; confirm `ExecuteWorkflowStarted` log
- [ ] Manual: send second event for same operationId while first is running; confirm `UpgradeMonitorDuplicateEvent` debug log
- [ ] Manual: send event for different operationId while one is running; confirm `UpgradeMonitorBusy` warn log
