# Review Guide — #555 Implement shared JSONL event writer

## Problem & Solution

The daemon needs a durable, crash-safe audit trail for upgrade and migration lifecycle
milestones — separate from journald which is ephemeral and operator-facing only.
This story introduces `internal/daemon/eventlog/`, a thin JSONL writer with two modes:
per-operation files for upgrade workflows and a fixed append-only file for migration.

## Changed Files

| File | Description |
|------|-------------|
| `internal/daemon/eventlog/event.go` | New: `Event` struct and `Level` constants (`INFO`/`ERROR`) |
| `internal/daemon/eventlog/logger.go` | New: `EventLogger`, `NewOperation`, `NewAppend`, `Log`, `Close`, `PruneOldest`, `Path()` |
| `internal/daemon/eventlog/logger_test.go` | New: 7 unit tests covering write, truncate, append, concurrent writes, and both retention conditions |
| `pkg/models/weaver_paths.go` | Modified: added `DaemonEventsDir` and `DaemonConsensusMigrateEventsPath`; `DaemonEventsDir` added to `AllDirectories` |
| `docs/claude/plans/00555-implement-shared-jsonl-event-writer.md` | New: full design plan including justification, file strategy, API, error policy, and open questions |

## Review Checklist

- [ ] `NewOperation(dir, operationID)` constructs filename as `consensus-<operationID>.jsonl` — naming convention enforced in one place, not at call sites
- [ ] `NewAppend(dir, fileName)` opens with `O_APPEND` — never truncates an existing migration event file
- [ ] `NewOperation` opens with `O_TRUNC` — each upgrade operation starts with a fresh file
- [ ] `Log()` holds the mutex for the entire write+fsync — concurrent calls cannot interleave partial JSON lines
- [ ] `Log()` fsyncs after every write — entries survive a daemon crash between writes
- [ ] File permission is `0o640` — readable by owner and group, not world-readable
- [ ] `Path()` returns the absolute file path without exposing the underlying `*os.File`
- [ ] `PruneOldest` applies two conditions in order: age filter first (removes files older than `maxAge`), then cap filter (removes oldest-first until `≤keep` files remain)
- [ ] `PruneOldest` sorts by filename ascending — ISO-8601 timestamps in filenames sort chronologically, so oldest = first after sort
- [ ] `PruneOldest` skips files that have already disappeared between glob and stat (race-safe `continue` on stat error)
- [ ] `DaemonEventsDir` is added to `AllDirectories` — daemon startup creates the directory without extra code
- [ ] `DaemonConsensusMigrateEventsPath` is `$home/daemon/events/consensus-migrate-events.jsonl`
- [ ] No K8s or daemon dependencies in `eventlog` — package is importable by migration monitor and UC without pulling in consensus package
- [ ] All new source files carry the SPDX Apache-2.0 header

## Test Commands

```bash
# Unit tests (macOS or UTM VM)
go test ./internal/daemon/eventlog/... -tags='!integration' -race -count=1 -v

# Targeted coverage
task test:coverage TEST_PATHS=./internal/daemon/eventlog/... TEST_REGEX="."
```

## Manual UAT

### Step 1 — Verify NewOperation creates and truncates

```bash
# In a Go playground or scratch main:
dir := "/tmp/eventlog-uat"
os.MkdirAll(dir, 0o750)

l, _ := eventlog.NewOperation(dir, "upgrade-20260415T143000-v0.75.0")
l.Log(eventlog.Event{...reason: "ExecuteWorkflowStarted"...})
l.Close()
# Expected file: /tmp/eventlog-uat/consensus-upgrade-20260415T143000-v0.75.0.jsonl
cat /tmp/eventlog-uat/consensus-upgrade-20260415T143000-v0.75.0.jsonl
# Expected: one JSON line with reason=ExecuteWorkflowStarted

# Open again — must truncate
l2, _ := eventlog.NewOperation(dir, "upgrade-20260415T143000-v0.75.0")
l2.Log(eventlog.Event{...reason: "ExecuteWorkflowCompleted"...})
l2.Close()
cat /tmp/eventlog-uat/consensus-upgrade-20260415T143000-v0.75.0.jsonl
# Expected: one JSON line only — ExecuteWorkflowCompleted (truncated)
```

### Step 2 — Verify NewAppend accumulates

```bash
l, _ := eventlog.NewAppend(dir, "consensus-migrate-events.jsonl")
l.Log(...MigrationStarted...)
l.Close()

l2, _ := eventlog.NewAppend(dir, "consensus-migrate-events.jsonl")
l2.Log(...MigrationCompleted...)
l2.Close()

cat /tmp/eventlog-uat/consensus-migrate-events.jsonl
# Expected: two JSON lines
```

### Step 3 — Verify PruneOldest retention policy

```bash
# Create 6 files, back-date 2 beyond 365 days
# Call PruneOldest(dir, "consensus-upgrade-*.jsonl", 365*24*time.Hour, 3)
# Expected: 2 old files removed by age, 1 more removed by cap → 3 files remain
ls /tmp/eventlog-uat/consensus-upgrade-*.jsonl | wc -l
# Expected: 3
```
