# #514 + #515 — Two-binary entry points (`solo-provisioner` CLI and `solo-provisioner-daemon`)

> **Issues:** https://github.com/hashgraph/solo-weaver/issues/514 and https://github.com/hashgraph/solo-weaver/issues/515
> **Epic:** #498 — Two-Binary Build Layout
> **Story branch:** `00514-cli-main-entry-point`
> **Epic branch (PR base):** `00498-feat-two-binary-build-layout` (branched from `origin/main` @ `7b410a5`)
> **PR closes:** #514, #515

## Summary

Land the directory layout and entry points for both binaries under epic #498 in a single PR:

1. **#514 — CLI rename.** Move `cmd/weaver/` → `cmd/cli/` so the `solo-provisioner` binary is built from a path that matches its role. The Cobra tree, subcommands, and binary name are unchanged — only file locations and the Go import paths under `cmd/weaver/...` move.
2. **#515 — Daemon split.** Create `cmd/daemon/main.go` with its own minimal Cobra root command, producing a separate `solo-provisioner-daemon` binary. Remove the `daemon` subcommand from the CLI's Cobra tree and unregister `daemonCmd` from the CLI root. This is where the attack-surface constraint (each binary contains only what it needs) is actually enforced.

This is the first PR in epic #498's four-story arc. Story-body quotes (verbatim from each issue) so scope boundaries are pinned down:

| # | Story | Body excerpt | Status |
|---|---|---|---|
| #514 | `cmd/cli/main.go` for `solo-provisioner` | "Create `cmd/cli/main.go` as the entry point for the `solo-provisioner` CLI binary, wiring up the existing Cobra command tree." | **this PR** |
| #515 | `cmd/daemon/main.go` for `solo-provisioner-daemon` | "Create `cmd/daemon/main.go` as the entry point for the `solo-provisioner-daemon` binary with its own Cobra root command." | **this PR** |
| #516 | Taskfile build targets for both binaries per platform | "Update `Taskfile.yaml` so build targets produce both `solo-provisioner` and `solo-provisioner-daemon` binaries for all target platforms (linux/amd64, linux/arm64)." | follow-up |
| #517 | CI/CD to publish both binaries in the same release | "Update CI/CD pipeline to publish both `solo-provisioner` and `solo-provisioner-daemon` as **named artifacts** in the same GitHub release tag." | follow-up |

The framing matters: #516 wants **build targets** (plural) producing both binaries — i.e., parallel, independent target families, not a single aggregate target. #517 wants **named artifacts**, not a `bin/*` glob — i.e., the release config explicitly lists each binary by name. This shape preserves the option to release them independently later (different versions, different tags) without restructuring the pipeline again.

### Design constraints

Setting the design contract for the epic so the work doesn't drift from it:

