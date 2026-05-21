# PR Review Guide — feat(cmd): split solo-provisioner CLI and daemon binaries

**Issues:** [#514](https://github.com/hashgraph/solo-weaver/issues/514), [#515](https://github.com/hashgraph/solo-weaver/issues/515) (both bundled in this PR)
**Epic:** [#498](https://github.com/hashgraph/solo-weaver/issues/498) — Two-Binary Build Layout
**Branch:** `00514-cli-main-entry-point`
**Base:** `00498-feat-two-binary-build-layout` (epic feature branch — not `main`)
**PR:** [#596](https://github.com/hashgraph/solo-weaver/pull/596)

---

## What Was Done

### Problem

Today `solo-provisioner` is a single Go binary built from `cmd/weaver/`. The `daemon` is a Cobra subcommand inside the CLI tree, so the systemd `solo-provisioner-daemon.service` unit runs the **entire CLI binary** with one argument — bundling every CLI subcommand, the bubbletea TUI, the workflow engine, the migration framework, and all interactive plumbing into a long-running service that uses none of them. This both:

1. Forces the daemon to bounce on every CLI change, defeating the "safe upgrade" property the operator needs.
2. Bloats the daemon's attack surface with code it never invokes.

### Solution

Two-binary build layout per epic #498:

- Rename `cmd/weaver/` → `cmd/cli/` so the CLI entry point sits at a path that matches its role. Binary name (`solo-provisioner`) is unchanged.
- Add `cmd/daemon/main.go` producing a separate `solo-provisioner-daemon` binary with its own minimal Cobra root. No `cmd/cli/...` imports — verified by `go list -deps`.
- Extract per-binary Taskfile target families into `taskfiles/cli.yaml` and `taskfiles/daemon.yaml` (matching the repo's existing `taskfiles/*.yaml` include convention). Rename `build:weaver:*` → `build:cli:*`.

### Outcome

- Daemon binary `solo-provisioner-daemon-linux-amd64` is ~10 MB vs the CLI's ~61 MB. Transitive dep count: 305 vs 1091 — a **3.5× attack-surface reduction** for the long-running process.
- `solo-provisioner --help` no longer lists `daemon`. The daemon runs as `solo-provisioner-daemon`.
- The CLI and daemon share `pkg/config`, `pkg/version`, `pkg/models`, `internal/proxy`, and `internal/doctor` — all packages with no TUI / workflow dependencies.

### Scope boundaries

Sibling stories not in this PR:

- **#516** (wires the daemon into the top-level `task build`/`hash`/`sign` aggregators so a single `task build` produces both binaries) — the per-binary task families + the `build:weaver:* → build:cli:*` rename land in this PR; only the aggregator wiring is #516's job.
- **#517** (CI/CD publishing both binaries as named release artifacts; owns `.releaserc` asset-list and release workflow updates).

The CLI ↔ daemon runtime contract (IPC mechanism, protocol versioning) is a future epic, not in scope here.

---

## Files Changed

### Group 1 — CLI rename (`cmd/weaver/` → `cmd/cli/`)

| Files | Change |
|---|---|
| `cmd/weaver/main.go` → `cmd/cli/main.go` | Entry point moved; import path updated from `cmd/weaver/commands` → `cmd/cli/commands`. |
| `cmd/weaver/commands/` → `cmd/cli/commands/` (~40 Go files) | Entire Cobra subtree moved per-file (git rename detection preserves history). All `github.com/hashgraph/solo-weaver/cmd/weaver/...` import paths inside the subtree rewritten to `…/cmd/cli/...`. |
| `internal/blocknode/migrations.go`, `internal/blocknode/optional_storage.go`, `internal/workflows/migration_legacy_binary.go` | Stale doc-comment paths `cmd/weaver/commands/...` updated to `cmd/cli/commands/...`. No behavior change. |

### Group 2 — Daemon split

| File | Change |
|---|---|
| `cmd/daemon/main.go` *(new)* | Standalone daemon entry. Own Cobra root `Use: "solo-provisioner-daemon"`. Self-contained `initConfig` (~30 lines duplicated from CLI; pinned to `solo-provisioner-daemon.log`, no TUI-aware branches). Signal-handling `RunE`. Imports only `pkg/config`, `pkg/models`, `pkg/version`, `internal/proxy`, `internal/doctor`, `logx`, `uuid`, `cobra`, `errorx`. |
| `cmd/cli/commands/daemon.go` *(deleted)* | The CLI's `daemonCmd` Cobra subcommand and its signal-handling body. Now lives in `cmd/daemon/main.go`. |
| `cmd/cli/commands/root.go` | Removed `rootCmd.AddCommand(daemonCmd)`. Simplified the log-filename block (dropped the dead `os.Args[1] == "daemon"` branch; now always `solo-provisioner.log`). |

### Group 3 — Taskfile restructure

| File | Change |
|---|---|
| `taskfiles/cli.yaml` *(new)* | `build:cli:*`, `hash:cli:*`, `sign:cli:*`, `run:cli`, `run:cli:skipdl`. Renamed from `*:weaver:*`. `run:cli` also fixes a pre-existing stale `bin/weaver-*` path that was always wrong (binary has been `solo-provisioner-*` since forever). |
| `taskfiles/daemon.yaml` *(new)* | `build:daemon:*`, `hash:daemon:*`, `sign:daemon:*`. Same shape as the CLI family. |
| `Taskfile.yaml` | Removed ~100 lines of inline weaver tasks (now in `taskfiles/cli.yaml`). Added `includes` entries for `cli` and `daemon` with `flatten: true`. Top-level `build`/`hash`/`sign` aggregators retargeted from `:weaver:all` to `:cli:all`. Top-level `run` retargeted to `run:cli`. Net: root Taskfile slimmed from ~370 to 223 lines. |

### Group 4 — Workflow + docs

| File | Change |
|---|---|
| `.github/workflows/zxc-uat-test.yaml` | One line: `task build:weaver` → `task build:cli`. The only workflow file that calls the renamed task by its old name; `task build` invocations in other workflows continue to work via the retargeted aggregator. |
| `CLAUDE.md` | "CLI Layer" section heading updated `(cmd/weaver/)` → `(cmd/cli/)`; entry-point reference updated; new "Daemon Layer (`cmd/daemon/`)" subsection added. One `task build:weaver GOOS=...` example updated to `task build:cli GOOS=...`. |
| `docs/dev/acceptance-tests.md` | Two `task build:weaver GOOS=linux GOARCH=arm64` references → `task build:cli GOOS=linux GOARCH=arm64`. |
| `docs/claude/plans/00514-cli-main-entry-point.md` | Plan updated to reflect that Taskfile extraction landed in this PR (was previously deferred to #516). |

---

## Code Review Checklist

### `cmd/daemon/main.go` (the new daemon entry)

- [ ] No `import` line references `github.com/hashgraph/solo-weaver/cmd/cli/...`. Verified by grep below.
- [ ] No `import` line references `bubbletea`, `charmbracelet`, `internal/ui`, or `internal/workflows`. Verified by `go list -deps`.
- [ ] `daemonRootCmd.Use == "solo-provisioner-daemon"` (binary name matches output of `cmd/daemon`).
- [ ] `initConfig` hardcodes `logConfig.Filename = "solo-provisioner-daemon.log"` (no `os.Args` branch — the daemon binary doesn't multiplex on subcommand).
- [ ] `initConfig` always uses raw `logx.Initialize` (no `ui.IsUnformatted()` / `ui.SuppressConsoleLogging()` branch — daemon is non-interactive by definition).
- [ ] `activateProxy(ctx)` is called after logging is initialized so proxy activation logs land in the daemon's log file.
- [ ] `signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)` is the only RunE work (signal-handling loop) — RunE does not do CLI-style workflow execution.

### `cmd/cli/commands/root.go`

- [ ] `rootCmd.AddCommand(daemonCmd)` is **removed** (the `daemonCmd` symbol no longer exists after deleting `daemon.go`).
- [ ] No remaining `os.Args[1] == "daemon"` branch in `initConfig`. The CLI log filename is unconditionally `solo-provisioner.log`.
- [ ] `cmd help` output (or `solo-provisioner --help`) does **not** list `daemon` as a subcommand.

### `cmd/cli/commands/daemon.go`

- [ ] File is deleted (was the source of `daemonCmd`). No other file imports `daemonCmd` from this package after deletion.

### Import-path rewrites under `cmd/cli/commands/`

- [ ] All in-tree imports use `github.com/hashgraph/solo-weaver/cmd/cli/...`. None left at `…/cmd/weaver/…`.
- [ ] License headers (`// SPDX-License-Identifier: Apache-2.0`) are preserved on moved files.
- [ ] Per-file rename detection worked (git log on any moved file should show pre-rename history under `--follow`).

### `cmd/cli/commands/common/run.go:144` — `daemonLogPath`

- [ ] The CLI's summary table (`ui.RenderSummaryTable`) still references `solo-provisioner-daemon.log` as the operator-facing daemon log path. This is correct: the CLI tells the operator where the daemon writes, even though the CLI itself doesn't write there.

### `Taskfile.yaml` + `taskfiles/cli.yaml` + `taskfiles/daemon.yaml`

- [ ] Root Taskfile `build`/`hash`/`sign` aggregators call `:cli:all` (not `:weaver:all`).
- [ ] Root Taskfile `run` task calls `run:cli` (not `run:weaver`).
- [ ] No `build:weaver:*` / `hash:weaver:*` / `sign:weaver:*` / `run:weaver*` tasks remain in any of the three files.
- [ ] `taskfiles/cli.yaml` `run:cli` and `run:cli:skipdl` use `bin/solo-provisioner-{{OS}}-{{ARCH}}` (not the pre-existing stale `bin/weaver-{{OS}}-{{ARCH}}`).
- [ ] `taskfiles/daemon.yaml` `build:daemon` builds from `./cmd/daemon` and writes to `bin/solo-provisioner-daemon-{OS}-{ARCH}`.
- [ ] Both new files carry SPDX headers (`# SPDX-License-Identifier: Apache-2.0`).

### `.github/workflows/zxc-uat-test.yaml`

- [ ] Line 463 calls `task build:cli GOOS=linux GOARCH=amd64` (renamed from `task build:weaver ...`).
- [ ] Line 465 still expects `bin/solo-provisioner-linux-amd64` — binary name didn't change.

### `CLAUDE.md`

- [ ] "CLI Layer" section header says `(cmd/cli/)` and entry-point sentence says `cmd/cli/main.go`.
- [ ] New "Daemon Layer (`cmd/daemon/`)" subsection follows the CLI Layer block.
- [ ] One example `task build:cli GOOS=linux GOARCH=amd64` (renamed).

### Cross-cutting

- [ ] `grep -rn "cmd/weaver" --include='*.go' --include='*.yaml' --include='*.yml' .` returns nothing outside `vendor/` and `docs/claude/{plans,reviews}/` (historical records).

---

## Running the Tests

### Lint (macOS-safe)

```bash
task lint:check
```

Expected: silent pass.

### Builds (macOS-safe)

```bash
task generate                                    # produces pkg/version/VERSION + pkg/version/COMMIT
task build:cli GOOS=linux GOARCH=amd64           # produces bin/solo-provisioner-linux-amd64
task build:daemon GOOS=linux GOARCH=amd64        # produces bin/solo-provisioner-daemon-linux-amd64
```

Expected:

```
task: [build:cli] go build -ldflags='-w -s' -trimpath -o bin/solo-provisioner-linux-amd64 ./cmd/cli
task: [build:cli] chmod +x bin/solo-provisioner-linux-amd64
task: [build:daemon] go build -ldflags='-w -s' -trimpath -o bin/solo-provisioner-daemon-linux-amd64 ./cmd/daemon
task: [build:daemon] chmod +x bin/solo-provisioner-daemon-linux-amd64
```

### Attack-surface check (macOS-safe)

```bash
GOOS=linux GOARCH=amd64 go list -deps ./cmd/daemon \
  | grep -E 'bubbletea|charmbracelet|hashgraph/solo-weaver/cmd/cli|hashgraph/solo-weaver/internal/ui|hashgraph/solo-weaver/internal/workflows'
```

Expected: no output (no matches).

```bash
echo "CLI deps:"   ; GOOS=linux GOARCH=amd64 go list -deps ./cmd/cli    2>/dev/null | wc -l
echo "Daemon deps:"; GOOS=linux GOARCH=amd64 go list -deps ./cmd/daemon 2>/dev/null | wc -l
```

Expected (approximate): CLI ~1091, daemon ~305.

### Sanity grep (macOS-safe)

```bash
grep -rn "cmd/weaver" --include='*.go' --include='*.yaml' --include='*.yml' . \
  | grep -v vendor \
  | grep -v "docs/claude/"
```

Expected: no output.

### Unit tests (must run inside the UTM VM — pre-existing `internal/mount` platform constraint)

```bash
task vm:test:unit
```

Spot-check the moved `root_test.go` and the new daemon-side surface:

```bash
task vm:test:unit TEST_PATHS=./cmd/cli/... TEST_REGEX="."
task vm:test:unit TEST_PATHS=./cmd/daemon/... TEST_REGEX="."
```

Expected: green for both. `cmd/daemon/` has no test files today; the package itself compiles green (that's the test).

### Integration smoke test (UTM VM)

Confirms the CLI binary still wires up the same Cobra tree from its new path:

```bash
task vm:test:integration TEST_NAME='^Test_StepKubeadm_Fresh_Integration$'
```

Expected: green.

---

## Manual UAT (inside the UTM VM)

### A. CLI binary surface

```bash
task build:cli GOOS=linux GOARCH=amd64
./bin/solo-provisioner-linux-amd64 --help
```

Expected — `daemon` is **not** in the subcommand list:

```
Available Commands:
  install         Install Solo Provisioner
  uninstall       Uninstall Solo Provisioner
  kube            Kubernetes cluster ops
  block           Block node lifecycle
  teleport        Teleport integration
  alloy           Alloy observability cluster
  version         Show version
  ...             (no 'daemon')
```

Sanity-check that the daemon subcommand is genuinely gone (not just hidden):

```bash
./bin/solo-provisioner-linux-amd64 daemon
# Expected: Error: unknown command "daemon" for "solo-provisioner"
```

```bash
./bin/solo-provisioner-linux-amd64 version --output yaml
# Expected: prints version/commit info (existing behavior, unchanged)
```

### B. Daemon binary surface

```bash
task build:daemon GOOS=linux GOARCH=amd64
./bin/solo-provisioner-daemon-linux-amd64 --help
```

Expected:

```
Long-lived foreground process started by the solo-provisioner-daemon.service systemd unit.

Usage:
  solo-provisioner-daemon [flags]
  solo-provisioner-daemon [command]

Available Commands:
  help        Help about any command
  version     Show version

Flags:
  -c, --config string      Path to config file
  -h, --help               help for solo-provisioner-daemon
      --log-level string   Set log level (debug, info, warn, error)
  -o, --output string      Output format (json, yaml) (default "json")
  -v, --version            Print version and exit
```

Smoke-run with the real config (only inside the VM where `/opt/solo/weaver/sandbox/etc/weaver/config.yaml` exists):

```bash
sudo ./bin/solo-provisioner-daemon-linux-amd64 --config /opt/solo/weaver/sandbox/etc/weaver/config.yaml &
DAEMON_PID=$!
sleep 1
tail -5 /opt/solo/weaver/sandbox/logs/solo-provisioner-daemon.log
# Expected: log line "Solo Provisioner daemon started"

kill -TERM $DAEMON_PID
wait $DAEMON_PID 2>/dev/null
tail -5 /opt/solo/weaver/sandbox/logs/solo-provisioner-daemon.log
# Expected: log line "Solo Provisioner daemon shutting down" with signal=terminated
```

### C. Existing UAT suite still passes

The full UAT suite covers the CLI's install/upgrade/reset/uninstall workflows and is unaffected by this PR's scope. Run it to confirm nothing regressed:

```bash
task uat:setup        # installs cluster + block node from scratch
task uat:core         # install → upgrade → reset
```

Expected: identical to pre-PR behavior. The annotation/proxy/version-stamping checks all remain green.

---

## Notes for the reviewer

- **Per-file git history preservation**: each renamed file should show its full pre-rename history with `git log --follow cmd/cli/<path>`. If GitHub's "Files changed" view collapses the rename into "added + deleted", the API-level rename detection should still pick it up; the local commit definitely records it (see `git show 887deeb --stat | head -10`).
- **License-check noise**: `task license:check` reports 18 missing license headers. Those failures are pre-existing on the epic branch base (verified by running the same command on `00498-feat-two-binary-build-layout` directly) — not introduced by this PR. Fixing them is out of scope.
- **The plan markdown lives on this branch**: `docs/claude/plans/00514-cli-main-entry-point.md` is the source-of-truth for design decisions, the design-constraints contract, and out-of-scope items. Worth a skim before code review if you weren't part of the planning conversation.
