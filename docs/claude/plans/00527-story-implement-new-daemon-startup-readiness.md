# #527 — Implement daemon startup readiness via sd_notify after K8s API confirmed

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/527
> **Epic:** #499 — solo-provisioner-daemon Core
> **Story branch:** `00527-story-implement-new-daemon-startup-readiness`
> **PR base:** `00499-feat-solo-provisioner-daemon-core` (branched from `origin/00499...` @ `daf3430`)
> **PR closes:** #527

## Summary

The daemon service uses `Type=notify`, meaning systemd blocks `systemctl start` until
the daemon sends `READY=1` via the `NOTIFY_SOCKET` Unix datagram socket.
Currently the daemon never sends this signal — `systemctl start solo-provisioner-daemon`
hangs until `TimeoutStartSec` expires.

This PR adds:
- A lightweight `sdnotify` helper (no new vendored dependency — implemented directly
  over `NOTIFY_SOCKET` unixgram writes)
- A K8s RBAC probe in `daemon.Run()` — retries every 2 s until the daemon's
  ServiceAccount is confirmed to have the required permissions; `READY=1` is sent
  only on success. If RBAC is misconfigured the probe keeps retrying until systemd
  cancels the context via `TimeoutStartSec`, which marks the service as failed.
- `STOPPING=1` sent on graceful shutdown and on panic before `os.Exit(2)`

## Problem

`cmd/daemon/main.go` and `internal/daemon/daemon.go:Run()` never call sd_notify.
With `Type=notify` in the service file, `systemctl start` will hang until
`TimeoutStartSec` (default 90 s) elapses and reports a timeout failure.

## Why probe K8s RBAC before signalling READY

The daemon's sole purpose is to autonomously execute upgrade steps during a
maintenance window — with no human watching. If the daemon starts but its
ServiceAccount lacks the required RBAC permissions (wrong ClusterRoleBinding,
wrong orbit namespace, kubeconfig not yet provisioned), it will **silently fail**
at the moment a `NetworkUpgradeExecute` CR lands. By then it is too late:
the upgrade window has already opened and the operator has no visibility.

Probing via `SelfSubjectAccessReview` at startup converts that silent runtime
failure into a **loud startup failure**:

- `systemctl start` hangs / the service stays in `activating` state → operator
  knows immediately the daemon is not operationally ready.
- Structured log messages identify exactly which verb/resource/namespace is
  missing, making diagnosis fast.
- Once the RBAC issue is fixed and the daemon is restarted, `READY=1` confirms
  the daemon is genuinely functional, not just process-alive.

This is the core distinction: `READY=1` should mean *"I can do my job"*, not
*"my process started"*. Using `Type=simple` or sending `READY=1` immediately
after process fork would lose this guarantee entirely.

### No internal probe timeout — by design

There is no internal fallback that sends `READY=1` after a fixed timeout.
If the probe keeps failing (misconfigured RBAC, missing kubeconfig), the daemon
intentionally never signals ready. Systemd's `TimeoutStartSec` (default 90 s)
is the safety net: it cancels the context, the probe goroutine exits, and
systemd marks the service as **failed** — which is exactly the loud startup
failure we want for a broken configuration.

Sending `READY=1` anyway after a timeout would silently promote a misconfigured
daemon to "operational", defeating the entire purpose of the probe.

### Resilience after startup

Once the daemon is running, the UpgradeMonitor and MigrationMonitor have their
own internal retry loops. If the K8s API becomes temporarily unreachable after
startup (e.g. a transient network blip or API server restart), both monitors
will keep retrying with backoff until connectivity is restored — they do not
crash the daemon. This means:

- The startup probe catches **configuration errors** (wrong kubeconfig, missing
  RBAC) that would never self-heal.
- Post-startup retry loops handle **transient failures** (network blips, API
  server restarts) that self-heal without operator intervention.

## Decisions