1. **Two binaries, not one — required for safe upgrades.** The daemon must be replaceable independently of the CLI.
2. **Each binary includes only the object code needed for its own features.** Attack-surface minimization: the daemon binary must not bundle CLI-only packages, and vice versa.
3. **Layout is symmetric: `cmd/cli/main.go` and `cmd/daemon/main.go`.** Same repo, same code base, two compiled executables.
4. **Independent build and release per binary.** Each binary gets its own Taskfile target family (`build:cli:*` and `build:daemon:*` — #516) and is published as its own named release artifact (#517). They may share the same release tag today, but the design preserves the option to ship them at different cadences, different versions, or different tags later — no further pipeline restructure required.
5. **Daemon is a long-running process; CLI is one-shot.** The daemon survives across CLI invocations; the CLI talks to it (or merely co-exists with it) at runtime. The specific runtime contract between them — IPC mechanism, message format, compatibility/protocol versioning — is a future concern, out of scope for this epic.

This PR addresses constraint #3 fully (both `cmd/cli/` and `cmd/daemon/` exist at the end) and constraint #2 (the daemon binary will not import any `cmd/cli/...` package). Constraints #1, #4, and #5 are about release/upgrade pipelines and runtime — out of scope here.

## Problem

Today the `solo-provisioner` binary is built from `cmd/weaver/`, a path that names the project, not the binary's role under the planned split. There is no separate daemon binary — the daemon is a subcommand inside the CLI tree (`solo-provisioner daemon`), so the systemd unit currently runs the *entire CLI binary* with a single argument. That bundles every CLI subcommand, TUI dependency, workflow engine, and migration framework into a long-running service that needs none of them, and forces the CLI and daemon to upgrade in lockstep.

Current state:

- `cmd/weaver/main.go:13-20` — the CLI's `main()`.
- `cmd/weaver/commands/root.go:48-62` — the `rootCmd` with `Use: "solo-provisioner"`.
- `cmd/weaver/commands/root.go:97` — `rootCmd.AddCommand(daemonCmd)` registers the daemon as a CLI subcommand.
- `cmd/weaver/commands/root.go:173-178` — log filename branches on `os.Args[1] == "daemon"`.
- `cmd/weaver/commands/daemon.go` — the daemon's `RunE` (signal-handling loop). Imports `cmd/weaver/commands/common` solely for `SkipGlobalChecks`.
- `cmd/weaver/commands/common/run.go` — `common` pulls in `bubbletea`, `automa`, `internal/workflows/...`, `internal/ui`, `internal/migration`, etc. Linking against this package drags the full CLI runtime in.
- `Taskfile.yaml:167` — `go build … ./cmd/weaver` produces `bin/solo-provisioner-{OS}-{ARCH}`.
- `cmd/weaver-shim/main.go` — empty stub, unused. Out of scope here.

## Decisions

| Question | Decision |
|---|---|
| Rename `cmd/weaver/` or add `cmd/cli/` alongside it? | Rename. The additive path leaves a duplicate entry point with no purpose. |
| Move the `commands` subtree too? | Yes — move the whole directory; rewrite every `github.com/hashgraph/solo-weaver/cmd/weaver/...` import to `…/cmd/cli/...`. No external (`internal/`, `pkg/`) packages import `cmd/weaver`, so the rewrite is local to the moved tree. |
| Update the CLI Taskfile build path? | Yes — flip `./cmd/weaver` to `./cmd/cli` at `Taskfile.yaml:167`. Required for the build to stay green. |
| Where does the daemon's Cobra root live? | Inline in `cmd/daemon/`. The daemon binary today is a single command (signal loop). No `cmd/daemon/commands/` subtree until it actually has subcommands. |
| Does the daemon share any of `cmd/cli/commands/common`? | **No.** Importing that package would pull in TUI + workflow engine — explicit violation of the attack-surface constraint. The daemon has its own root with no `PersistentPreRunE`, so it doesn't need `SkipGlobalChecks` at all. Other helpers (`FlagConfig`, `FlagLogLevel`, `FlagVersion`) are tiny — duplicate the few flag descriptors needed, or factor into a tiny `internal/cmdflags/` package if duplication exceeds ~30 LOC. |
| Where does the daemon's bootstrap (config + log + proxy init) live? | Duplicate the ~30 lines needed from `cmd/cli/commands/root.go:initConfig` into `cmd/daemon/main.go`. The CLI's `initConfig` does too much TUI-aware work to share cleanly. A future refactor can extract a shared bootstrap helper if maintenance pain shows up. |
| Daemon's log filename? | `solo-provisioner-daemon.log`, set directly by `cmd/daemon/main.go`. The CLI's `os.Args[1] == "daemon"` branch (`cmd/cli/commands/root.go:173-178`) becomes dead code and gets removed. |
| Touch the `daemon` Cobra subcommand? | **Yes — this is the heart of #515.** Delete `cmd/cli/commands/daemon.go`, remove `rootCmd.AddCommand(daemonCmd)` from `cmd/cli/commands/root.go:97`, and move the signal-handling `RunE` body to `cmd/daemon/main.go`. After this PR, `solo-provisioner daemon` returns "unknown command"; the daemon runs only as the standalone `solo-provisioner-daemon` binary. This is the change that physically enforces attack-surface constraint #2. |
| Touch `cmd/weaver-shim/`? | No. Empty stub; unrelated cleanup. |
| Daemon build target in Taskfile? | Extract both binaries' build/hash/sign families into `taskfiles/cli.yaml` and `taskfiles/daemon.yaml` (matching the repo's existing `taskfiles/*.yaml` include pattern used for `alloy`, `teleport`, `proxy`, `tests`, etc.). Rename `build:weaver:*`/`hash:weaver:*`/`sign:weaver:*` → `build:cli:*`/`hash:cli:*`/`sign:cli:*` for symmetry with the new `build:daemon:*` family. Top-level `build`/`hash`/`sign` aggregators are retargeted to call `:cli:all`. Top-level aggregators continue to build only the CLI; daemon is built via explicit `task build:daemon`. Wiring the daemon into the top-level aggregator is deferred to #516. |
| Update `CLAUDE.md`? | Yes — update the "CLI Layer" path reference and add a one-line note about the daemon binary's location. |
| Binary name change? | No. CLI stays `solo-provisioner`; daemon is `solo-provisioner-daemon`. Both match the epic's naming. |

