# Review guide — #518 Unix socket server

> **Branch:** `00518-implement-unix-socket-server-with-health`
> **PR base:** `00499-feat-solo-provisioner-daemon-core`
> **Issue:** https://github.com/hashgraph/solo-weaver/issues/518

## Problem and solution

No `internal/daemon/` package existed. This PR adds the Unix domain socket HTTP server
that forms the control plane for `solo-provisioner-daemon`. The daemon is structured as a
thin controller (`Daemon`) composing three sub-systems (`Server`, `UpgradeMonitor`, `SoakWatcher`)
so stories #519 and #520 can implement their respective types in isolation.

## Changed files

| File | Description |
|---|---|
| `pkg/models/weaver_paths.go` | Add `DaemonDir`, `DaemonSockPath` (derived from `DaemonDir`); append to `AllDirectories` |
| `internal/daemon/types.go` | `HealthResponse`, `ErrorResponse`, `SoakStartRequest`, `SoakStartResponse`, `SoakStatusResponse` |
| `internal/daemon/upgrade_monitor.go` | New: `UpgradeMonitor` stub; story #519 implements `Run(ctx)` here |
| `internal/daemon/soak_watcher.go` | New: `SoakWatcher` — all soak state, `TryEnqueue`, `Status`, `Run`, `run`, `resumeIfNeeded` |
| `internal/daemon/daemon.go` | New: thin controller; `Run` is a 3-line errgroup orchestrator |
| `internal/daemon/server.go` | New: Unix socket server; holds `sockPath string` + `*SoakWatcher` |
| `internal/daemon/handlers.go` | New: `handleHealth`, `handleSoakStatus`, `handleSoakStart` |
| `internal/daemon/server_test.go` | New: 6 unit tests over a real unix socket in a temp dir |
| `cmd/daemon/main.go` | Wire existing entrypoint to `daemon.New(models.Paths()).Run(ctx)` |
| `docs/claude/plans/00518-…` | Plan doc (architecture decisions, observability scope) |

## Code review checklist

- [ ] `DaemonSockPath` is derived from `pp.DaemonDir` in post-init block (not a repeated string literal)
- [ ] `DaemonDir` appears in `AllDirectories` — confirms auto-creation on install
- [ ] `Daemon.Run` is a pure orchestrator — no state, no channels directly on `Daemon`
- [ ] `Server` holds `sockPath string` + `*SoakWatcher` + `*http.Server` — not coupled to `Daemon`
- [ ] `http.Server` has `ReadHeaderTimeout: 5s` set
- [ ] `errors.Is(err, http.ErrServerClosed)` used (not `!=`)
- [ ] `SoakWatcher.TryEnqueue` uses `soakActive.Swap(true)` before channel send — closes the race window
- [ ] `SoakWatcher.Status()` returns `*SoakStatusResponse` (pointer, not copy); idle sentinel is package-level var
- [ ] `SoakWatcher.run` has a single outermost `defer` — recovery wraps all cleanup (no LIFO panic shadowing)
- [ ] `soakWg.Wait()` is deferred inside `SoakWatcher.Run` — quiescence guaranteed before return
- [ ] `soakWg.Add(1)` called before goroutine spawn in dispatch loop
- [ ] `resumeIfNeeded` stub documents `soakActive.Store(true)` + `soakWg.Add(1)` invariant for story #520
- [ ] `os.Chmod(0o660)` called after `net.Listen`; chmod failure closes listener, removes socket, and aborts
- [ ] `Shutdown` uses `context.WithTimeout(5s)` — cannot hang indefinitely
- [ ] `errgroup.WithContext` used — server startup failure cancels daemon context
- [ ] `handleSoakStart` caps body with `http.MaxBytesReader(16 KiB)` and validates `node_id` is non-empty
- [ ] All error responses use `writeError` → `ErrorResponse{Error string}` with `Content-Type: application/json`
- [ ] `SoakStartResponse{Accepted bool}` used (not `map[string]bool`)
- [ ] All state-transition log entries include `reason` field
- [ ] No `/upgrade/abort` route — correct per design
- [ ] `server_test.go` uses `require.Eventually` for async soak status polling — no flaky sleep
- [ ] Conflict test asserts `Content-Type: application/json` and decodes `ErrorResponse`
- [ ] `Test_SoakStart_InvalidBody` and `Test_SoakStart_MissingNodeID` cover 400 paths
- [ ] Build tag `!integration` on test file — safe for `task test:unit`

## Test commands

```bash
# Run daemon package unit tests (in UTM VM)
task vm:test:unit

# Targeted coverage
task test:coverage TEST_PATHS=./internal/daemon/... TEST_REGEX="."

# Lint
task lint
```

## Manual UAT (UTM VM)

> **Note:** The daemon socket lives under `/opt/solo/weaver/daemon/` which is created by
> `solo-provisioner install`. Run the install step first, then start the daemon.
> The socket is `0660 root:weaver` — connect as root or a member of the `weaver` group.

```bash
# 1. Build CLI and daemon binaries
task build:cli GOOS=linux GOARCH=arm64
task build:weaver GOOS=linux GOARCH=arm64

# 2. Install solo-provisioner to create the directory structure
sudo ./solo-provisioner-linux-arm64 install

# 3. Start the daemon
sudo ./solo-provisioner-daemon-linux-arm64 &

# 4. Health check
sudo curl --unix-socket /opt/solo/weaver/daemon/daemon.sock http://local/health
# Expected: {"status":"ok"}

# 5. Soak status (idle)
sudo curl --unix-socket /opt/solo/weaver/daemon/daemon.sock http://local/soak/status
# Expected: {"active":false}

# 6. Start soak
sudo curl -X POST --unix-socket /opt/solo/weaver/daemon/daemon.sock http://local/soak/start \
  -H 'Content-Type: application/json' \
  -d '{"node_id":"0.0.3","cutover_timestamp":"2025-01-01T00:00:00Z","migration_plan_path":"/opt/solo/weaver/migration/consensus/0.0.3-20250521T143022Z-migration-plan.yaml"}'
# Expected: {"accepted":true}

# 7. Soak status (active)
sudo curl --unix-socket /opt/solo/weaver/daemon/daemon.sock http://local/soak/status
# Expected: {"active":true,"request":{"node_id":"0.0.3",...}}

# 8. Second start while active → conflict
sudo curl -X POST --unix-socket /opt/solo/weaver/daemon/daemon.sock http://local/soak/start \
  -H 'Content-Type: application/json' \
  -d '{"node_id":"0.0.3","cutover_timestamp":"2025-01-01T00:00:00Z","migration_plan_path":"/opt/solo/weaver/migration/consensus/0.0.3-20250521T143022Z-migration-plan.yaml"}'
# Expected: HTTP 409, Content-Type: application/json, body: {"error":"soak already active or pending"}

# 9. Verify socket permissions (owner+group only)
ls -la /opt/solo/weaver/daemon/daemon.sock
# Expected: srw-rw---- root weaver (0660)
```
