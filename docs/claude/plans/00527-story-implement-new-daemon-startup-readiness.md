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
- A K8s API probe in `daemon.Run()` — retries for up to 60 s; sends `READY=1` once
  the API server is reachable (or after the timeout, to avoid hanging `systemctl start`)
- `STOPPING=1` sent on graceful shutdown when the context is cancelled

## Problem

`cmd/daemon/main.go` and `internal/daemon/daemon.go:Run()` never call sd_notify.
With `Type=notify` in the service file, `systemctl start` will hang until
`TimeoutStartSec` (default 90 s) elapses and reports a timeout failure.

## Decisions

| Question | Decision |
|---|---|
| sd_notify implementation | Inline `internal/daemon/sdnotify.go` — write to `NOTIFY_SOCKET` unixgram directly; no new vendor dep |
| K8s probe method | `SelfSubjectAccessReview` for `list` + `watch` on `networkupgradeexecutes` (group `hedera.com`) in the orbit namespace — proves the daemon's ServiceAccount has the exact verbs it needs, not just that the API server is reachable |
| Probe retry strategy | Every 2 s, up to 60 s total (30 attempts); send `READY=1` regardless after timeout to avoid blocking `systemctl start` |
| Probe kubeconfig | `cfg.Kubeconfig` + `cfg.Orbit` (same values used by UpgradeMonitor) |
| READY=1 call site | `daemon.Run()` — after probe, before `eg.Wait()` |
| STOPPING=1 call site | `daemon.Run()` — via `defer` so it fires on any return path (ctx cancel or error) |
| No-op outside systemd | `sdNotify` returns `nil` silently when `NOTIFY_SOCKET` is unset (safe in tests and manual runs) |
| kubernetes.Clientset dep | Already vendored via `k8s.io/client-go` |

## Scope

### `internal/daemon/sdnotify.go` (new)
- [ ] `sdNotify(state string) error` — reads `NOTIFY_SOCKET` env var, writes state string via `net.Dial("unixgram", ...)`, no-ops when socket unset
- [ ] Constants: `sdReady = "READY=1"`, `sdStopping = "STOPPING=1"`

### `internal/daemon/daemon.go`
- [ ] Add `probeKubeRBAC(ctx, kubeconfigPath, namespace string)` — builds `kubernetes.Clientset`, issues `SelfSubjectAccessReview` for `list` and `watch` on `networkupgradeexecutes` in orbit namespace
- [ ] Add `probeKubeRBACWithRetry(ctx, kubeconfigPath, namespace string, timeout time.Duration)` — retry wrapper; logs warning and returns after timeout
- [ ] Modify `Run()`: call `probeKubeRBACWithRetry`, then `sdNotify(sdReady)`; add `defer sdNotify(sdStopping)` at top of `Run()`

### `internal/daemon/sdnotify_test.go` (new)
- [ ] `Test_sdNotify_NoopWhenSocketUnset` — verifies no error when `NOTIFY_SOCKET` not set
- [ ] `Test_sdNotify_WritesToSocket` — spins up a `unixgram` listener in the test, verifies the correct payload is delivered

## Out of scope

- `WatchdogUSec` / `sd_notify("WATCHDOG=1")` keepalive pings
- `ERRNO=` or `STATUS=` extended notify messages
- Blocking `systemctl start` indefinitely (probe timeout releases READY after 60 s)

## Test plan

- [ ] Unit: `task test:unit` — `Test_sdNotify_*` in `internal/daemon/`
- [ ] Targeted: `task test:coverage TEST_PATHS=./internal/daemon/... TEST_REGEX="."`
- [ ] Lint: `task lint`
- [ ] Manual UAT (UTM VM):
  1. Deploy daemon binary to `/opt/solo/weaver/bin/solo-provisioner-daemon`
  2. `sudo systemctl start solo-provisioner-daemon`
  3. Verify: command returns promptly (not after 90 s timeout)
  4. `systemctl status solo-provisioner-daemon` → `active (running)`
  5. `sudo systemctl stop solo-provisioner-daemon`
  6. `journalctl -u solo-provisioner-daemon | grep STOPPING` — verify STOPPING logged

## Risks / rollbacks

- If K8s is permanently unreachable (misconfigured kubeconfig), probe times out after 60 s
  and READY=1 is sent anyway — daemon starts but UpgradeMonitor/MigrationMonitor will
  keep retrying internally (existing behavior, unchanged).
- `sdNotify` silently no-ops outside systemd — no regression for manual/test invocations.
