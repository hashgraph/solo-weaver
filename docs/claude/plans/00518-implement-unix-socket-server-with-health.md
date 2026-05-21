# #518 — Implement Unix socket server

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/518
> **Epic:** #499 — solo-provisioner-daemon Core
> **Story branch:** `00518-implement-unix-socket-server-with-health`
> **PR base:** `00499-feat-solo-provisioner-daemon-core` (branched from `origin/main` @ `b294657`)
> **PR closes:** #518

## Summary

Implement the Unix domain socket HTTP server that forms the control plane for `solo-provisioner-daemon`.
The server listens at `/opt/solo/weaver/daemon/daemon.sock` and exposes three endpoints used by
the CLI and the soak goroutine (story #520). A38_1 (#498) already landed `cmd/daemon/main.go` on main.

## Problem

No `internal/daemon/` package exists. The Unix socket server, its endpoints, request/response types,
and the daemon socket path are entirely new. `WeaverPaths` has no `DaemonDir` or `DaemonSockPath` fields.

## Architecture — Daemon as controller, sub-systems as owned types

`Daemon` is a thin controller that composes three sub-systems and owns their lifecycle via `Run`.
Each sub-system has its own file, its own state, and its own `Run(ctx) error` method so stories
#519 and #520 can be implemented without touching `daemon.go`.

```
Daemon (controller)
├── *Server          — HTTP control plane (sockPath + *SoakWatcher)
├── *UpgradeMonitor  — K8s watch; story #519 implements Run(ctx)
└── *SoakWatcher     — owns soakStartCh, soakStatus, soakActive, soakWg
                       TryEnqueue() called by handler
                       Status()     called by handler
                       Run(ctx)     dispatch loop + per-activation goroutine
```

## Decisions

| Question | Decision |
|---|---|
| Package location | `internal/daemon/` — new package; daemon concerns stay out of `pkg/` |
| Entrypoint | `cmd/daemon/main.go` already exists from A38_1 (#498); wire it to `daemon.New(paths).Run(ctx)` |
| HTTP transport | `net/http` over `net.Listen("unix", sockPath)` — no extra deps |
| Response encoding | JSON; `Content-Type: application/json` |
| Socket path | `DaemonDir` (`$home/daemon`) + `DaemonSockPath` (derived from `DaemonDir`) in `WeaverPaths` |
| Graceful shutdown | `http.Server.Shutdown` with 5 s timeout on ctx cancel; remove socket file on exit |
| Socket permissions | `os.Chmod(0o660)` after `Listen` — owner+group only, prevents unprivileged access |
| `/upgrade/abort` | **Not on this socket.** Two separate abort paths exist entirely outside the daemon. |
| `/soak/start` dispatch | `SoakWatcher.TryEnqueue` — atomic flag + non-blocking channel send; 409 if either rejects |
| Shared soak status | `atomic.Pointer[SoakStatusResponse]` — type-safe, nil = idle; `Status()` returns pointer (zero copy) |
| Goroutine lifecycle | `errgroup.WithContext` for critical goroutines; server startup failure cancels daemon |
| Quiescence | `soakWg.Wait()` deferred inside `SoakWatcher.Run` — caller gets a fully quiesced daemon |
| Panic recovery | Single outermost `defer` in `SoakWatcher.run` — recovery wraps all cleanup |

## Scope

### `pkg/models/weaver_paths.go`
- [x] Add `DaemonDir string` field (`$home/daemon`)
- [x] Add `DaemonSockPath string` field (derived from `DaemonDir` in post-init block)
- [x] Append `DaemonDir` to `AllDirectories` so `SetupHomeDirectoryStructure` creates it on install

### `internal/daemon/types.go`
- [x] `SoakStartRequest{NodeID, CutoverTimestamp, MigrationPlanPath}`
- [x] `SoakStatusResponse{Active bool, Request *SoakStartRequest}`
- [x] `HealthResponse{Status string}`

### `internal/daemon/upgrade_monitor.go` (new)
- [x] `UpgradeMonitor` type with `Run(ctx) error` stub
- [x] Story #519 implements this file in isolation

### `internal/daemon/soak_watcher.go` (new)
- [x] `SoakWatcher` type owning all soak state
- [x] `TryEnqueue(req) bool` — atomic flag + non-blocking send; documented failure modes
- [x] `Status() *SoakStatusResponse` — zero-copy; idle sentinel via package-level var
- [x] `Run(ctx) error` — dispatch loop; `defer soakWg.Wait()` for quiescence
- [x] `run(ctx, req)` — per-activation goroutine; single outermost defer with recover
- [x] `resumeIfNeeded(ctx)` stub with invariant doc for story #520

### `internal/daemon/daemon.go`
- [x] `Daemon` struct: `paths`, `server`, `upgradeMonitor`, `soakWatcher`
- [x] `New(paths)` — constructs sub-systems, wires `NewServer(sockPath, sw)`
- [x] `Run(ctx)` — 3-line errgroup orchestrator

### `internal/daemon/server.go`
- [x] `Server` struct: `sockPath string`, `sw *SoakWatcher`, `srv *http.Server`
- [x] `NewServer(sockPath, sw)` — registers 3 routes
- [x] `Start(ctx)` — removes stale socket, listens, chmod 0o660, serves, shuts down

### `internal/daemon/handlers.go`
- [x] `handleHealth` → `{"status":"ok"}`
- [x] `handleSoakStatus` → `s.sw.Status()`
- [x] `handleSoakStart` → `s.sw.TryEnqueue(req)`; 409 on false

### `cmd/daemon/main.go`
- [x] Wire existing entrypoint to `daemon.New(models.Paths()).Run(ctx)`

### `internal/daemon/server_test.go`
- [x] `Test_Health` — `GET /health` → 200 `{"status":"ok"}`
- [x] `Test_SoakStatus_Idle` — `GET /soak/status` → `{active:false}`
- [x] `Test_SoakStart_Then_Status` — POST then poll status with `require.Eventually`
- [x] `Test_SoakStart_Conflict_When_Active` — second POST → 409 once watcher is running

## Observability — HIP alignment

The HIP specifies a three-channel observability model for the daemon:

| Channel | Owner | Scope |
|---|---|---|
| Structured logs → journald (daemon stdout) | All goroutines | Every state transition, with `reason` + `nodeId` fields |
| JSONL soak events (`soak-events.jsonl`) | Soak watcher | Reasons: `SoakStarted`, `SoakCheck`, `CriterionMet`, `FleetThresholdReached`, `DecommissionTriggered`, `DecommissionCompleted` |
| K8s Events on `ConsensusCapsule` | Soak watcher | Same lifecycle milestones |

### In scope for #518

- [x] All state-transition log entries include `reason` field
- [x] `reason` values: `ServerStarted`, `ServerStopped`, `UpgradeMonitorStarted`, `UpgradeMonitorStopped`, `SoakStartAccepted`, `SoakStarted`, `SoakStopped`, `SoakPanic`, `SoakDispatcherStopped`, `DaemonStopped`

### Deferred to later stories

- JSONL file writing (`soak-events.jsonl`) — story #520
- K8s Events on `ConsensusCapsule` — story #520
- Upgrade event log (`/opt/solo/weaver/daemon/events/`) — story #519

## Out of scope

- Actual soak-watcher polling logic (story #520)
- Actual upgrade-monitor K8s watch (story #519)
- `sd_notify` READY signal (story #527)
- `POST /upgrade/abort` socket endpoint (does not belong on daemon socket)

## Migration plan path convention

`/opt/solo/weaver/migration/consensus/<node-id>-<ISO8601>-migration-plan.yaml`

Example: `/opt/solo/weaver/migration/consensus/0.0.3-20250521T143022Z-migration-plan.yaml`

## Test plan

- [ ] Unit: `task vm:test:unit` (run inside UTM VM)
- [ ] Targeted: `task test:coverage TEST_PATHS=./internal/daemon/... TEST_REGEX="."`
- [ ] Lint: `task lint`
- [ ] Manual smoke (UTM VM): build daemon, `curl --unix-socket /opt/solo/weaver/daemon/daemon.sock http://local/health`

## Risks / rollbacks

- Stale socket on unclean exit — mitigated: `Start` removes it before `Listen` and in shutdown path.
- Socket world-accessible — mitigated: `os.Chmod(0o660)` after `Listen`; chmod failure aborts startup.
- Double soak activation — mitigated: `soakActive atomic.Bool` + non-blocking channel send.
- Panic in soak watcher — mitigated: single outermost `defer recover()` in `SoakWatcher.run`.
- Daemon continues headless after server startup failure — mitigated: server in `errgroup`, failure cancels ctx.
- Run returns before goroutines quiesce — mitigated: `defer soakWg.Wait()` in `SoakWatcher.Run`.
]