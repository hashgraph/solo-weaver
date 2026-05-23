# Plan — Daemon Event Logger (JSONL)

> **Depends on:** First story that implements `handleExecute` steps (InfraConfig placement, ConsensusConfig CR, status patch)
> **HIP channels:** Upgrade event log + Migration event log under `daemon/events/consensus/`
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
Events are organised under `daemon/events/<component>/` so future node types
(block node, relay, etc.) get sibling directories without polluting a single flat directory.

## Directory Layout

```
$home/daemon/events/
  consensus/
    upgrade/    <- consensus-upgrade-<ts>Z-<ver>.jsonl  (one per operation; pruned)
    migrate/    <- consensus-migrate-events.jsonl        (append-only; not pruned by age)
  block-node/   <- future
  relay/        <- future
```

Separating upgrade and migrate into their own subdirectories means:
- Each pruner call uses a simple `*.jsonl` glob against its own directory — no cross-contamination risk.
- No need for special "skip if no timestamp" logic when pruning the upgrade directory.
- Adding a new component is a one-line `WeaverPaths` addition.

## File Strategy

### Upgrade Workflow — Per-Operation File

```
/opt/solo/weaver/daemon/events/consensus/upgrade/consensus-upgrade-20260415T143000Z-v0.75.0.jsonl
```

One file per upgrade operation (`consensus-upgrade-<ts>Z-<ver>.jsonl`).
The timestamp uses compact ISO-8601 with a UTC `Z` suffix: `20060102T150405Z`.

**Why per-operation:**
- One file = one incident. Trivial to isolate, grep, share, or attach to a post-mortem.
- Retention is clean and bounded: files older than 365 days are removed, then oldest-first
  until at most 50 remain.
- Corruption blast radius is limited to the current operation's file.

**Note on filename vs operationId:** The `operationId` (e.g. `upgrade-20260415T143000Z-v0.75.0`)
does not carry the `consensus-` prefix — it is a semantic identifier used in CR labels
and event payloads. The filename prefix is a filesystem namespace convention only.

**Opening / closing events (written to this file):**
- `ExecuteWorkflowStarted` — first entry; written on `ReadyForProvisionerDaemon` detection
- `ExecuteWorkflowCompleted` / `ExecuteWorkflowFailed` — terminal entry

Internal UpgradeMonitor watch-loop transitions (backoff, reconnect, auth errors) go to
journald only — they are operational noise, not business milestones.

**Retention policy — two triggers:**
1. **On daemon startup** — `daemon.New()` calls the pruner before starting monitors.
2. **After each `handleExecute` completes** — pruner runs immediately after closing the
   per-operation logger. This covers long-running daemons where startup pruning never
   re-runs. Files are only added during upgrades so this is the natural trigger.

Both triggers apply the same policy: remove files older than 365 days (by filename
timestamp), then remove oldest-first until at most 50 remain.

### Migration Workflow — Fixed Append-Only File

```
/opt/solo/weaver/daemon/events/consensus/migrate/consensus-migrate-events.jsonl
```

**Why fixed file:** Migration soak is a long-running continuous process potentially
spanning days. There is no clean "one operationId = one file" boundary. A single fixed
file fits naturally; the `operationId` field in every entry scopes events to a specific
migration. Not subject to pruning.

### UC Events (reference — not owned by this plan)

```
/opt/solo/weaver/uc/events/upgrade-<ts>Z-<ver>.jsonl
```

UC runs as a sidecar on the consensus node pod — it is always consensus-scoped by
context. No `consensus-` prefix needed. Same per-operation strategy and identical retention policy (365 days / 50-file cap)
applied on UC startup — UC and the daemon share the same hostPath mount and upgrade
cadence, so a unified policy avoids divergence.

### Summary Table

| Component | Workflow  | File strategy      | Path                                                                              | Retention                                          |
|-----------|-----------|--------------------|-----------------------------------------------------------------------------------|----------------------------------------------------|
| Daemon    | Upgrade   | Per-operation      | `daemon/events/consensus/upgrade/consensus-upgrade-<ts>Z-<ver>.jsonl`             | ≤365 days & ≤50 files; on startup + post-execute   |
| Daemon    | Migration | Fixed append-only  | `daemon/events/consensus/migrate/consensus-migrate-events.jsonl`                  | None                                               |
| UC        | Upgrade   | Per-operation      | `uc/events/upgrade-<ts>Z-<ver>.jsonl`                                             | ≤365 days & ≤50 files on startup                  |

