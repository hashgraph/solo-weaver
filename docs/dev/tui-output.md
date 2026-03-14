# TUI Output Layer

Solo Provisioner uses a rich terminal UI (TUI) built with
[Bubble Tea](https://github.com/charmbracelet/bubbletea) to display workflow
progress in a human-friendly way. The previous output — raw zerolog structured
log lines interleaved with ANSI error dumps — is replaced by a clean
spinner/status-icon flow inspired by `docker`, `terraform`, and `gh`.

---

## Architecture

```
┌──────────────────────────────────────────────────────┐
│  cmd/weaver/commands/common/run.go  (RunWorkflow)    │
│  ┌────────────┐          ┌──────────────────┐        │
│  │ TTY?       │──yes───▶ │ runWithTUI()     │        │
│  │            │          │ tea.NewProgram() │        │
│  └────────────┘          └──────────────────┘        │
│        │ no                       │                   │
│        ▼                          ▼                   │
│  ┌──────────────────┐   ┌──────────────────────────┐ │
│  │ runWithFallback() │   │ notify.SetDefault(       │ │
│  │ line-based output │   │   ui.NewTUIHandler(pgm)) │ │
│  └──────────────────┘   └──────────────────────────┘ │
│                                   │                   │
│                        ┌──────────▼──────────┐       │
│                        │  workflow.Execute()  │       │
│                        │  (background gortn)  │       │
│                        └──────────┬──────────┘       │
│                                   │                   │
│                        ┌──────────▼──────────┐       │
│                        │  WorkflowDoneMsg    │       │
│                        │  → tea.Quit         │       │
│                        └──────────┬──────────┘       │
│                                   │                   │
│                        ┌──────────▼──────────┐       │
│                        │ handleWorkflowResult│       │
│                        │ summary + report     │       │
│                        └─────────────────────┘       │
└──────────────────────────────────────────────────────┘
```

### Key packages

| Package | Purpose |
|---------|---------|
| `internal/ui` | Bubble Tea model, messages, TUI & fallback handlers, logging hooks |
| `internal/workflows/notify` | Callback interface (`StepStart`, `StepCompletion`, `StepFailure`, `StepDetail`, `PhaseStart`, `PhaseCompletion`, `PhaseFailure`) |
| `cmd/weaver/commands/common` | `RunWorkflow` orchestration — detects TTY and wires the appropriate handler |
| `internal/doctor` | Error diagnostics — compact by default, full stacktrace with `-VV` |

### Integration model

The TUI layer works by calling `notify.SetDefault(handler)` at startup to replace
the default logging-based handler with one that sends Bubble Tea messages (TUI mode)
or writes formatted lines (fallback mode). Workflow steps use the existing
`notify.As().StepStart(...)`, `notify.As().StepCompletion(...)`, etc. callbacks
without any TUI-specific knowledge.

---

## Bubble Tea model (`internal/ui/model.go` + `view.go`)

The TUI is built on the [Elm Architecture](https://guide.elm-lang.org/architecture/)
via Bubble Tea: **Model** holds state, **Update** processes messages, **View** renders.

### Data structures

```
Model
├── phases []phaseEntry         ← ordered list of workflow phases
│   ├── id, name, status        ← phase identity and current status
│   ├── duration, started       ← timing for progress bar and duration display
│   ├── steps []stepEntry       ← ordered list of steps within this phase
│   │   ├── id, name, status    ← step identity and current status
│   │   ├── duration, started   ← per-step timing
│   │   ├── detail              ← transient grey text (e.g. "installing kubelet...")
│   │   └── errMsg              ← error message on failure
│   └── completedSteps          ← count of finished steps
├── spinner                     ← Bubble Tea spinner component (animated dot)
├── report                      ← automa.Report captured on WorkflowDoneMsg
├── weaving                     ← latest background detail message
├── done                        ← true after WorkflowDoneMsg
└── quitting                    ← true on ctrl+c or workflow completion
```

**`entryStatus`** enum: `statusRunning`, `statusSuccess`, `statusFailed`, `statusSkipped`, `statusPending`.

### Message flow

Workflow steps emit `notify` callbacks → the TUI handler (`NewTUIHandler`) converts
them to Bubble Tea messages → `Update()` modifies the model state → `View()` re-renders.

| notify callback | Bubble Tea message | Model change |
|----------------|--------------------|--------------|
| `PhaseStart` | `PhaseStartedMsg` | Appends a new `phaseEntry` with `statusRunning` |
| `PhaseCompletion` | `PhaseDoneMsg` | Sets phase status to `statusSuccess`/`statusSkipped`, records duration |
| `PhaseFailure` | `PhaseFailedMsg` | Sets phase status to `statusFailed`, records error |
| `StepStart` | `StepStartedMsg` | Appends/updates `stepEntry` in current phase with `statusRunning` |
| `StepCompletion` | `StepDoneMsg` | Sets step status, records duration, clears detail |
| `StepFailure` | `StepFailedMsg` | Sets step status to `statusFailed`, records error |
| `StepDetail` | `StepDetailMsg` | Sets `detail` on the currently running step |
| (workflow ends) | `WorkflowDoneMsg` | Sets `done=true`, captures `report`, triggers `tea.Quit` |

### Rendering pipeline (`view.go`)

`View()` iterates over `m.phases` and dispatches to one of two renderers based on
verbosity:

- **`renderPhaseCompact(ph)`** — used at level 0 when the phase has a name. Shows a
  single line per phase: completed phases show `✓ PhaseName (duration)`, running
  phases show a spinner + progress bar on two lines.

- **`renderPhaseExpanded(ph)`** — used at level 1+ or for unnamed phases. Shows the
  phase header, then all child steps with status icons. At level 2+, running steps
  also show their `detail` text.

Both renderers use the same style constants (`successIcon`, `failedIcon`, etc.) and
`formatDuration()` for consistent output.

The progress bar (`renderTimeProgressBar`) uses `bubbles/progress.ViewAs(ratio)` for
static gradient rendering. The ratio is `elapsed / PhaseBenchmarks[phaseID]`.

The summary table (`RenderSummaryTable`) is rendered **after** the TUI quits, not
inside `View()`, because it needs the full `automa.Report` which includes steps that
may not have emitted notify callbacks.

### Fallback handler (`handler.go`)

When stdout is not a TTY, the fallback handler replaces the TUI. It uses the same
notify callbacks but writes directly to a file descriptor instead of sending Bubble
Tea messages. Key differences:

- **Level 0:** Uses `writeInline()` for overwritable multi-line content (phase name +
  progress bar). A 1-second ticker refreshes the progress bar.
- **Level 1:** `StepStart` writes an inline (transient) line, replaced by
  `StepCompletion` which writes a permanent line with duration.
- **Level 2:** `StepStart` writes a permanent line. Detail lines are written as
  permanent indented grey text below.

State tracked via closure variables: `curPhaseName` (for indentation decisions),
`startTimes` map (for duration calculation), `curStepName`/`curDetail` (for compact
inline rendering).

---

## Verbosity levels

| Level | Flag | Behaviour |
|-------|------|-----------|
| 0 | (none) | Collapsed phases with gradient progress bar + current step name |
| 1 | `-V` | Completion lines only (✓/✗/⊘) with durations, indented under phase headers |
| 2 | `-VV` | All steps (start + completion) + greyed detail lines + software version info + full error stacktraces |
| 3 | `-VVV` | Unformatted output: no formatting, zerolog writes directly to the console (replaces `--raw`) |

### Level 0 — Compact (default)

Phases are collapsed into a single line with a gradient progress bar. The bar
uses the `bubbles/progress` component with a scaled gradient (`#6C6CFF` → `#22D3EE`).
Phase name appears on the first line, progress bar + time estimate + current step
on the second:

```
  ✓ Preflight Checks  (1.2s)
  ✓ System Setup  (5.2s)
  • Kubernetes Setup
    ████████████████████░░░░░░░░░░░░░░░░░░░░  ~2m47s  Initializing Kubernetes cluster
```

### Level 1 — Completions only (`-V`)

Phase headers appear with `•` (blue dot) before their children. Steps are
indented and show only the completion line with duration. A version header is
printed at the top:

```
  solo-provisioner 0.29.0 (f5806409) go1.25.2

  • Preflight Checks
    ✓ Validating privileges
    ✓ Validating service account
    ✓ Validating host profile
  ✓ Preflight Checks

  • Kubernetes Setup
    ✓ kubelet setup successfully (2.3s)
    ✓ kubectl setup successfully (0.8s)
    ✓ Helm setup successfully (1.1s)
  ✓ Kubernetes Setup (2m20s)
```

At this level, `StepStart` is rendered as an inline (transient) line that shows
`• Setting up kubelet` briefly, then gets replaced by the `✓` completion line.

### Level 2 — Full detail (`-VV`)

Both start and completion lines are shown for each step. Detail lines (from
`StepDetail` callbacks and the zerolog log hook) appear indented below the
running step, making it clear which step they belong to. Software version info
is displayed:

```
  • Kubernetes Setup
    • Setting up kubelet
        installing kubelet...
        resolved software version
        kubelet version: 1.33.4
    ✓ kubelet setup successfully (2.3s)
    • Setting up Helm
        installing Helm...
        Helm version: 3.18.6
    ✓ Helm setup successfully (1.1s)
  ✓ Kubernetes Setup (2m20s)
```

### Error output

**Default (compact, levels 0–1):**
```
  ✗ Error: requires superuser privilege
    Cause: current user is not root

  Resolution:
    1. Run the command with 'sudo': `sudo solo-provisioner block node install`

  See logs: /opt/solo/weaver/logs/solo-provisioner.log
  Use -VV for full diagnostics
```

**With `-VV`:**
Full error stacktrace, error diagnostics panel, profiling data, and resolution.

---

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--verbose` | `-V` | `0` | Increase output verbosity (`-V`, `-VV`, `-VVV` for raw) |

Persistent flag on the root command, applies to all subcommands. Non-TTY
environments (pipes, CI) automatically use the fallback handler. TTY detection
(`ShouldUseTUI()`) selects between the Bubble Tea TUI and the line-based fallback.

---

## Logging

### Always-on file logging

Regardless of the `log.fileLogging` config setting, solo-provisioner **always**
writes structured logs to a file. This ensures that full debug/trace output is
available for post-mortem analysis even when the TUI suppresses console logging.
Every log entry includes the CLI `version` field.

Defaults (applied when not overridden by config):
- **Directory:** `/opt/solo/weaver/logs`
- **Filename:** `solo-provisioner.log`
- **MaxSize:** 50 MB
- **MaxBackups:** 3
- **MaxAge:** 30 days

### Console logging suppression

The output is **mutually exclusive**: either the TUI/fallback handler owns stdout,
or zerolog does — **never both**. File logging is always active.

When the TUI or fallback handler is active, `ui.SuppressConsoleLogging()` replaces
the global zerolog logger with a **file-only** writer. A `logHook` is attached
that forwards log messages to the output handler as greyed detail text.

> **Why SuppressConsoleLogging?** The upstream `logx.Initialize()` unconditionally
> creates a `ConsoleWriter` regardless of the `ConsoleLogging` config field.
> `SuppressConsoleLogging()` calls `*logx.As() = ...` to replace the logger
> in-place after `Initialize()` returns.

### How `logx` messages flow to the terminal

When a workflow step calls `logx.As().Info().Msgf("kubelet version: %s", v)`, the
message takes two paths:

```
logx.As().Info().Msgf("kubelet version: 1.33.4")
  │
  ├──▶ Log file (always)
  │     Written as structured JSON with all Str/Int fields:
  │     {"level":"info","software":"kubelet","version":"1.33.4",
  │      "message":"kubelet version: 1.33.4","time":"..."}
  │
  └──▶ logHook.Run() (attached by SuppressConsoleLogging)
        │
        ├── Level filter:
        │   • Info, Warn     → always forwarded
        │   • Debug          → forwarded only at VerboseLevel >= 2 (-VV)
        │   • Trace and below → never forwarded
        │   • Error and above → never forwarded (handled by notify.StepFailure)
        │
        ├── Throttle: skip if last send was < 80ms ago
        │
        ├── Sanitize: collapse whitespace, truncate at 200 chars
        │
        └──▶ Output handler
             • TUI mode:     program.Send(StepDetailMsg{Detail: "kubelet version: 1.33.4"})
             • Fallback mode: writes greyed indented line to stdout
```

**Key implication for step authors:** The `Msg`/`Msgf` text of any `Info`-level log
call will appear as greyed detail text at `-VV`. To show useful information at `-VV`,
make the message human-friendly. Structured fields (`Str`, `Int`, etc.) only go to
the log file — they are not visible on the terminal.

Detail text is only displayed when inside an active phase (`curPhaseName != ""`).
Log messages emitted before any phase starts (e.g. during global checks) are
silently dropped from the terminal but still written to the log file.

### Examples of log messages designed for `-VV` display

Software version (shown as detail under the running step):
```go
logx.As().Info().
    Str("software", installer.GetSoftwareName()).
    Str("version", installer.Version()).
    Msgf("%s version: %s", installer.GetSoftwareName(), installer.Version())
```

Hardware validation (shown as detail under preflight checks):
```go
logx.As().Info().Msgf("detected: %d cores, required: %d cores", hostProfile.GetCPUCores(), reqs.MinCpuCores)
```

---

## Progress bar

The progress bar uses the `bubbles/progress` component (`charmbracelet/bubbles v1.0.0`)
with `ViewAs(ratio)` for static rendering — no animation machinery is needed.

Configuration:
- **Width:** 40 characters
- **Gradient:** Scaled gradient from `#6C6CFF` (indigo) to `#22D3EE` (cyan)
- **Color profile:** Forced to `TrueColor` (avoids detection issues when stdout is captured)
- **No percentage:** Time estimate is shown instead (`~2m47s`)

Benchmark durations for each phase are stored in `internal/ui/benchmark.go`. The
progress bar ratio is `elapsed / expected`, capped at 1.0. When no benchmark exists
for a phase, only the elapsed time is shown.

---

## stdout capture

`internal/ui/capture_unix.go` redirects file descriptors 1 (stdout) and 2 (stderr)
to discard pipes at the OS level using `dup2`. This catches **all** output — even
from third-party C/Go libraries that write directly to fd 1/2 (e.g. Helm OCI
"Pulled:" messages, syspkg "apt manager"). The original stdout fd is preserved for
the output handler to write to.

---

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/charmbracelet/bubbletea` | v1.3.10 | TUI framework / event loop |
| `github.com/charmbracelet/bubbles` | v1.0.0 | Spinner + progress bar components |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | Terminal styling / colors |
| `github.com/charmbracelet/harmonica` | v0.2.0 | Spring physics (transitive, for progress) |

---

## File inventory

| File | Description |
|------|-------------|
| `internal/ui/config.go` | Package-level `VerboseLevel` flag variable and `IsUnformatted()` helper |
| `internal/ui/messages.go` | Bubble Tea message types (`StepStartedMsg`, `StepDoneMsg`, `StepFailedMsg`, `StepDetailMsg`, `PhaseStartedMsg`, `PhaseDoneMsg`, `PhaseFailedMsg`, `WorkflowDoneMsg`) |
| `internal/ui/model.go` | Bubble Tea `Model` — data structures (`phaseEntry`, `stepEntry`), `Init()`, `Update()` message handling |
| `internal/ui/view.go` | `View()` rendering, `renderPhaseCompact`, `renderPhaseExpanded`, progress bar, version header, summary table, styles |
| `internal/ui/handler.go` | `NewTUIHandler()` and `NewFallbackHandler()` — bridges `notify.Handler` into the TUI or line-based output |
| `internal/ui/logging.go` | `logHook` (unified zerolog hook), `SuppressConsoleLogging()`, `SuppressConsoleLoggingForFallback()`, `sanitizeDetail()` |
| `internal/ui/capture_unix.go` | OS-level stdout/stderr capture via `dup2` |
| `internal/ui/benchmark.go` | Phase benchmark durations for progress bar estimates |

---

## Design: exclusive output contract

- **TUI mode** (TTY detected): Bubble Tea owns stdout. Console logging is
  disabled. All `logx.As()` calls go to the log file only. The TUI renders step
  progress via the `notify` handler. After the TUI quits, `handleWorkflowResult()`
  prints the summary table.

- **Fallback mode** (non-TTY): The fallback handler prints line-based step events
  to stdout. Console logging is suppressed. After the workflow,
  `handleWorkflowResult()` prints the summary.

- **Unformatted mode** (`-VVV`): No formatting. Zerolog writes directly to the console.
  The default notify handler logs via logx.

**Never both.** File logging is always active regardless of output mode.

### Why the TUI `View()` does NOT render a summary

The TUI model tracks only steps that emit `notify` callbacks. The definitive
summary uses `countStatuses()` which recurses through the full automa `Report` —
this always reflects the true step count. Having a single summary source avoids
count mismatches and duplicate summary blocks.
