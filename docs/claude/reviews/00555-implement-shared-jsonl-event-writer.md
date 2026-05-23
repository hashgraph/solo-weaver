# Review Guide — #555 Implement shared JSONL event writer

## Problem & Solution

The daemon needs a durable, crash-safe audit trail for upgrade and migration lifecycle
milestones — separate from journald which is ephemeral and operator-facing only.
This story introduces two packages:
- `internal/daemon/eventlog/` — thin JSONL writer with per-operation and append-only modes
- `pkg/filepruner/` — strategy-based file pruner usable by daemon, UC, and any future component

## Changed Files

| File | Description |
|------|-------------|
| `internal/daemon/eventlog/event.go` | New: `Event` struct and `Level` constants (INFO/ERROR) with doc explaining why only two levels exist |
| `internal/daemon/eventlog/errors.go` | New: `ErrInvalidEvent` errorx type |
| `internal/daemon/eventlog/logger.go` | New: `EventLogger`, `NewOperation(dir, operationID)`, `NewAppend(dir, fileName)`, `Log`, `Close`, `Path()` |
| `internal/daemon/eventlog/logger_test.go` | New: unit tests for write, truncate, append, concurrent writes, validation, and Path() absoluteness |
| `pkg/filepruner/pruner.go` | New: `Strategy` interface, `Pruner`, `FilenameTimestampStrategy`, `ModTimeStrategy`, `FileSizeStrategy`, `All`, `Any` |
| `pkg/filepruner/errors.go` | New: `ErrPruneFailed`, `ErrNoTimestamp` errorx types |
| `pkg/filepruner/pruner_test.go` | New: unit tests for all strategies, composites, and cap enforcement |
| `pkg/models/weaver_paths.go` | Modified: added `DaemonEventsDir` and `DaemonConsensusMigrateEventsPath`; `DaemonEventsDir` added to `AllDirectories` |
| `internal/daemon/daemon.go` | Modified: calls `pruneUpgradeEventLogs` on startup via `filepruner` |

## Review Checklist

### eventlog
- [ ] `NewOperation(dir, operationID)` constructs filename as `consensus-<operationID>.jsonl` - naming enforced in one place
- [ ] `NewOperation` opens with `O_TRUNC` - each upgrade operation starts with a fresh file
- [ ] `NewAppend(dir, fileName)` opens with `O_APPEND` - never truncates an existing migration event file
- [ ] `Log()` holds the mutex for the entire write+fsync - concurrent calls cannot interleave partial JSON lines
- [ ] `Log()` fsyncs after every write - entries survive a daemon crash between writes
- [ ] `Log()` validates all fields before writing - `ErrInvalidEvent` returned on missing required fields
- [ ] File permission is `0o640` - readable by owner and group, not world-readable
- [ ] `Path()` returns the absolute file path (via `filepath.Abs`) without exposing the underlying `*os.File`
- [ ] No K8s or daemon dependencies in `eventlog` - importable by migration monitor and UC
- [ ] `Level` constants are INFO and ERROR only - doc comment explains why HIP-level milestones do not need WARN/DEBUG

### filepruner
- [ ] `FilenameTimestampStrategy` uses a sliding window scan to find the timestamp in the filename
- [ ] Timestamp format includes Z suffix: `20060102T150405Z` - matches `consensus-upgrade-20260415T143000Z-v0.75.0.jsonl`
- [ ] Files with no parseable timestamp return `ErrNoTimestamp` from `ShouldPrune` - pruner keeps them (protects `consensus-migrate-events.jsonl`)
- [ ] `Prune` collects all delete errors and returns `errors.Join` - partial pruning visible to callers
- [ ] `ModTimeStrategy` uses `os.Stat` mod time for files without embedded timestamps
- [ ] `FileSizeStrategy` uses `os.Stat` size - files exceeding `MaxBytes` are pruned
- [ ] `All(strategies...)` prunes only when ALL strategies agree (AND logic)
- [ ] `Any(strategies...)` prunes when AT LEAST ONE strategy agrees (OR logic)
- [ ] Cap enforcement sorts ascending by filename so oldest (smallest ISO-8601) are removed first
- [ ] All errors use `errorx` typed errors (`ErrPruneFailed`, `ErrNoTimestamp`) - no bare `fmt.Errorf`