| Question | Decision |
|---|---|
| sd_notify implementation | Inline `internal/daemon/sdnotify.go` — write to `NOTIFY_SOCKET` unixgram directly; no new vendor dep |
| K8s probe method | `SelfSubjectAccessReview` for `list` + `watch` on `networkupgradeexecutes` (group `hedera.com`) in the orbit namespace — proves the daemon's ServiceAccount has the exact verbs it needs, not just that the API server is reachable |
| Probe retry strategy | Every 2 s, indefinitely, until success or ctx cancelled by systemd `TimeoutStartSec`; `READY=1` sent only on success |
| Probe REST timeout | 30 s per attempt (`restCfg.Timeout`) — matches `upgrade_monitor.go` and `criteria.go`; prevents a hung API server from blocking a single attempt indefinitely |
| Kubeconfig re-read each attempt | Yes — file may not exist at daemon start (written by a later provisioning step); re-reading picks it up the moment it appears |
| Probe runs concurrently | Yes — probe goroutine starts after `eg.Go` calls so the socket server accepts connections immediately; avoids blocking tests with empty kubeconfig |
| Probe kubeconfig | `cfg.Kubeconfig` + `cfg.Orbit` (same values used by UpgradeMonitor) |
| READY=1 call site | Probe goroutine in `daemon.Run()` — only on successful probe |
| STOPPING=1 call site | `daemon.Run()` — `defer` for normal paths; explicit call in panic handler before `os.Exit(2)` (os.Exit bypasses defers) |
| No-op outside systemd | `sdNotify` returns `nil` silently when `NOTIFY_SOCKET` is unset (safe in tests and manual runs) |
| kubernetes.Clientset dep | Already vendored via `k8s.io/client-go` |

## Scope

### `internal/daemon/sdnotify.go` (new)
- [x] `sdNotify(state string) error` — reads `NOTIFY_SOCKET` env var, writes state string via `net.Dial("unixgram", ...)`, no-ops when socket unset
- [x] Constants: `sdReady = "READY=1"`, `sdStopping = "STOPPING=1"`

### `internal/daemon/daemon.go`
- [x] Add `probeKubeRBAC(ctx, kubeconfigPath, namespace string)` — builds `kubernetes.Clientset` with 30 s REST timeout, issues `SelfSubjectAccessReview` for `list` and `watch` on `networkupgradeexecutes` in orbit namespace
- [x] Add `probeKubeRBACWithRetry(ctx, kubeconfigPath, namespace string)` — retry loop every 2 s; returns nil on success, ctx.Err() on cancellation; no internal timeout
- [x] Modify `Run()`: start probe in background goroutine after `eg.Go` calls; send `READY=1` only on probe success; `defer sdNotify(sdStopping)` for normal paths; explicit `sdNotify(sdStopping)` before `os.Exit(2)` in panic handler

### `internal/daemon/sdnotify_test.go` (new)
- [x] `Test_sdNotify_NoopWhenSocketUnset` — verifies no error when `NOTIFY_SOCKET` not set
- [x] `Test_sdNotify_WritesToSocket` — spins up a `unixgram` listener in the test, verifies the correct payload is delivered
- [x] `Test_sdNotify_StoppingPayload` — verifies STOPPING=1 payload delivered correctly
- [x] `Test_sdNotify_NoopWhenSocketEmpty` — verifies no-op when env var is set to empty string

### `internal/daemon/consensus/migration_monitor.go`
- [x] `TryEnqueue` stores `soakStatus{Active: true}` synchronously after channel send — status visible immediately without waiting for the watcher goroutine

### `internal/daemon/server_test.go`
- [x] `startTestDaemonWithConfig` starts only `srv.Start(ctx)` instead of the full daemon — decouples server tests from K8s probe goroutine; adds `errCh` for early failure detection

## Out of scope

- `WatchdogUSec` / `sd_notify("WATCHDOG=1")` keepalive pings
- `ERRNO=` or `STATUS=` extended notify messages

## Test plan

- [x] Unit: `task test:unit` — `Test_sdNotify_*` in `internal/daemon/`
- [ ] Targeted: `task test:coverage TEST_PATHS=./internal/daemon/... TEST_REGEX="."`
- [x] Lint: `task lint`
- [ ] Manual UAT (UTM VM):
  1. Deploy daemon binary to `/opt/solo/weaver/bin/solo-provisioner-daemon`
  2. `sudo systemctl start solo-provisioner-daemon`
  3. Verify: command blocks until RBAC probe succeeds, then returns
  4. `systemctl status solo-provisioner-daemon` → `active (running)`
  5. `sudo systemctl stop solo-provisioner-daemon`
  6. `journalctl -u solo-provisioner-daemon | grep STOPPING` — verify STOPPING logged
  7. Break RBAC (wrong kubeconfig path), restart — verify service stays in `activating` until `TimeoutStartSec` and is marked failed

## Risks / rollbacks

- If K8s is permanently unreachable (misconfigured kubeconfig), the probe retries until
  systemd's `TimeoutStartSec` cancels the context. The service is marked failed — which
  is the correct behaviour. The operator must fix the kubeconfig and restart.
- `sdNotify` silently no-ops outside systemd — no regression for manual/test invocations.
