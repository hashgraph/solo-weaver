# #615 — `--version` and `version` subcommand require install

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/615
> **Story branch:** `00615-fix-version-skip-global-checks`
> **PR base:** `main` (branched from `origin/main` @ `f07505c`)
> **PR closes:** #615

## Summary

The CLI's `--version` flag and `version` subcommand both fail on a freshly built binary because the root command's `PersistentPreRunE` (`common.RunPersistentPreRun`) always runs `CheckWeaverInstallationWorkflow` and any pending startup migrations before the version is printed. The fix opts the `version` subcommand out via the existing `SkipGlobalChecks` annotation, and short-circuits `RunPersistentPreRun` when the root command was invoked with `--version`.

## Problem

- `cmd/cli/commands/root.go:52-54` wires `PersistentPreRunE → common.RunPersistentPreRun`.
- `cmd/cli/commands/common/run.go:190-217` `RunPersistentPreRun` runs `CheckWeaverInstallationWorkflow` + `RunStartupMigrations` unless the command has `Annotations["requireGlobalChecks"] == "false"` (set via `common.SkipGlobalChecks`).
- Only `selfInstallCmd` (`cmd/cli/commands/root.go:87`) and `tuiDemoCmd` (`cmd/cli/commands/demo.go:34`) opt out today.
- `version.Cmd()` (`cmd/cli/commands/root.go:96`, returning a fresh `*cobra.Command` from `pkg/version/cli/releases.go:50`) is not opted out, so `./solo-provisioner version` fails on an uninstalled host.
- The `--version` flag is bound to the root command's `RunE` (`cmd/cli/commands/root.go:55-61`); cobra runs `PersistentPreRunE` first, so even `./solo-provisioner --version` fails before the print path is reached.
- Surfaced in the UAT for #516/#517 (PR #605) where reviewers are instructed to run `./bin/solo-provisioner-linux-amd64 --version` as a no-install sanity check.

The daemon binary (`cmd/daemon/main.go`) defines no `PersistentPreRunE`, so this is CLI-only — no daemon-side change.

## Decisions

| Question | Decision |
|---|---|
| How to opt the `version` subcommand out? | Capture the command returned by `version.Cmd()` into a local var, call `common.SkipGlobalChecks(versionCmd)`, then `rootCmd.AddCommand(versionCmd)` — mirroring the `selfInstallCmd` pattern. |
| How to opt the `--version` flag out? | Short-circuit `RunPersistentPreRun` at the top: when `cmd.Parent() == nil` (i.e. the root command itself is running) and the persistent `version` flag is set, return `nil` before any check or migration runs. |
| Why not also annotate the root? | `SkipGlobalChecks` is per-command; we cannot conditionally toggle it based on a flag value. The flag-check short-circuit is the minimal targeted bypass and keeps every non-`--version` invocation of the root command unchanged. |
| Should `block check --version` etc. also bypass? | No. `--version` only triggers the version-print path on the root's `RunE`; on subcommands the flag is inherited but inert. Preserving the install/migration checks for those invocations matches current behavior. |
| Scope of unit tests? | (1) a `common` package test that `RunPersistentPreRun` returns nil with no side effects when `--version` is set on a root-like command; (2) a `commands` package test that the registered `version` subcommand has `RequireGlobalChecks == false`. |

## Scope

### `cmd/cli/commands/common/run.go`
- [ ] In `RunPersistentPreRun`, after the `cmd == nil` guard, add a short-circuit: if `cmd.Parent() == nil` and `cmd.Flags().GetBool("version")` is true, return `nil`.

### `cmd/cli/commands/root.go`
- [ ] In `init()`, capture `version.Cmd()` into a local variable, call `common.SkipGlobalChecks(versionCmd)`, and use that variable in `rootCmd.AddCommand(versionCmd)`.

### `cmd/cli/commands/common/run_test.go`
- [ ] Add `TestRunPersistentPreRun_VersionFlagShortCircuit`: build a minimal cobra command with a `version` bool persistent flag, set it, call `RunPersistentPreRun`, expect `nil` and no panic (workflow not constructed).

### `cmd/cli/commands/root_test.go`
- [ ] Add `TestVersionSubcommandSkipsGlobalChecks`: walk `rootCmd`'s subcommands to find the one with `Use == "version"` and assert `common.RequireGlobalChecks(versionCmd) == false`.

## Out of scope

- `selfUninstallCmd` is not in the `SkipGlobalChecks` list either (`cmd/cli/commands/root.go:91` adds it without the annotation). That's a separate question — not raised in #615 and not changed here.
- Migrating `--version` handling away from `PersistentPreRunE`-then-`RunE` to cobra's built-in `Version` field (which would obviate the short-circuit entirely). Bigger surface change; defer.
- Any docs update under `docs/quickstart.md` — this PR neither adds nor removes a CLI flag/subcommand; semantics of `version` are unchanged (it just becomes invokable pre-install).

## Test plan

- [ ] Unit:
  - `go test -race ./cmd/cli/commands/common/...` — passes new short-circuit test.
  - `go test -race ./cmd/cli/commands/...` — passes new subcommand-annotation test.
- [ ] Manual UAT on a freshly built binary (no install):
  - `./bin/solo-provisioner-linux-<arch> --version` → prints version JSON (or `-o text` etc.), exit 0.
  - `./bin/solo-provisioner-linux-<arch> version` → same.
  - `./bin/solo-provisioner-linux-<arch> version -o json` → JSON output.
  - `./bin/solo-provisioner-linux-<arch> block node check --profile=local` → still triggers the installation check (current failure mode preserved on uninstalled hosts).
- [ ] `task lint` clean.
- [ ] `task license:check` clean.

## Risks / rollbacks

- **Risk:** the short-circuit accidentally bypasses checks for some other invocation path. Mitigation: condition is narrow (`cmd.Parent() == nil` AND `--version` flag set) — only matches `./solo-provisioner --version` (or `-v`), which is exactly the targeted case.
- **Risk:** `cmd.Flags().GetBool("version")` errors if the flag isn't defined on some test cmd. Mitigation: the function already ignores the error; lookup against the persistent flag (registered at init time) so production cmd always has it.
- **Rollback:** revert the two-file change; behavior returns to pre-fix. No state, schema, or wire-format changes.