## HIP-Defined Events

All events share the base fields: `ts`, `level`, `reason`, `msg`, `operationId`, `nodeId`.

| Reason                     | Level   | File                                    | When emitted |
|----------------------------|---------|-----------------------------------------|--------------|
| `ExecuteWorkflowStarted`   | INFO    | `consensus/upgrade/consensus-upgrade-*.jsonl` | `handleExecute` begins |
| `FilesPlaced`              | INFO    | `consensus/upgrade/consensus-upgrade-*.jsonl` | All assets written to host filesystem |
| `ExecuteWorkflowCompleted` | INFO    | `consensus/upgrade/consensus-upgrade-*.jsonl` | Workflow finished; `PendingNodeUpgrade` signaled |
| `ExecuteWorkflowFailed`    | ERROR   | `consensus/upgrade/consensus-upgrade-*.jsonl` | Workflow halted; manual intervention required |

Example file `consensus/upgrade/consensus-upgrade-20260415T143000Z-v0.75.0.jsonl`:

```jsonl
{"ts":"2026-04-15T14:30:00Z","level":"INFO","reason":"ExecuteWorkflowStarted","msg":"Execute workflow triggered by ReadyForProvisionerDaemon; beginning upgrade steps","operationId":"upgrade-20260415T143000Z-v0.75.0","nodeId":"0.0.3"}
{"ts":"2026-04-15T14:30:05Z","level":"INFO","reason":"FilesPlaced","msg":"InfraConfig and infrastructure-versions.yaml placed on host filesystem","operationId":"upgrade-20260415T143000Z-v0.75.0","nodeId":"0.0.3"}
{"ts":"2026-04-15T14:31:02Z","level":"INFO","reason":"ExecuteWorkflowCompleted","msg":"Execute workflow finished; PendingNodeUpgrade signaled","operationId":"upgrade-20260415T143000Z-v0.75.0","nodeId":"0.0.3"}
```

## Package Design

### Packages

- `pkg/eventlog/` — JSONL writer; no K8s or pruning dependencies; placed under `pkg/` so it is importable by daemon, UC, and any future component outside `internal/daemon`
- `pkg/filepruner/` — strategy-based file pruner; usable by daemon, UC, and any future component

### eventlog API

```go
// NewOperation creates a per-operation JSONL file in dir named
// "consensus-<operationID>.jsonl". Truncates on open. Caller must Close when done.
// operationID must be a plain identifier with no path separators.
func NewOperation(dir, operationID string) (*EventLogger, error)

// NewAppend opens (or creates) a fixed append-only file dir/fileName.
// fileName must be a plain filename with no path separators.
func NewAppend(dir, fileName string) (*EventLogger, error)

// Log validates all fields, appends one JSON line, and fsyncs.
func (l *EventLogger) Log(e Event) error

// Path returns the absolute path of the underlying file.
func (l *EventLogger) Path() string

// Close flushes and closes the underlying file.
func (l *EventLogger) Close() error
```

### filepruner API

```go
// Strategy decides whether a file is a pruning candidate.
// If ShouldPrune returns an error the file is treated as protected and never
// deleted — neither by the strategy pass nor by cap enforcement.
type Strategy interface {
    ShouldPrune(path string) (bool, error)
}

// Built-in strategies
FilenameTimestampStrategy{Layout string, MaxAge time.Duration}  // timestamp in filename; Layout must be non-empty
ModTimeStrategy{MaxAge time.Duration}                           // file ModTime
FileSizeStrategy{MaxBytes int64}                                // file size

// Composite strategies
All(strategies ...Strategy) Strategy  // prune if ALL match (AND)
Any(strategies ...Strategy) Strategy  // prune if ANY match (OR)

// Pruner
func New(strategy Strategy) *Pruner
func (p *Pruner) Prune(dir, glob string, keep int) error
```

### Error handling policy

If `Log()` returns an error (disk full, permissions lost):
- **Do not silently drop** — the file is the crash-safe audit trail
- **Log a warn to journald** with `reason=EventLogWriteFailed` and the underlying error
- **Continue the upgrade** — a missing audit trail is bad; a halted upgrade is worse

