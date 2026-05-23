# Plan — Daemon Event Logger (JSONL)

> **Depends on:** First story that implements `handleExecute` steps (InfraConfig placement, ConsensusConfig CR, status patch)
> **HIP channels:** Upgrade event log + Migration event log under `daemon/events/`
> **Related:** CR retention HIP amendment (pending), UC event log (separate component)

## Why JSONL in Addition to Structured Logs

The daemon already emits structured JSON logs via `logx` (zerolog), forwarded to journald.
JSONL event files serve a distinct and complementary purpose:

**What structured logs (journald) give you:**
- Real-time operational visibility — watch attempts, backoff, auth errors, dedup rejections
- Verbose — everything the daemon does
- Ephemeral — journald rotates and may not persist across reboots depending on host configuration
- Consumed by: operators tailing logs, alerting on log patterns

**What the JSONL event file gives you:**
- Business-level milestone record — what upgrade operations happened and where they stopped
- Sparse — only 4–5 events per upgrade (`ExecuteWorkflowStarted`, `FilesPlaced`, `ExecuteWorkflowCompleted`/`Failed`)
- Durable — written with `O_APPEND + fsync`; survives daemon crashes and restarts
- Consumed by: `provisioner daemon check` (reads the last event to report upgrade status), support diagnostics, post-mortem audit

**The key distinction that justifies both:**

If the daemon crashes mid-upgrade, journald may not reliably tell you where it stopped.
The JSONL file will — the last line is the last milestone reached before the crash. That is
the crash-safe audit trail the HIP references.

Additionally, `provisioner daemon check` reading a predictable local file is simpler and
more reliable than parsing journald output.

**HIP mandate:** the HIP defines the event schema, file location, and retention as a
contractual interface — the JSONL files are not optional.

## Decision

The daemon writes two categories of JSONL event logs using different file strategies.
All daemon event filenames are prefixed with `consensus-` to namespace the `events/`
directory as the daemon gains support for additional node types (block node, relay, etc.)
in future.

## File Strategy

### Upgrade Workflow — Per-Operation File

```
/opt/solo/weaver/daemon/events/consensus-upgrade-20260415T143000-v0.75.0.jsonl
```

One file per upgrade operation (`consensus-upgrade-<ts>-<ver>.jsonl`).

**Why per-operation:**
- One file = one incident. Trivial to isolate, grep, share, or attach to a post-mortem.
- Retention is clean and bounded: on daemon startup, files older than 365 days are
  removed, then oldest-first until at most 50 remain (whichever limit is hit first).
- Corruption blast radius is limited to the current operation's file.

**Note on filename vs operationId:** The `operationId` (e.g. `upgrade-20260415T143000-v0.75.0`)
does not carry the `consensus-` prefix — it is a semantic identifier used in CR labels
and event payloads. The filename prefix is a filesystem namespace convention only.

**Bookend events (written to this file):**
- `ExecuteWorkflowStarted` — first entry; written on `ReadyForProvisionerDaemon` detection
- `ExecuteWorkflowCompleted` / `ExecuteWorkflowFailed` — terminal entry

Internal UpgradeMonitor watch-loop transitions (backoff, reconnect, auth errors) go to
journald only — they are operational noise, not business milestones.

**Retention:** on daemon startup, apply the unified policy: delete all
`consensus-upgrade-*.jsonl` files older than 365 days, then delete oldest-first
until at most 50 files remain. Both thresholds are enforced together — a file is kept
only if it is within 365 days **and** within the 50-file cap. Sorted by filename
timestamp (ISO-8601 embedded in the name). No runtime rotation needed.

### Migration Workflow — Fixed Append-Only File

```
/opt/solo/weaver/daemon/events/consensus-migrate-events.jsonl
```

**Why fixed file:** Migration soak is a long-running continuous process potentially
spanning days. There is no clean "one operationId = one file" boundary. A single fixed
file fits naturally; the `operationId` field in every entry scopes events to a specific
migration. Not subject to rotation.

