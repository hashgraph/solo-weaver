# Review Guide — #527 Daemon startup readiness via sd_notify

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/527
> **PR base:** `00499-feat-solo-provisioner-daemon-core`

## Summary

The daemon service uses `Type=notify`, so `systemctl start` blocks until `READY=1` is
delivered via `NOTIFY_SOCKET`. Before this PR the daemon never sent it, causing
`systemctl start` to hang until `TimeoutStartSec` (90 s) expired.

This PR adds:
- `internal/daemon/sdnotify.go` — inline `sdNotify(state)` helper writing to `NOTIFY_SOCKET` unixgram; no new vendor dep
- `internal/daemon/sdnotify_test.go` — unit tests for no-op and payload delivery
- `daemon.go` — `probeKubeRBAC` / `probeKubeRBACWithRetry`; `READY=1` after probe; `defer STOPPING=1`

## Changed files

| File | Change |
|---|---|
| `internal/daemon/sdnotify.go` | New — `sdNotify(state string) error`; constants `sdReady`, `sdStopping` |
| `internal/daemon/sdnotify_test.go` | New — 4 unit tests covering no-op, READY payload, STOPPING payload |
| `internal/daemon/daemon.go` | Add `cfg DaemonConfig` field; `probeKubeRBAC`; `probeKubeRBACWithRetry`; sd_notify calls in `Run()` |

## Review checklist

- [ ] `sdNotify` returns `nil` silently when `NOTIFY_SOCKET` is unset (no regression for manual / test runs)
- [ ] `defer sdNotify(sdStopping)` fires on normal exit paths; explicit `sdNotify(sdStopping)` in panic handler before `os.Exit(2)` (os.Exit bypasses defers)
- [ ] `READY=1` is sent **only on probe success** — never as a fallback after timeout; a misconfigured daemon stays in `activating` until systemd's `TimeoutStartSec` marks it failed
- [ ] Probe runs in a background goroutine so the Unix socket server starts immediately; tests that start only `srv.Start(ctx)` are not blocked by the probe
- [ ] `probeKubeRBAC` sets `restCfg.Timeout = 30 s` so a hung API server cannot block a single attempt indefinitely
- [ ] `probeKubeRBAC` uses `SelfSubjectAccessReview` for both `list` and `watch` on `networkupgradeexecutes.hedera.com` in orbit namespace
- [ ] `Daemon.cfg` is populated in `NewFromConfig` (not just `New`)
- [ ] No new vendor dependency (k8s client-go was already used in `internal/daemon/consensus/criteria.go`)

## Test commands

```bash
# Run sdnotify unit tests (UTM VM required for full compile)
go test -tags='!integration' -run 'Test_sdNotify' ./internal/daemon/

# Or via task
task vm:test:unit
```

## Manual UAT (UTM VM)

1. Deploy daemon binary: `scp bin/solo-provisioner-daemon-linux-arm64 vm:/opt/solo/weaver/bin/solo-provisioner-daemon`
2. `sudo systemctl start solo-provisioner-daemon`
   - Expected: command returns promptly (not after 90 s)
3. `systemctl status solo-provisioner-daemon` → `active (running)`
4. `sudo systemctl stop solo-provisioner-daemon`
5. `journalctl -u solo-provisioner-daemon | grep -E 'READY|STOPPING'` — verify both signals logged
