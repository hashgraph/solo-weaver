# #520 — Implement soak-watcher goroutine for migration soak criteria monitoring and auto-decommission

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/520
> **Epic:** #499 — Epic A38_2 — solo-provisioner-daemon Core
> **Story branch:** `00520-story-implement-soak-watcher-goroutine-for`
> **PR base:** `00499-feat-solo-provisioner-daemon-core` (branched @ `1967568`)
> **PR closes:** #520
> **Depends on (must merge first):** #555 (pkg/eventlog, pkg/filepruner, WeaverPaths), #518 (Unix socket server, MigrationMonitor stub), #519 (DaemonConfig with NodeID)

## Sibling stories

| # | Title | State |
|---|---|---|
| #518 | Unix socket server (health, soak/status, soak/start) | CLOSED (merged) |
| #519 | UpgradeMonitor goroutine | OPEN — PR #609 |
| #520 | **This story** — soak-watcher goroutine | OPEN |
| #521 | systemd unit file | OPEN |
| #522 | Install via operator cluster install | OPEN |

**Scope boundary vs #519:** #519 owns the UpgradeMonitor goroutine (K8s watch for `NetworkUpgradeExecute` CR). This story (#520) owns only the MigrationMonitor's `run()` goroutine body — the dispatch loop, `TryEnqueue`, `Status`, and the `MigrationMonitor` struct itself were already scaffolded in #518/#555 and must not be re-written.

---

## Summary

Story #518 scaffolded `MigrationMonitor` with a fully functional dispatch loop (`Run`, `TryEnqueue`, `Status`) but left the per-activation watcher goroutine body (`run`) and the crash-resume function (`resumeIfNeeded`) as stubs that do `<-ctx.Done()`.

This story fills in those two stubs:

1. **`run(ctx, req)`** — polls soak criteria every 15 min (default), emits HIP-defined JSONL milestones to `consensus-migrate-events.jsonl` (opened via `pkg/eventlog.NewAppend`), and auto-triggers decommission when all criteria are green.
2. **`resumeIfNeeded(ctx)`** — reads `cutover-state.jsonl` on daemon startup; if a soak was in progress before the restart, re-activates the watcher exactly as if `TryEnqueue` had been called. Elapsed soak time is calculated from `cutover_timestamp` — no time lost.

## Background — how soak monitoring works in practice

After a consensus node (CN) completes migration cutover, the operator (or automated tooling) calls `POST /migration/consensus/soak/start` with a `SoakStartRequest` containing the node ID, cutover timestamp, and migration plan path. This activates the soak-watcher goroutine, which emits `SoakStarted` and writes `cutover-state.jsonl` for crash recovery.

### Every 15 minutes: the poll loop

The daemon evaluates four criteria on each tick:

| Criterion | Implementation | Status |
|---|---|---|
| `SoakDuration48h` | `time.Since(req.CutoverTimestamp) >= 48*time.Hour` — pure clock math, no I/O | **Real** |
| `UploaderBacklogCleared` | Query record stream uploader — confirms no backlogged events from the old CN are pending upload to mirror nodes | **Stub** (separate story) |
| `NoPodRestarts` | Query K8s API for CN pod restart count since `cutover_timestamp`; true when count is 0 | **Stub** (separate story) |
| `ConsensusParticipationNominal` | Check CN is actively signing/submitting transactions, round participation within bounds | **Stub** (separate story) |

With the current stubs returning `(false, nil)`, **decommission cannot trigger** until the real implementations land — this is intentional safety.

### Events emitted each tick

- **`SoakCheck`** — always emitted, carries a snapshot: `soak_hours`, `uploader_backlog_cleared`, `pod_restarts_since_cutover`, `fleet_nodes_migrated`, `next_check_in_seconds` (reflects actual configured poll interval — not hardcoded)
- **`CriterionMet`** — emitted **once** when a criterion flips `false → true` (idempotent; not repeated on subsequent ticks)
- **`FleetThresholdReached`** — emitted **once** when `/opt/solo/weaver/migration/fleet-threshold-reached` flag file appears

### Decommission gate

Decommission triggers only when **all four criteria return `(true, nil)` AND the fleet threshold flag file is present**. The fleet flag is the operator gate — it prevents a single node from self-decommissioning before the network operator confirms enough of the fleet (≥26/39 nodes) has successfully migrated.

When the gate opens: emit `DecommissionTriggered` → call `Decommissioner.Decommission(ctx, nodeID)` → emit `DecommissionCompleted` → delete `cutover-state.jsonl` → goroutine exits.

### Practical soak timeline