### UC Events (reference — not owned by this plan)

```
/opt/solo/weaver/uc/events/upgrade-<ts>-<ver>.jsonl
```

UC runs as a sidecar on the consensus node pod — it is always consensus-scoped by
context. No `consensus-` prefix needed. Same per-operation strategy and identical retention policy (365 days / 50-file cap)
applied on UC startup — UC and the daemon share the same hostPath mount and upgrade
cadence, so a unified policy avoids divergence.

### Summary Table

| Component | Workflow  | File strategy      | Path                                              | Retention         |
|-----------|-----------|--------------------|---------------------------------------------------|-------------------|
| Daemon    | Upgrade   | Per-operation      | `daemon/events/consensus-upgrade-<ts>-<ver>.jsonl` | ≤365 days & ≤50 files on startup |
| Daemon    | Migration | Fixed append-only  | `daemon/events/consensus-migrate-events.jsonl`    | None              |
| UC        | Upgrade   | Per-operation      | `uc/events/upgrade-<ts>-<ver>.jsonl`              | ≤365 days & ≤50 files on startup |

## HIP-Defined Events

All events share the base fields: `ts`, `level`, `reason`, `msg`, `operationId`, `nodeId`.

| Reason                     | Level   | File                          | When emitted |
|----------------------------|---------|-------------------------------|--------------|
| `ExecuteWorkflowStarted`   | INFO    | `consensus-upgrade-*.jsonl`   | `handleExecute` begins |
| `FilesPlaced`              | INFO    | `consensus-upgrade-*.jsonl`   | All assets written to host filesystem |
| `ExecuteWorkflowCompleted` | INFO    | `consensus-upgrade-*.jsonl`   | Workflow finished; `PendingNodeUpgrade` signaled |
| `ExecuteWorkflowFailed`    | ERROR   | `consensus-upgrade-*.jsonl`   | Workflow halted; manual intervention required |

Example file `consensus-upgrade-20260415T143000-v0.75.0.jsonl`:

```jsonl
{"ts":"2026-04-15T14:30:00Z","level":"INFO","reason":"ExecuteWorkflowStarted","msg":"Execute workflow triggered by ReadyForProvisionerDaemon; beginning upgrade steps","operationId":"upgrade-20260415T143000-v0.75.0","nodeId":"0.0.3"}
{"ts":"2026-04-15T14:30:05Z","level":"INFO","reason":"FilesPlaced","msg":"InfraConfig and infrastructure-versions.yaml placed on host filesystem","operationId":"upgrade-20260415T143000-v0.75.0","nodeId":"0.0.3"}
{"ts":"2026-04-15T14:31:02Z","level":"INFO","reason":"ExecuteWorkflowCompleted","msg":"Execute workflow finished; PendingNodeUpgrade signaled","operationId":"upgrade-20260415T143000-v0.75.0","nodeId":"0.0.3"}
```

## Package Design

### Package

`internal/daemon/eventlog/`