## Scope

### Part A — Move the CLI entry point (#514)

- [ ] `git mv cmd/weaver/main.go cmd/cli/main.go`
- [ ] Update the import in `cmd/cli/main.go` from `github.com/hashgraph/solo-weaver/cmd/weaver/commands` to `github.com/hashgraph/solo-weaver/cmd/cli/commands`.
- [ ] `git mv cmd/weaver/commands cmd/cli/commands` (moves all files under `commands/` — `root.go`, `demo.go`, `self_install.go`, `root_test.go`, and the `alloy/`, `block/`, `common/`, `kube/`, `teleport/` subtrees — preserving per-file history).
- [ ] Rewrite every `github.com/hashgraph/solo-weaver/cmd/weaver/` import path under the moved tree to `github.com/hashgraph/solo-weaver/cmd/cli/` (mechanically: `find cmd/cli -name '*.go' -exec sed -i '' 's|hashgraph/solo-weaver/cmd/weaver|hashgraph/solo-weaver/cmd/cli|g' {} +`).
- [ ] Sanity grep after rewrite: `grep -rn "hashgraph/solo-weaver/cmd/weaver" --include='*.go' .` returns zero results outside `vendor/` (historical plan/review docs that mention old paths are point-in-time records and left alone).
- [ ] Delete the now-empty `cmd/weaver/` directory.
- [ ] `Taskfile.yaml:167` — change `./cmd/weaver` to `./cmd/cli`.

### Part B — Split the daemon into its own binary (#515)

- [ ] Create `cmd/daemon/main.go` containing:
  - Trace-ID + context setup (mirroring `cmd/cli/main.go`).
  - A self-contained `initConfig(ctx)` that reads the config file, sets up logging with `Filename = "solo-provisioner-daemon.log"`, and activates the proxy. Reuse `pkgconfig`, `models`, `logx`, and `proxy` package APIs directly — do **not** import anything under `cmd/cli/...`.
  - A `daemonRootCmd` with `Use: "solo-provisioner-daemon"` whose `RunE` is the signal-handling loop currently in `cmd/cli/commands/daemon.go`.
  - Persistent flags: `--config`, `--log-level`. (No `--version` short flag conflict to worry about — keep `--version` if cheap, drop if it complicates anything.)
  - `Execute(ctx)` entry that invokes `daemonRootCmd.ExecuteContextC(ctx)`.
- [ ] Delete `cmd/cli/commands/daemon.go`.
- [ ] In `cmd/cli/commands/root.go`:
  - Remove `rootCmd.AddCommand(daemonCmd)` (currently line 97).
  - Replace the conditional log-filename block (`os.Args[1] == "daemon"`) with a flat `logConfig.Filename = "solo-provisioner.log"`.