### Wire-up
- [ ] `daemon.New()` calls `pruneUpgradeEventLogs(paths.DaemonEventsDir)` before starting monitors
- [ ] Pruning failure is logged as a warning and does not block daemon startup
- [ ] `DaemonEventsDir` is added to `AllDirectories` - daemon startup creates the directory automatically
- [ ] `DaemonConsensusMigrateEventsPath` is `$home/daemon/events/consensus-migrate-events.jsonl`
- [ ] Post-execute pruning noted as IMPORTANT comment in `handleExecute` stub on #519 branch
- [ ] All new source files carry the SPDX Apache-2.0 header

## Test Commands

```bash
# eventlog unit tests
go test ./internal/daemon/eventlog/... -tags='!integration' -race -count=1 -v

# filepruner unit tests
go test ./pkg/filepruner/... -tags='!integration' -race -count=1 -v

# Coverage
task test:coverage TEST_PATHS=./internal/daemon/eventlog/... TEST_REGEX="."
task test:coverage TEST_PATHS=./pkg/filepruner/... TEST_REGEX="."
```

## Manual UAT

### Step 1 - Verify NewOperation creates and truncates

```go
dir := "/tmp/eventlog-uat"
os.MkdirAll(dir, 0o750)

l, _ := eventlog.NewOperation(dir, "upgrade-20260415T143000Z-v0.75.0")
l.Log(eventlog.Event{Ts: time.Now(), Level: eventlog.LevelInfo, Reason: "ExecuteWorkflowStarted",
    Msg: "started", OperationID: "upgrade-20260415T143000Z-v0.75.0", NodeID: "0.0.3"})
l.Close()
// Expected file: /tmp/eventlog-uat/consensus-upgrade-20260415T143000Z-v0.75.0.jsonl
// One JSON line with reason=ExecuteWorkflowStarted

// Open again - must truncate
l2, _ := eventlog.NewOperation(dir, "upgrade-20260415T143000Z-v0.75.0")
l2.Log(eventlog.Event{..., Reason: "ExecuteWorkflowCompleted"})
l2.Close()
// One JSON line only - ExecuteWorkflowCompleted (previous line gone)
```

### Step 2 - Verify NewAppend accumulates

```go
l, _ := eventlog.NewAppend(dir, "consensus-migrate-events.jsonl")
l.Log(eventlog.Event{..., Reason: "MigrationStarted"})
l.Close()

l2, _ := eventlog.NewAppend(dir, "consensus-migrate-events.jsonl")
l2.Log(eventlog.Event{..., Reason: "MigrationCompleted"})
l2.Close()
// Two JSON lines (MigrationStarted + MigrationCompleted)
```

### Step 3 - Verify filepruner retention policy

```bash
mkdir /tmp/pruner-uat
# 2 old (2024) + 4 recent (2026); cap=3
touch /tmp/pruner-uat/consensus-upgrade-20240101T000000Z-v0.70.0.jsonl
touch /tmp/pruner-uat/consensus-upgrade-20240601T000000Z-v0.71.0.jsonl
touch /tmp/pruner-uat/consensus-upgrade-20260101T000000Z-v0.72.0.jsonl
touch /tmp/pruner-uat/consensus-upgrade-20260201T000000Z-v0.73.0.jsonl
touch /tmp/pruner-uat/consensus-upgrade-20260301T000000Z-v0.74.0.jsonl
touch /tmp/pruner-uat/consensus-upgrade-20260415T143000Z-v0.75.0.jsonl
# Apply: 2 removed by age, 1 more by cap -> 3 remain
ls /tmp/pruner-uat/consensus-upgrade-*.jsonl | wc -l
# Expected: 3
```

### Step 4 - Verify consensus-migrate-events.jsonl is never pruned

```bash
# FilenameTimestampStrategy on a dir containing consensus-migrate-events.jsonl
# ShouldPrune returns ErrNoTimestamp -> pruner keeps it
ls /tmp/pruner-uat/consensus-migrate-events.jsonl
# Expected: file still present
```