If `Prune()` returns an error (delete failed, permissions):
- **Log a warn to journald** with `reason=UpgradeEventLogPruneFailed`
- **Continue** — a stale extra file is less harmful than a blocked daemon or failed upgrade

All errors use `errorx` typed errors (`ErrInvalidEvent`, `ErrPruneFailed`, `ErrNoTimestamp`).

### File paths on WeaverPaths

```go
DaemonEventsDir                  string  // $home/daemon/events
DaemonConsensusEventsDir         string  // $home/daemon/events/consensus
DaemonConsensusUpgradeEventsDir  string  // $home/daemon/events/consensus/upgrade
DaemonConsensusMigrateEventsDir  string  // $home/daemon/events/consensus/migrate
DaemonConsensusMigrateEventsPath string  // $home/daemon/events/consensus/migrate/consensus-migrate-events.jsonl
```

All four directories are added to `AllDirectories` so daemon startup creates them automatically.

### Wire-up

**On daemon startup** (`daemon.New()`):
- Validate `DaemonConsensusUpgradeEventsDir` is within the weaver home tree via `sanity.ValidatePathWithinBase`
- Call `filepruner` with `FilenameTimestampStrategy{Layout: "20060102T150405Z", MaxAge: 365d}` and `keep=50` against `DaemonConsensusUpgradeEventsDir`.

**In `handleExecute`** (subsequent story):
- Open a per-operation logger via `eventlog.NewOperation(paths.DaemonConsensusUpgradeEventsDir, operationID)`
- Emit opening/closing events via `logEvent` helper (nil-safe wrapper)
- Close logger on completion
- Run pruner again post-close — covers long-running daemons where startup pruning never re-runs

**Migration logger** (`daemon.New()`, story #520):
- Open once via `eventlog.NewAppend(paths.DaemonConsensusMigrateEventsDir, "consensus-migrate-events.jsonl")`
- Inject into `MigrationMonitor`

Nil-safe `logEvent` helper on `UpgradeMonitor`:

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

- `pkg/eventlog/event.go` — `Event`, `Level` constants, field validation
- `pkg/eventlog/errors.go` — `ErrInvalidEvent` errorx type
- `pkg/eventlog/logger.go` — `EventLogger`, `NewOperation`, `NewAppend`, `Log`, `Close`, `Path`
- `pkg/eventlog/logger_test.go` — unit tests (write, truncate, append, concurrent writes, validation, Path absoluteness, unsafe path rejection)
- `pkg/filepruner/pruner.go` — `Strategy`, `Pruner`, `FilenameTimestampStrategy`, `ModTimeStrategy`, `FileSizeStrategy`, `All`, `Any`
- `pkg/filepruner/errors.go` — `ErrPruneFailed`, `ErrNoTimestamp` errorx types
- `pkg/filepruner/pruner_test.go` — unit tests for all strategies, composites, cap enforcement, protected-file exclusion, empty Layout guard
- `pkg/models/weaver_paths.go` — add `DaemonConsensusEventsDir`, `DaemonConsensusUpgradeEventsDir`, `DaemonConsensusMigrateEventsDir`, `DaemonConsensusMigrateEventsPath`
- `internal/daemon/daemon.go` — call pruner on startup against `DaemonConsensusUpgradeEventsDir`

### Out of scope

- `handleExecute` event emission and post-execute pruning — story that implements `handleExecute` steps
- Migration event logger (`consensus-migrate-events.jsonl`) wiring — story #520
- K8s Event emission — belongs in the `handleExecute` implementation story
- UC event log — separate component, separate story

## Open Questions for HIP Editors

1. **`nodeId` source**: where does the daemon get `nodeId` (e.g. `0.0.3`)? Is it in
   `daemon.yaml`, derived from the orbit namespace, or read from the `NetworkUpgradeExecute`
   CR spec? The current CR spec only has `operationId` and `orbit`.

2. **File I/O failure policy**: the plan above continues the upgrade on write failure.
   Should the HIP mandate halt-on-log-failure for strict auditability?

3. **Startup pruning timing**: a crash loop could prune files before they are read.
   Should pruning happen after the watch is established, or is startup pruning acceptable?
   Note: post-execute pruning (trigger 2) is unaffected by crash loops.
