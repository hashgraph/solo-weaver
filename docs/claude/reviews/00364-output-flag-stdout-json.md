# Review Guide — 00364: `-o/--output` controls the stdout log format

## Problem & Solution

[Issue #364](https://github.com/hashgraph/solo-weaver/issues/364): `-o json` had
no effect — workflow commands ignored the flag entirely, and the operator wanted
JSON on stdout for automation (Ansible / `jq` / CI).

**Solution:** `-o/--output` now selects the **stdout format**, `{text, json}`,
default `text`.
- `text` (default) — human output: the interactive TUI on a terminal, or plain
  console log lines when piped / `--non-interactive` (unchanged behavior).
- `json` — forces non-interactive mode and emits one JSON object per log event
  (NDJSON) on stdout, followed by a final tagged summary object
  `{"type":"summary","status":…,"report_path":…,"report":{…}}`.

This matches common tooling (`kubectl -o json`, `terraform -json`): requesting a
machine format disables the interactive UI. Human error panels already go to
**stderr** (`internal/doctor/diagnose.go`), so the JSON stdout stream stays clean.

**Design note (why not the report file):** an earlier iteration made `-o` switch
the report *file* between `.json`/`.yaml`. Per engineer review, the real need is
stdout, so `-o` is scoped to stdout only. The report file stays YAML
(`setup_report_<ts>.yaml`) in both modes; its path is surfaced in the
`report_path=…` log field and in the JSON summary object.

**Mechanism:** no `logx` change needed. `logx` wraps zerolog, whose native writer
already emits compact JSON — the `ConsoleWriter` is just the pretty-printer. The
repo already swaps custom loggers via `logx.SetLogger` (see
`SuppressConsoleLogging`). The new `SetJSONConsoleLogging` installs a zerolog
logger writing JSON to stdout+file (no `ConsoleWriter`).

## Changed Files

| File | Change |
|------|--------|
| `cmd/cli/commands/common/flags_common.go` | `--output` → `(text, json)`, default `text` |
| `cmd/cli/commands/common/run.go` | `resolveOutputFormat` (text/json), `OutputIsJSON()`; `finalizeWorkflowReport` emits the tagged JSON summary (and suppresses the human table) in json mode; report file back to `.yaml` |
| `cmd/cli/commands/root.go` | Three-way logging init: json → force non-interactive + `SetJSONConsoleLogging`; else existing raw/TUI paths |
| `internal/ui/logging.go` | `newJSONConsoleLogger` + `SetJSONConsoleLogging` (JSON to stdout+file via `logx.SetLogger`) |
| `cmd/cli/commands/version.go` | `renderVersion` → text (default) / json; `yaml` removed |
| `internal/workflows/steps/report.go` | Revert to YAML-only; keep the error-return hardening |
| `*_test.go` | `resolveOutputFormat`/`OutputIsJSON`, JSON console logger, version text-default, report YAML tests; IT call sites reverted to 2-arg |
| `docs/quickstart.md` | `--output` semantics, version examples, cheat-sheet |

## Review Checklist

- `-o json` forces `ui.NonInteractive = true` so the TUI never owns stdout; NDJSON is emitted via a `logx.SetLogger` swap (no `ConsoleWriter`).
- The JSON summary is tagged `type=summary`; consumers select by tag (a trailing handler log line may follow it — do not assume strict last-line).
- In json mode the human summary table is suppressed (would corrupt NDJSON); text mode ordering is unchanged.
- Error panels stay on stderr; stdout is pure NDJSON in json mode.
- Default is `text`; only an explicit `json` switches (empty/yaml/unknown → text). `version` default flips JSON→text; `-o yaml` removed.
- No `logx`/vendor bump.

## Tests

```bash
task lint                       # 0 issues
task vm:test:unit               # full unit suite (Linux-only deps; runs under sudo)
go test -race -tags='!integration' \
  ./cmd/cli/commands/... ./cmd/cli/commands/common/... \
  ./internal/ui/... ./internal/workflows/steps/...
```

## Manual UAT (in the VM, provisioner installed)

```bash
BIN=/opt/solo/weaver/bin/solo-provisioner

$BIN version            # human text
$BIN version -o json    # {"version":…}
$BIN version -o yaml    # error: unsupported format "yaml" (want text or json)

# JSON workflow output: every stdout line is a JSON object; summary is tagged
sudo $BIN block node check -p local -o json | jq -c 'select(.type=="summary") | {status, report_path}'
# -> {"status":"success","report_path":"/opt/solo/weaver/logs/setup_report_<ts>.yaml"}

# Default text output unchanged (TUI on a TTY, console lines when piped)
sudo $BIN block node check -p local
```

## Risks

- Behavior change: `version` default output flips JSON→text; `-o yaml` removed (CLI surface reduction).
- `-o json` forcing non-interactive changes stdout for TTY users who pass it — intended, matches common tooling.
- No embedded-template / install-time-config changes; no upgrade/migration path touched.
- `-V` (verbose) + `-o json` is a contradictory combo; the verbose diagnostics path is unaffected by this change.