Kept separate from `internal/daemon/consensus/` so the migration monitor (#520) and any
future daemon sub-system can share the same writer without importing the consensus package.

### Types

```go
// Level is the severity of an event — mirrors HIP-defined values.
type Level string

const (
    LevelInfo  Level = "INFO"
    LevelError Level = "ERROR"
)

// Event is a single lifecycle milestone written to a JSONL file.
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

// NewOperation creates a new per-operation JSONL file at path and returns a logger.
// The caller is responsible for calling Close() when the operation completes.
func NewOperation(path string) (*EventLogger, error)

// NewAppend opens (or creates) a fixed append-only JSONL file at path.
func NewAppend(path string) (*EventLogger, error)

// Log appends one JSON line and fsyncs. Returns an error if I/O fails.
func (l *EventLogger) Log(e Event) error

// Close flushes and closes the underlying file.
func (l *EventLogger) Close() error

// PruneOldest applies the retention policy to per-operation JSONL files matching
// glob in dir: first removes files older than maxAge, then removes oldest-first
// until at most keep files remain. Called on daemon and UC startup.
func PruneOldest(dir, glob string, maxAge time.Duration, keep int) error
```

### Error handling policy

If `Log()` returns an error (disk full, permissions lost):
- **Do not silently drop** — the file is the crash-safe audit trail
- **Log a warn to journald** with `reason=EventLogWriteFailed` and the underlying error
- **Continue the upgrade** — a missing audit trail is bad; a halted upgrade is worse
- The operator can investigate via journald; the JSONL gap is bounded and recoverable

### File paths on WeaverPaths

```go
DaemonEventsDir                  string  // $home/daemon/events
// Per-operation files are constructed at runtime:
// filepath.Join(DaemonEventsDir, "consensus-upgrade-"+operationID+".jsonl")
// Fixed file:
DaemonConsensusUpgradeEventsDir  string  // same as DaemonEventsDir (used for pruning glob)
DaemonConsensusMigrateEventsPath string  // $home/daemon/events/consensus-migrate-events.jsonl
```

`DaemonEventsDir` is added to `AllDirectories` so daemon startup creates it.

### Wire-up

`EventLogger` for migration is constructed once in `daemon.New()` and injected into
`MigrationMonitor`. For upgrade, a new per-operation logger is created inside
`handleExecute` for each operation and closed when the operation completes.

Nil-safe `logEvent` helper on `UpgradeMonitor` avoids nil checks at every call site:

```go
func (um *UpgradeMonitor) logEvent(log *eventlog.EventLogger, e eventlog.Event) {
    if log == nil {
        return
    }
    if err := log.Log(e); err != nil {
        logx.As().Warn().Err(err).Str("reason", "EventLogWriteFailed").
            Msg("Failed to write upgrade event — continuing")
    }
}
```

### K8s Events (second HIP channel)

K8s Events on `NetworkUpgradeExecute` are a separate concern and must NOT go through
`EventLogger`. They are emitted via a K8s client `record.EventRecorder` call directly
in `handleExecute`. This keeps the JSONL logger free of K8s dependencies.

## Scope

### In scope

- `internal/daemon/eventlog/logger.go` — `EventLogger`, `NewOperation`, `NewAppend`, `Log`, `Close`, `PruneOldest`
- `internal/daemon/eventlog/event.go` — `Event`, `Level` constants
- `internal/daemon/eventlog/logger_test.go` — unit tests (temp file, concurrent writes, fsync, pruning)
- `pkg/models/weaver_paths.go` — add `DaemonEventsDir`, `DaemonConsensusMigrateEventsPath`
- `internal/daemon/daemon.go` — construct migration `EventLogger`, inject into `MigrationMonitor`; call `PruneOldest` on startup
- `internal/daemon/consensus/upgrade_monitor.go` — create per-operation logger in `handleExecute`, emit bookend events, close on completion

### Out of scope

- Migration event logger (`consensus-migrate-events.jsonl`) wiring — story #520
- K8s Event emission — belongs in the `handleExecute` implementation story
- UC event log — separate component, separate story

## Open Questions for HIP Editors

1. **`nodeId` source**: where does the daemon get `nodeId` (e.g. `0.0.3`)? Is it in
   `daemon.yaml`, derived from the orbit namespace, or read from the `NetworkUpgradeExecute`
   CR spec? The current CR spec only has `operationId` and `orbit`.

2. **File I/O failure policy**: the plan above continues the upgrade on write failure.
   Should the HIP mandate halt-on-log-failure for strict auditability?

3. **Startup pruning timing**: pruning the last 10 upgrade files on daemon startup means
   a crash loop could prune files before they are read. Should pruning happen after the
   watch is established, or is startup pruning acceptable?