- [ ] Extract both binaries' build/hash/sign families into the repo's existing `taskfiles/*.yaml` include pattern:
  - `taskfiles/cli.yaml` — `build:cli`/`hash:cli:*`/`sign:cli:*`/`run:cli`/`run:cli:skipdl` (renamed from the existing `*:weaver:*` family; `run:cli` also fixes a pre-existing stale `bin/weaver-*` path that should always have been `bin/solo-provisioner-*`).
  - `taskfiles/daemon.yaml` — `build:daemon`/`hash:daemon:*`/`sign:daemon:*` producing `bin/solo-provisioner-daemon-{OS}-{ARCH}` from `./cmd/daemon`.
  - Root `Taskfile.yaml` — add `includes:` entries for both with `flatten: true`; retarget the top-level `build`/`hash`/`sign` aggregators to call `:cli:all`. The top-level aggregators continue to build only the CLI; daemon is built via explicit `task build:daemon`. Wiring the daemon into the top-level aggregator is deferred to #516; `.releaserc` named-artifact changes are deferred to #517.
- [ ] Update `.github/workflows/zxc-uat-test.yaml` to call `task build:cli` (the renamed task), since it currently calls `task build:weaver` directly.
- [ ] Verify `grep -rn "hashgraph/solo-weaver/cmd/cli" cmd/daemon/` returns nothing — the daemon binary must not depend on any `cmd/cli/...` package.

### Part C — Docs

- [ ] `CLAUDE.md` — update the "CLI Layer" section heading from `(cmd/weaver/)` to `(cmd/cli/)` and the entry-point reference. Add a short "Daemon Layer (`cmd/daemon/`)" subsection describing the standalone daemon binary at one-paragraph depth.

## Out of scope

- Multi-target Taskfile aggregation (top-level `task build` producing both binaries) — #516.
- CI/CD release pipeline updates so both binaries land in the same GitHub Release — #517.
- The CLI ↔ daemon runtime contract (IPC mechanism, message format, protocol versioning) — future epic.
- Deleting `cmd/weaver-shim/` — unrelated cleanup.
- Updating historical plan/review docs under `docs/claude/{plans,reviews}/*.md` that mention old `cmd/weaver/...` paths.
- Any change to CLI subcommand logic, flags, or behavior (other than removing the daemon subcommand).
- Extracting a shared bootstrap helper for log/config/proxy init between the two binaries. Acceptable duplication for now; revisit if it grows.

## Test plan

- [ ] Lint: `task lint:check` passes.
- [ ] License: `task license:check` passes (preserved SPDX headers on moved files; new SPDX header on `cmd/daemon/main.go`).
- [ ] Build CLI: `task build:cli GOOS=linux GOARCH=amd64` produces `bin/solo-provisioner-linux-amd64`.
- [ ] Build daemon: `task build:daemon GOOS=linux GOARCH=amd64` produces `bin/solo-provisioner-daemon-linux-amd64`.
- [ ] Unit (macOS-safe): `go test -race -tags='!integration' ./cmd/cli/... ./cmd/daemon/...` — relocated `root_test.go` runs green; any new daemon tests run green.
- [ ] Unit (full, in UTM VM): `task vm:test:unit` passes.
- [ ] Integration (UTM VM, smoke): `task vm:test:integration TEST_NAME='^Test_StepKubeadm_Fresh_Integration$'` — confirms the CLI binary still wires up the same Cobra tree after the rename.
- [ ] Manual UAT CLI: `./bin/solo-provisioner-linux-amd64 --help` lists `install`, `uninstall`, `kube`, `block`, `teleport`, `alloy`, `version` (and `demo`). **`daemon` is no longer listed.** Running `./bin/solo-provisioner-linux-amd64 daemon` fails with "unknown command".
- [ ] Manual UAT daemon: `./bin/solo-provisioner-daemon-linux-amd64 --config /opt/solo/weaver/sandbox/etc/weaver/config.yaml` starts the daemon, writes to `solo-provisioner-daemon.log`, and exits cleanly on SIGTERM.
- [ ] Attack-surface check: `go list -deps ./cmd/daemon | grep -E 'bubbletea|charmbracelet|cmd/cli'` returns **nothing**. The daemon binary must not transitively depend on TUI libraries or any `cmd/cli/...` package.
- [ ] Sanity grep: `grep -rn "cmd/weaver" --include='*.go' --include='*.yaml' .` returns nothing outside `vendor/`.

