# Review Guide — #520 Soak-Watcher Goroutine for MigrationMonitor

> **Branch:** `00520-story-implement-soak-watcher-goroutine-for`
> **PR base:** `00499-feat-solo-provisioner-daemon-core`

## Problem & Solution

The `MigrationMonitor` in `internal/daemon/consensus/migration_monitor.go` had two stub bodies —
`run()` and `resumeIfNeeded()` — with TODO comments. This PR fills them in:

- `run()` starts a polling loop that evaluates soak criteria every 15 min (configurable),
  emits HIP-defined JSONL events on every tick and on every state transition, and triggers
  decommission when all criteria are green and the fleet threshold flag is present.
- `resumeIfNeeded()` reads the persisted `cutover-state.jsonl` on daemon startup and
  re-activates `run()` — crash recovery without operator intervention.
- `NoPodRestarts` soak criterion implemented with the K8s typed client.
- Event reason strings moved to typed constants (two blocks: HIP-defined JSONL vs journald-only).
- `SoakDuration` parameterized (zero Period defaults to 48 h per HIP spec).
- GitHub issues created for deferred stubs: #620 (UploaderBacklogCleared), #621 (ConsensusParticipationNominal), #622 (real Decommissioner).

## Changed Files

| File | Change |
|---|---|
| `internal/daemon/consensus/migration_monitor.go` | Implements `run()` and `resumeIfNeeded()`; typed event-reason constants; `pollInterval()`, `fleetThresholdPath()`, `writeSoakState()`, `allCriteriaGreen()` helpers |
| `internal/daemon/consensus/criteria.go` | New file — `SoakCriterion` interface, `SoakDuration`, `NoPodRestarts` (real K8s typed client), `UploaderBacklogCleared` / `ConsensusParticipationNominal` stubs, `buildTypedClient` |
| `internal/daemon/consensus/decommission.go` | New file — `Decommissioner` interface, `NoopDecommissioner` stub |
| `internal/daemon/consensus/errors.go` | Added `ErrSoakWatcher` error type |
| `internal/daemon/consensus/export_test.go` | Added `NewNoPodRestartsWithClient` test helper |
| `internal/daemon/consensus/criteria_test.go` | New file — unit tests for `NoPodRestarts` (fake K8s client) and `SoakDuration` |
| `internal/daemon/consensus/migration_monitor_test.go` | New file — 9 unit tests for `run()`, `resumeIfNeeded()`, and criteria integration |
| `internal/daemon/daemon.go` | Opens `migrateLogger`; wires `NewMigrationMonitorWith` + `WithCriteria` including `NoPodRestarts{...}` |

## Code Review Checklist

- [ ] `SoakCheck` event is emitted **unconditionally** on every tick — even when no criterion is green.
      A missing `SoakCheck` in Loki triggers the liveness alert; suppressing it on failure is a bug.
- [ ] `next_check_in_seconds` in `SoakCheck` is `int(mm.pollInterval().Seconds())` — never hardcoded 900.
- [ ] HIP-defined JSONL reasons are **exported** constants; journald-only reasons are unexported.
      Changing JSONL reason strings is a breaking change for Alloy / Loki.
- [ ] `resumeIfNeeded()` validates the state file with `Validate()` before calling `run()`.
      Invalid state emits `SoakStateCorrupted` (JSONL) and returns without panicking.
- [ ] `writeSoakState()` uses atomic write (`.tmp` + rename) — no partial state file on crash.
- [ ] State file is **not** deleted on daemon shutdown; only on successful decommission — enables crash recovery.
- [ ] `NoPodRestarts` considers only pods created **after** `req.CutoverTimestamp`. Pre-cutover pods with
      restarts must not block the criterion.
- [ ] `NoPodRestarts` returns `(false, nil)` when no post-cutover pod exists yet (not an error).
- [ ] `buildTypedClient` timeout is 30 s — mirrors `buildDynamicClient` in `upgrade_monitor.go`.
- [ ] `SoakDuration.Check()` uses `time.Since(req.CutoverTimestamp) >= period` (>=, not >).
- [ ] `allCriteriaGreen()` returns false on the first criterion error (logs a warning, does not decommission).
- [ ] `NoopDecommissioner.Decommission()` logs at Info level and returns nil — safe no-op.
- [ ] `NoPodRestarts.PodLabelSelector` in `daemon.go` has a TODO for exact label keys pending solo-operator spec.

## Test Commands

```bash
# Unit tests (macOS-safe)
task test:unit

# Targeted with coverage
task test:coverage TEST_PATHS=./internal/daemon/consensus/... TEST_REGEX="."

# Lint
task lint
```

## Manual UAT (UTM VM)

```bash
# 1. Build the daemon binary
task build:weaver GOOS=linux GOARCH=amd64

# 2. Deploy daemon binary and daemon.yaml (with node_id, kubeconfig, orbit)

# 3. Trigger a soak via the socket
curl -s --unix-socket /opt/solo/weaver/daemon/daemon.sock \
  -X POST http://local/migration/consensus/soak/start \
  -H 'Content-Type: application/json' \
  -d '{"node_id":"0.0.3","cutover_timestamp":"2025-01-01T00:00:00Z","migration_plan_path":"/tmp/plan.yaml"}'
# Expected: {"accepted":true}

# 4. Watch the JSONL event log
tail -f /opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl
# Expected: SoakStarted event, then SoakCheck every 15 min

# 5. Check soak status
curl -s --unix-socket /opt/solo/weaver/daemon/daemon.sock \
  http://local/migration/consensus/soak/status | jq .
# Expected: {"state":"soak_active", "request":{...}}

# 6. Restart the daemon; verify SoakResumed event emitted
systemctl restart solo-provisioner-daemon
grep SoakResumed /opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl
```
