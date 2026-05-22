# Plan — Upgrade Event Logger (JSONL)

> **Depends on:** First story that implements `handleExecute` steps (InfraConfig placement, ConsensusConfig CR, status patch)
> **HIP channel:** Upgrade event log (`/opt/solo/weaver/daemon/upgrade-events.jsonl`)
> **Related:** CR retention HIP amendment (pending), soak event log (#520)

## Problem

The HIP defines a three-channel observability model for the daemon. Channel 1 (structured
logs to journald) is already implemented. Channel 2 (upgrade JSONL event log) is not.

Journald is insufficient as the sole audit record:
- It rotates and may not persist across reboots depending on host configuration
- It is verbose — separating business milestones from operational noise requires log parsing
- `provisioner daemon check` needs a machine-readable local file to report last upgrade
  status without journald access
- A crash mid-upgrade leaves no reliable record of the last completed step in journald;
  an `O_APPEND` + `fsync` file survives the crash and points to where recovery should start

## HIP-Defined Events

All events share the base fields: `ts`, `level`, `reason`, `msg`, `operationId`, `nodeId`.

| Reason                    | Level   | When emitted |
|---------------------------|---------|--------------|
| `ExecuteWorkflowStarted`  | INFO    | `handleExecute` begins |
| `FilesPlaced`             | INFO    | All `external-files.yaml` assets and `infrastructure-versions.yaml` written to host filesystem |
| `ExecuteWorkflowCompleted`| INFO    | Workflow finished; `PendingNodeUpgrade` signaled on the CR |
| `ExecuteWorkflowFailed`   | ERROR   | Workflow halted by an unrecoverable error; manual intervention required |

Example JSONL entries:

```jsonl
{"ts":"2026-04-15T14:30:00Z","level":"INFO","reason":"ExecuteWorkflowStarted","msg":"Execute workflow triggered by ReadyForProvisionerDaemon; beginning upgrade steps","operationId":"upgrade-20260415T143000-v0.75.0","nodeId":"0.0.3"}
{"ts":"2026-04-15T14:30:05Z","level":"INFO","reason":"FilesPlaced","msg":"InfraConfig and infrastructure-versions.yaml placed on host filesystem","operationId":"upgrade-20260415T143000-v0.75.0","nodeId":"0.0.3"}
{"ts":"2026-04-15T14:31:02Z","level":"INFO","reason":"ExecuteWorkflowCompleted","msg":"Execute workflow finished; PendingNodeUpgrade signaled","operationId":"upgrade-20260415T143000-v0.75.0","nodeId":"0.0.3"}
```

## Design

### Package

`internal/daemon/eventlog/`

Kept separate from `internal/daemon/consensus/` so the soak watcher (#520) and any
future daemon sub-system can share the same writer without importing the consensus package.

### Types

```go
// Level is the severity of an event — mirrors HIP-defined values.
type Level string

const (
    LevelInfo  Level = "INFO"
    LevelError Level = "ERROR"
)

// Event is a single upgrade lifecycle milestone written to the JSONL file.
// All fields are required; zero values produce invalid entries.
type Event struct {
    Ts          time.Time `json:"ts"`
    Level       Level     `json:"level"`
    Reason      string    `json:"reason"`
    Msg         string    `json:"msg"`
    OperationID string    `json:"operationId"`
    NodeID      string    `json:"nodeId"`
}
```

### EventLogger

```go
type EventLogger struct {
    mu   sync.Mutex
    file *os.File  // opened with O_WRONLY|O_APPEND|O_CREATE, perm 0o640
}

// New opens (or creates) the JSONL file at path.
func New(path string) (*EventLogger, error)

// Log appends one JSON line and fsyncs. Returns an error if I/O fails.
// Callers must decide whether to fail the upgrade step or warn and continue.
func (l *EventLogger) Log(e Event) error

// Close flushes and closes the underlying file.
func (l *EventLogger) Close() error
```

### Error handling policy

If `Log()` returns an error (disk full, permissions lost):
- **Do not silently drop** — the whole point of the file is crash-safe durability
- **Log a warn to journald** with `reason=EventLogWriteFailed` and the underlying error
- **Continue the upgrade** — a missing audit trail is bad; a halted upgrade is worse
- The operator can investigate via journald; the JSONL gap is bounded and recoverable

### File location

New field on `WeaverPaths`:

```go
DaemonUpgradeEventsPath string  // $home/daemon/upgrade-events.jsonl
```

One append-only file per orbit (not per operation). Rotation: lumberjack with
`MaxSize=10MB`, `MaxBackups=5`, `MaxAge=90` (aligns with proposed CR retention window).

### Wire-up

`EventLogger` constructed once in `daemon.New()` and injected into `UpgradeMonitor`:

```go
type UpgradeMonitor struct {
    // ...
    eventLog *eventlog.EventLogger  // nil-safe; no-ops if nil (tests)
}
```

Nil-safe `log()` helper on `UpgradeMonitor` avoids nil checks at every call site:

```go
func (um *UpgradeMonitor) logEvent(e eventlog.Event) {
    if um.eventLog == nil {
        return
    }
    if err := um.eventLog.Log(e); err != nil {
        logx.As().Warn().Err(err).Str("reason", "EventLogWriteFailed").
            Msg("Failed to write upgrade event — continuing")
    }
}
```

### K8s Events (second HIP channel)

K8s Events on `NetworkUpgradeExecute` (`ExecuteWorkflowStarted`, `ExecuteWorkflowCompleted`,
`ExecuteWorkflowFailed`) are a separate concern and must NOT go through `EventLogger`.
They are emitted via a K8s client `record.EventRecorder` call directly in `handleExecute`.
This keeps the JSONL logger free of K8s dependencies.

## Scope

### In scope

- `internal/daemon/eventlog/logger.go` — `EventLogger`, `New`, `Log`, `Close`
- `internal/daemon/eventlog/event.go` — `Event`, `Level` constants
- `internal/daemon/eventlog/logger_test.go` — unit tests (temp file, concurrent writes, fsync)
- `pkg/models/weaver_paths.go` — add `DaemonUpgradeEventsPath`
- `internal/daemon/daemon.go` — construct `EventLogger`, inject into `UpgradeMonitor`
- `internal/daemon/consensus/upgrade_monitor.go` — add `eventLog` field, `logEvent` helper,
  emit `ExecuteWorkflowStarted` / `ExecuteWorkflowCompleted` / `ExecuteWorkflowFailed`

### Out of scope

- Soak event log (`soak-events.jsonl`) — story #520 owns its own `EventLogger` instance
- K8s Event emission — belongs in the `handleExecute` implementation story
- Log rotation configuration in `daemon.yaml` — hardcoded constants are sufficient

## Open Questions for HIP Editors

1. **`nodeId` source**: where does the daemon get `nodeId` (e.g. `0.0.3`)? Is it in
   `daemon.yaml`, derived from the orbit namespace, or read from the `NetworkUpgradeExecute`
   CR spec? The current CR spec only has `operationId` and `orbit`.

2. **File I/O failure policy**: the plan above continues the upgrade on write failure.
   Should the HIP mandate halt-on-log-failure for strict auditability?

3. **Single file vs per-operation file**: one append-only file per orbit is proposed.
   The HIP does not specify. Per-operation files are easier to correlate but harder to
   tail across upgrades.