## Acceptance criteria

- [ ] `cmd/weaver/` is gone; `cmd/cli/main.go` and `cmd/cli/commands/...` exist with the same contents minus import rewrites.
- [ ] `cmd/daemon/main.go` exists and produces a working `solo-provisioner-daemon` binary.
- [ ] The CLI binary's `--help` no longer mentions `daemon`; `rootCmd.AddCommand(daemonCmd)` is gone.
- [ ] No file under `cmd/daemon/` imports `github.com/hashgraph/solo-weaver/cmd/cli/...`. Verified by grep and by `go list -deps`.
- [ ] No file outside `vendor/` imports `github.com/hashgraph/solo-weaver/cmd/weaver/...`.
- [ ] `Taskfile.yaml`: CLI build target points at `./cmd/cli`. Daemon `build:daemon`/`hash:daemon:*`/`sign:daemon:*` tasks exist. Top-level `build`/`hash`/`sign` aggregators unchanged.
- [ ] `CLAUDE.md` references `cmd/cli/` in the CLI Layer section and mentions the new daemon binary.
- [ ] `task lint`, `task test:unit` (VM), and the smoke integration test pass.
- [ ] CLI's produced binary `--help`, flags, and remaining subcommands are byte-identical to main's output except: (a) `daemon` subcommand missing, (b) version/commit metadata.

## Risks / rollbacks

- **Risk — silent import-path miss.** A single un-rewritten `cmd/weaver/` import would fail `go build`; mitigated by the sanity grep + clean rebuild.
- **Risk — daemon pulls in TUI deps via a non-obvious transitive import.** Mitigated by the `go list -deps` check in the test plan. If it surfaces, the fix is to relocate the offending package, not to special-case the daemon.
- **Risk — release pipeline starts publishing the daemon prematurely.** Mitigated by leaving the top-level `task build` aggregator and `.releaserc` untouched (other than retargeting `build:weaver:all` → `build:cli:all` to follow the rename). The release pipeline calls `task hash → task build → build:cli:all`, so only the CLI is in `bin/*` until #516 lands; even if a daemon binary did sneak into `bin/`, `.releaserc`'s `bin/*` glob and the eventual #517 named-artifact list together govern what actually gets attached to the release.
- **Risk — `cmd/weaver-shim/` (empty stub) breaks `go vet ./...` or `go test ./...` because the package has no Go files.** Mitigated: it has `package main` declared, which keeps it valid; leave it alone.
- **Risk — conflict with parallel work on the same files.** Low — the rename touches ~50 files but the changes are mechanical and self-contained. Coordinate by landing this first.
- **Rollback:** Revert the merge commit. The change is mechanical, self-contained, and does not modify state on disk for existing installs.

## References

- Issues: #514 (CLI entry), #515 (daemon entry)
- Epic: #498 — Two-Binary Build Layout
- Sibling stories not in this PR: #516 (wires the daemon into the top-level `task build`/`hash`/`sign` aggregators so a single `task build` produces both binaries — the per-binary task families and the `build:weaver:* → build:cli:*` rename land in this PR; only the aggregator wiring is #516's job), #517 (CI/CD publishing both binaries as named release artifacts; owns `.releaserc` asset-list changes and the release workflow update)
- Current code:
  - `cmd/weaver/main.go:13-20`
  - `cmd/weaver/commands/root.go:48-62`, `root.go:90-98`, `root.go:173-178`
  - `cmd/weaver/commands/daemon.go`
  - `cmd/weaver/commands/common/run.go:264-272` (`SkipGlobalChecks`)
  - `Taskfile.yaml:135-241` (build/hash/sign families)
- HIP context (background only, not load-bearing for this PR):
  - `hip-xxxx1 - network-deployment.md` — `solo-provisioner` CLI vs daemon roles
  - `solo-weaver-catalog-alternatives.md` — embedded catalog versioning model