| Time after cutover | Event |
|---|---|
| T+0 | `SoakStarted` emitted; state file written |
| T+15min, T+30min, … | `SoakCheck` every tick; `CriterionMet` events as criteria turn green |
| T+48h | `SoakDuration48h` → true; `CriterionMet` emitted |
| Fleet operator action | Touch `/opt/solo/weaver/migration/fleet-threshold-reached` on ≥26/39 nodes; `FleetThresholdReached` emitted |
| First tick after all green + fleet flag | `DecommissionTriggered` → decommission → `DecommissionCompleted` |

### Crash recovery

If the daemon restarts mid-soak, `resumeIfNeeded` reads `cutover-state.jsonl` on startup and re-activates the goroutine with the **original** `cutover_timestamp`. The 48h clock never resets — elapsed time is measured from the original cutover, not from the restart.


---

## Problem

`MigrationMonitor.run()` currently contains only `<-ctx.Done()` — the goroutine never polls soak criteria, never emits events, and never auto-decommissions. A daemon restart loses all soak state because `resumeIfNeeded` is also a no-op.

---

## Decisions

| Question | Decision |
|---|---|
| Soak criteria definition | Framework (poll loop + per-criterion interface) with four concrete criteria wired: `SoakDuration48h`, `UploaderBacklogCleared`, `NoPodRestarts`, `ConsensusParticipationNominal`. Criteria return `(bool, error)`; on error logged to journald and treated as not-green. |
| Poll interval | **15 min (900s)** per HIP spec (implementation-guide line 181: `SoakCheck` every 15 min). Configurable via `MigrationMonitorConfig.PollInterval` for tests. |
| HIP-defined JSONL event reasons | Per implementation-guide § Migration audit log: `SoakStarted`, `SoakCheck`, `CriterionMet`, `FleetThresholdReached`, `DecommissionTriggered`, `DecommissionCompleted`. K8s Events (out of scope this story): `SoakStarted`, `FleetThresholdReached`, `DecommissionTriggered`, `DecommissionCompleted` (Normal); `SoakCriterionFailed` (Warning). |
| `SoakCheck` event payload | Carries context fields per HIP sample: `soak_hours`, `uploader_backlog_cleared`, `pod_restarts_since_cutover`, `fleet_nodes_migrated`, `next_check_in_seconds`. Stored in `Event.Msg` as a structured string; extra fields added as JSON extension (HIP schema allows context fields). Emitted **unconditionally every tick** (every 15 min) — even if no criterion state changed since the last poll. Serves dual duty: (1) progress snapshot of current criterion states; (2) goroutine liveness heartbeat. **Suppressing this event when state is unchanged is a bug.** Over a 48h window at 15-min intervals this produces ~192 entries. `next_check_in_seconds` must reflect the actual configured poll interval at runtime (`int(mm.pollInterval().Seconds())`) — not hardcoded `900` — so the JSONL is self-describing and coherent with any operator-tuned interval. Canonical liveness alert: `absent_over_time({job="soak-monitor", reason="SoakCheck"}[30m])` fires if no `SoakCheck` seen in 30 min (2× the default poll interval), catching daemon crash or stuck goroutine well before the 48h window expires. |
| Fleet threshold check | Read local flag file `/opt/solo/weaver/migration/fleet-threshold-reached`; if present, emit `FleetThresholdReached` once (idempotent — track in-memory bool to avoid duplicate events). Future: mirror node REST API query (separate story). |
| Decommission trigger | When all criteria green AND fleet threshold reached: emit `DecommissionTriggered`, call `Decommissioner.Decommission(ctx, nodeID)`, emit `DecommissionCompleted`, delete state file, goroutine exits. `NoopDecommissioner` used in this story. |
| State persistence (cutover-state.jsonl) | Written to `DaemonConsensusMigrateEventsDir`. Contains the `SoakStartRequest` JSON. Written atomically (write `.tmp` + rename) after `TryEnqueue` succeeds. Deleted on clean goroutine exit after decommission. NOT deleted on daemon shutdown mid-soak — enables crash recovery. |
| JSONL event logger injection | `MigrationMonitor` takes `*eventlog.EventLogger` at construction time. `daemon.New()` opens via `eventlog.NewAppend(paths.DaemonConsensusMigrateEventsDir, "consensus-migrate-events.jsonl")`; passes `nil` on failure (nil-safe `logMigrateEvent` helper). |
| Remote observability | JSONL is written to the host filesystem (`/opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl`). Alloy runs as a host agent (systemd service) and tails this file via `loki.source.file`, shipping structured events to Loki for Grafana dashboards and alerting — no daemon changes needed. A Prometheus `/metrics` endpoint was considered but ruled out: soak is a one-shot lifecycle event (not a continuously-trending metric), JSONL+Loki already covers history and alerting (e.g. alert when `DecommissionCompleted` never appears after 72h), and a scrape endpoint adds daemon surface area with minimal incremental value. |
| `resumeIfNeeded` idempotency | Validates persisted `SoakStartRequest` before re-activating. If invalid: deletes stale file, logs warn, does NOT fail startup. |
| Existing struct/dispatch loop | **Do not change** `MigrationMonitor` fields, `Run`, `TryEnqueue`, or `Status` — those shipped in #518 and are covered by existing tests. |
| NodeID source | From `DaemonConfig.NodeID` (wired in #519); passed into `MigrationMonitor` at construction time. |
| errorx | `ErrSoakWatcher` type under the `consensus` namespace (already defined in `internal/daemon/consensus/errors.go` from #519). |

---

## Scope

### `internal/daemon/consensus/migration_monitor.go` — fill in stubs

- [ ] Add `MigrationMonitorConfig` struct: `PollInterval time.Duration` (default `900s`), `FleetThresholdPath string` (default `/opt/solo/weaver/migration/fleet-threshold-reached`)
- [ ] Add `Decommissioner` interface: `Decommission(ctx context.Context, nodeID string) error`
- [ ] Update `MigrationMonitor` struct: add `logger *eventlog.EventLogger`, `nodeID string`, `decommissioner Decommissioner`, `cfg MigrationMonitorConfig`, `stateFilePath string`, `criteria []SoakCriterion`
- [ ] Update `NewMigrationMonitor(nodeID string, logger *eventlog.EventLogger, d Decommissioner, cfg MigrationMonitorConfig, stateFilePath string) *MigrationMonitor`
- [ ] Implement `run(ctx, req)`:
  - Emit `SoakStarted` event
  - Write `cutover-state.jsonl` atomically (`writeSoakState` helper)
  - Poll every `cfg.PollInterval` via `time.Ticker`:
    - Evaluate all criteria; emit `CriterionMet` per criterion that turns green for the first time
    - Check fleet threshold flag file; emit `FleetThresholdReached` once when file appears
    - Emit `SoakCheck` with context fields (`soak_hours`, `uploader_backlog_cleared`, `pod_restarts_since_cutover`, `fleet_nodes_migrated`, `next_check_in_seconds`)
    - If all criteria green AND fleet threshold reached: emit `DecommissionTriggered`, call `Decommission`, emit `DecommissionCompleted`, delete state file, return
  - On `ctx.Done()`: log info, do NOT delete state file
  - `SoakCheck` emission is **unconditional on every tick** — emit even if no criterion state changed; do NOT suppress (absence of `SoakCheck` is the failure signal; ~192 entries over 48h is expected and correct)
  - `next_check_in_seconds` in each `SoakCheck` payload = `int(mm.pollInterval().Seconds())` — never hardcode `900`
  - Alloy liveness alert (document in review guide): `absent_over_time({job="soak-monitor", reason="SoakCheck"}[30m])` — 2× default poll interval
- [ ] Implement `resumeIfNeeded(ctx)`:
  - Read + unmarshal `cutover-state.jsonl`; validate with `req.Validate()`
  - If valid: `mm.soakActive.Store(true)`, `mm.soakWg.Add(1)`, `go mm.run(ctx, req)`
  - If invalid/missing: log debug/warn, delete stale file, return
- [ ] `writeSoakState(path string, req SoakStartRequest) error` — atomic write via `.tmp` + rename
- [ ] `logMigrateEvent` nil-safe helper
- [ ] `pollInterval()` helper — returns `cfg.PollInterval` if non-zero, else `900 * time.Second`

### `internal/daemon/consensus/criteria.go` — new file

- [ ] `SoakCriterion` interface: `Name() string`, `Check(ctx context.Context, req SoakStartRequest) (bool, error)`
- [ ] `SoakDuration48h` — returns true when `time.Since(req.CutoverTimestamp) >= 48*time.Hour`
- [ ] `UploaderBacklogCleared` — stub returning `(false, nil)` with TODO for actual check (separate story)
- [ ] `NoPodRestarts` — stub returning `(false, nil)` with TODO
- [ ] `ConsensusParticipationNominal` — stub returning `(false, nil)` with TODO
- [ ] `WithCriteria(criteria ...SoakCriterion) *MigrationMonitor` builder method
- [ ] `allCriteriaGreen(ctx, req) bool` — true only when every criterion returns `(true, nil)`; on error logs warn + treats as not-green

### `internal/daemon/consensus/decommission.go` — new file

- [ ] `NoopDecommissioner` — logs `SoakDecommissionCalled` INFO, returns nil

### `internal/daemon/daemon.go` — wire up

- [ ] Open migrate logger via `eventlog.NewAppend(paths.DaemonConsensusMigrateEventsDir, "consensus-migrate-events.jsonl")`; log warn + pass nil on failure
- [ ] Construct `NewMigrationMonitor(cfg.NodeID, migrateLogger, &NoopDecommissioner{}, MigrationMonitorConfig{}, paths.DaemonConsensusMigrateEventsDir)`
- [ ] Wire default criteria: `.WithCriteria(SoakDuration48h{}, UploaderBacklogCleared{}, NoPodRestarts{}, ConsensusParticipationNominal{})`
- [ ] Close `migrateLogger` deferred in `Run` (if non-nil)

### `internal/daemon/consensus/migration_monitor_test.go` — unit tests

- [ ] `Test_MigrationMonitor_EmitsSoakStarted` — `run()` emits `SoakStarted` to logger
- [ ] `Test_MigrationMonitor_EmitsSoakCheck` — after one poll tick, `SoakCheck` event written with context fields
- [ ] `Test_MigrationMonitor_DecommissionsWhenAllCriteriaGreen` — inject all-true criteria + fleet threshold file; verify `DecommissionTriggered` + `DecommissionCompleted` emitted and `Decommission` called
- [ ] `Test_MigrationMonitor_DoesNotDecommissionUntilFleetThreshold` — all criteria green but no fleet flag file; verify decommission NOT called
- [ ] `Test_MigrationMonitor_EmitsCriterionMet` — criterion transitions false→true; verify `CriterionMet` emitted once (not on subsequent ticks)
- [ ] `Test_MigrationMonitor_ResumesOnRestart` — write valid `cutover-state.jsonl`; call `resumeIfNeeded`; verify watcher goroutine activates
- [ ] `Test_MigrationMonitor_ResumeIgnoresInvalidState` — malformed JSON; verify no goroutine spawned + file deleted
- [ ] `Test_MigrationMonitor_WriteSoakState_Atomic` — state file written atomically
- [ ] `Test_SoakDuration48h_TrueWhenElapsed` / `_FalseWhenNotElapsed`

---

## Out of scope

- Real K8s decommission API call (`NoopDecommissioner` — wired in later story)
- Real implementations of `UploaderBacklogCleared`, `NoPodRestarts`, `ConsensusParticipationNominal` (stubs with TODO)
- K8s Events on ConsensusCapsule (`SoakStarted`, `SoakCriterionFailed`, etc.) — separate story
- `provisioner daemon status` CLI read of migrate events file — separate story
- Mirror node REST API fleet threshold check (flag-file approach used here)
- Any change to `Run`, `TryEnqueue`, `Status`, or existing `MigrationMonitor` fields/tests

---

## Test plan

- [ ] Unit: `task test:unit TEST_PATHS=./internal/daemon/consensus/... TEST_REGEX="^Test_MigrationMonitor"`
- [ ] Full unit suite: `task test:unit` (no regressions)
- [ ] Manual UAT (UTM VM):
  1. Build: `task build:weaver GOOS=linux GOARCH=amd64`
  2. `POST /migration/consensus/soak/start` with `cutover_timestamp` 49h in the past
  3. Touch `/opt/solo/weaver/migration/fleet-threshold-reached`
  4. Within one poll tick (use short `--poll-interval` override), confirm `DecommissionCompleted` in journald
  5. Confirm `consensus-migrate-events.jsonl` contains `SoakStarted` → `SoakCheck` → `CriterionMet` (×N) → `FleetThresholdReached` → `DecommissionTriggered` → `DecommissionCompleted`
  6. Kill daemon mid-soak (cutover 1h ago, not yet 48h); restart; confirm `SoakStarted` re-emitted from `resumeIfNeeded`
- [ ] Lint: `task lint`

---

## Risks / rollbacks

- **`resumeIfNeeded` double-spawn:** guarded by `soakActive.Store(true)` before `go run()`; `TryEnqueue` checks `soakActive` — existing guard from #518.
- **State file corruption:** atomic rename; `resumeIfNeeded` validates before activating, deletes bad files.
- **Poll interval in tests:** inject `MigrationMonitorConfig{PollInterval: 10ms}` to avoid real waits.
- **Fleet threshold false-positive:** flag file presence is the trigger; `NoopDecommissioner` means no irreversible action in this story.
- **Rollback:** safe — `NoopDecommissioner` takes no irreversible action; criteria stubs return false by default.
