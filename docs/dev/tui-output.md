# TUI Output Layer

Solo Provisioner uses a rich terminal UI (TUI) built with
[Bubble Tea](https://github.com/charmbracelet/bubbletea) to display workflow
progress in a human-friendly way.  The previous output – raw zerolog structured
log lines interleaved with ANSI error dumps – is replaced by a clean
spinner → status-icon flow inspired by `docker`, `terraform`, and `gh`.

---

## Architecture

```
┌──────────────────────────────────────────────────────┐
│  cmd/weaver/commands/common/run.go  (RunWorkflow)    │
│  ┌────────────┐          ┌──────────────────┐        │
│  │ TTY?       │──yes───▶ │ runWithTUI()     │        │
│  │ --no-tui?  │          │ tea.NewProgram() │        │
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
| `internal/ui` | Bubble Tea model, messages, TUI & fallback handlers |
| `internal/workflows/notify` | Existing callback interface (`StepStart`, `StepCompletion`, `StepFailure`) – **no changes** |
| `cmd/weaver/commands/common` | `RunWorkflow` orchestration – detects TTY and wires the appropriate handler |
| `internal/doctor` | Error diagnostics – compact by default, verbose with `--verbose` |

### Zero-change integration

All ~50 workflow step files already call `notify.As().StepStart(...)`,
`notify.As().StepCompletion(...)`, and `notify.As().StepFailure(...)`.  The TUI
layer works by calling `notify.SetDefault(handler)` at startup to replace the
default logging-based handler with one that sends Bubble Tea messages.  **No
step files need to be modified.**

---

## Output modes

### 1. TUI mode (default on TTY)

When stdout is a terminal and `--no-tui` is not set, the Bubble Tea program
renders live spinner animations and status icons:

```
  ⣾ Validating privileges
  ✓ Validating weaver user (0.2s)
  ✓ Setting up home directory structure (0.1s)
  ⣾ Installing iptables...

  ─────────────────────────────────────────────────
  Summary: 15 passed, 2 skipped, 0 failed
  Duration: 2m 34s
  Report: /opt/solo/weaver/logs/setup_report_20260312_143022.yaml
  ─────────────────────────────────────────────────
```

Symbols:
- `✓` (green) – step succeeded
- `✗` (red) – step failed
- `⊘` (yellow) – step skipped
- spinner (cyan) – step in progress

### 2. Fallback mode (non-TTY / `--no-tui`)

In CI, piped output, or when `--no-tui` is passed, a simple line-based handler
prints step events without spinners:

```
  • Validating privileges
  ✓ Privilege validation step completed successfully (0.1s)
  • Setting up home directory structure
  ✓ Home directory structure setup successfully (0.1s)
```

### 3. Error output

**Default (compact):**
```
  ✗ Error: requires superuser privilege
    Cause: current user is not root

  Resolution:
    1. Run the command with 'sudo': `sudo solo-provisioner block node install`

  See logs: /opt/solo/weaver/logs/solo-provisioner.log
  Use --verbose for full error details
```

**With `--verbose` / `-V`:**
```
************************************** Error Stacktrace ****…
  (full stacktrace)
************************************** Error Diagnostics ****…
  Error: requires superuser privilege
  Cause: current user is not root
  Error Type: common.illegal_state
  Error Code: 10500
  Commit: abc1234
  Pid: 12345
  ...
****************************************** Resolution ****…
  Run the command with 'sudo': `sudo solo-provisioner block node install`
```

---

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--verbose` | `-V` | `false` | Show full error stacktraces and profiling on the terminal |
| `--no-tui` | — | `false` | Force line-based output even on a TTY (hidden flag) |

Both are persistent flags on the root command and apply to all subcommands.

`--no-tui` is hidden to discourage casual use.  Non-TTY environments (pipes, CI)
automatically use the fallback handler without needing this flag.

---

## Logging

### Always-on file logging

Regardless of the `log.fileLogging` config setting, solo-provisioner **always**
writes structured logs to a file.  This ensures that full debug/trace output is
available for post-mortem analysis even when the TUI suppresses console logging.

Defaults (applied when not overridden by config):
- **Directory:** `/opt/solo/weaver/logs`
- **Filename:** `solo-provisioner.log`
- **MaxSize:** 50 MB
- **MaxBackups:** 3
- **MaxAge:** 30 days
- **Compress:** true (if config sets it)

### Console logging suppression

The output is **mutually exclusive**: either the Bubble Tea TUI owns stdout, or
the classic zerolog console logger does — **never both**.  File logging is always
active regardless of which output mode is in use.

When the TUI is active, `logConfig.ConsoleLogging` is set to `false` and, after
`logx.Initialize()` returns, `ui.SuppressConsoleLogging()` replaces the global
zerolog logger with a **file-only** writer so that raw structured log lines
cannot interleave with the Bubble Tea render loop.  The TUI becomes the sole
owner of stdout.

When the TUI is _not_ active (non-TTY or `--no-tui`), console logging follows
the config setting as before.

> **Why SuppressConsoleLogging?** The upstream `logx.Initialize()` (v0.1.0)
> ignores the `ConsoleLogging` field and unconditionally creates a
> `ConsoleWriter`.  We cannot patch the vendored file because `task build` runs
> `go mod vendor` which overwrites local edits.  Instead,
> `SuppressConsoleLogging()` calls `*logx.As() = ...` to replace the logger
> in-place with a file-only writer after `Initialize()` returns.  A PR should
> be raised upstream so this workaround can be dropped in a future version.

Additionally, the redundant `logx.Initialize()` call in
`internal/config/init.go` was removed.  That early `init()` re-initialized
logging with `ConsoleLogging: true` at package-load time — before cobra could
parse flags — creating a window where console output leaked before the TUI
could suppress it.  The logx package's own `init()` already provides a working
default console logger; the real configuration happens in `root.go`'s
`initConfig()` after flags are parsed.

---

## Dependencies added

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/charmbracelet/bubbletea` | v1.3.10 | TUI framework / event loop |
| `github.com/charmbracelet/bubbles` | v1.0.0 | Spinner component |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | Terminal styling / colors |

These bring in transitive deps like `charmbracelet/x`, `muesli/termenv`,
`mattn/go-isatty` (already present), etc.

---

## Files changed / created

### New files

| File | Description |
|------|-------------|
| `internal/ui/config.go` | Package-level `Verbose` and `NoTUI` flag variables |
| `internal/ui/logging.go` | `SuppressConsoleLogging()` – replaces the logx logger with a file-only writer when TUI is active |
| `internal/ui/messages.go` | Bubble Tea message types (`StepStartedMsg`, `StepDoneMsg`, `StepFailedMsg`, `WorkflowDoneMsg`) |
| `internal/ui/model.go` | Bubble Tea `Model` – tracks step state, renders spinner/status view and summary table |
| `internal/ui/handler.go` | `NewTUIHandler()` and `NewFallbackHandler()` – bridges `notify.Handler` into the TUI |
| `docs/dev/tui-output.md` | This documentation |

### Modified files

| File | Changes |
|------|---------|
| `go.mod` / `go.sum` | Added charm dependencies |
| `cmd/weaver/commands/root.go` | Added `--verbose`, `--no-tui` flags; `initConfig` enforces file logging and calls `SuppressConsoleLogging` when TUI is active |
| `cmd/weaver/commands/common/run.go` | `RunWorkflow` now detects TTY and dispatches to `runWithTUI` or `runWithFallback`; compact summary printed after workflow |
| `internal/doctor/diagnose.go` | `CheckErr` uses compact output by default; full verbose output behind `--verbose` flag |
| `internal/config/init.go` | Removed redundant early `logx.Initialize()` call that leaked console output before TUI could suppress it |
| `internal/workflows/steps/step_weaver.go` | Added `WithPrepare`, `WithOnCompletion`, `WithOnFailure` notify callbacks to `InstallWeaver()` and `UninstallWeaver()` so the TUI tracks them |
| `cmd/weaver/commands/self_install.go` | Removed redundant `logx.As().Info()` calls after `RunWorkflow` — success messages are now conveyed via notify callbacks and the summary table |

---

## Design: exclusive output contract

The output is **mutually exclusive**:

- **TUI mode** (TTY detected, `--no-tui` not set): Bubble Tea owns stdout.
  Console logging is disabled (`ConsoleLogging: false`). All `logx.As()` calls
  go to the log file only. The TUI renders step progress via the `notify`
  handler. After the TUI quits, `handleWorkflowResult()` prints the one and
  only summary table.

- **Fallback mode** (non-TTY or `--no-tui`): The fallback handler prints
  line-based step events to stdout. Console logging follows the config setting.
  After the workflow, `handleWorkflowResult()` prints the summary.

**Never both.** File logging is always active regardless of output mode.

### Why the TUI `View()` does NOT render a summary

The TUI model tracks only steps that emit `notify` callbacks. Some steps
(historically `InstallWeaver`, any future step without callbacks) would be
missing from the TUI's count. The definitive summary uses `countStatuses()`
which recurses through the full automa `Report` — this always reflects the
true step count. Having a single summary source avoids count mismatches and
duplicate summary blocks.



