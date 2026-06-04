# PR Review Guide — fix(cli): make `--version` and `version` work on uninstalled hosts

**Issue:** [#615](https://github.com/hashgraph/solo-weaver/issues/615)
**Branch:** `00615-fix-version-skip-global-checks`

---

## What Was Done

### Problem

The CLI's `--version` flag and `version` subcommand both fail on a freshly built binary because the
root command's `PersistentPreRunE` (`common.RunPersistentPreRun`) always runs the
`CheckWeaverInstallationWorkflow` and any pending startup migrations before the version is printed.

- `version.Cmd()` is a normal subcommand and was not opted out via `common.SkipGlobalChecks`, so
  `./solo-provisioner version` fails on an uninstalled host.
- `--version` is bound to the root's `RunE`, but cobra runs `PersistentPreRunE` first, so
  `./solo-provisioner --version` fails before the print path is ever reached.

This was surfaced in the UAT for #516/#517 (PR #605), where reviewers are instructed to run
`./bin/solo-provisioner-linux-<arch> --version` as a no-install sanity check.

### Solution

Two narrowly scoped bypasses:

1. **`version` subcommand:** capture the command returned by `version.Cmd()` into a local variable
   and call `common.SkipGlobalChecks(versionCmd)` before adding it to `rootCmd`, mirroring the
   existing pattern for `selfInstallCmd`.
2. **`--version` flag:** short-circuit `RunPersistentPreRun` at the top when the running command is
   the root (`cmd.Parent() == nil`) **and** the persistent `version` flag is set. The check uses
   `cmd.PersistentFlags().GetBool("version")` so the same code path is exercisable in unit tests
   that invoke the PreRun directly without going through `cobra.Command.Execute()`.

The daemon binary (`cmd/daemon/main.go`) defines no `PersistentPreRunE`, so no daemon-side change is
needed.

### Files Changed

| File | Change |
|------|--------|
| `cmd/cli/commands/common/run.go` | Short-circuit `RunPersistentPreRun` when `cmd.Parent() == nil` **and** the `version` persistent flag is set; returns `nil` before the installation check or migrations run. Queries `PersistentFlags()` (not `Flags()`) so the path is testable without cobra's pre-Execute flag merge. |
| `cmd/cli/commands/root.go` | Capture `version.Cmd()` into a local variable, annotate it with `common.SkipGlobalChecks`, then `AddCommand(versionCmd)` — same pattern used for `selfInstallCmd`. |
| `cmd/cli/commands/common/run_test.go` | New `TestRunPersistentPreRun_VersionFlagShortCircuits` builds a minimal root-like cobra command with a `version` persistent flag, sets it, and asserts `RunPersistentPreRun` returns `nil` without reaching the workflow build (which would panic on test harness). |
| `cmd/cli/commands/root_test.go` | New `TestVersionSubcommandSkipsGlobalChecks` walks `rootCmd.Commands()` for `Use == "version"` and asserts `common.RequireGlobalChecks(versionCmd) == false`. |

---

## Code Review Checklist

### `cmd/cli/commands/common/run.go`

- [x] Short-circuit is placed **after** the `cmd == nil` guard and **before** `RequireGlobalChecks`,
      so a non-root caller (e.g. `block node check`) never enters the bypass.
- [x] Guard is `cmd.Parent() == nil` — the short-circuit only fires on the root command, not on any
      subcommand that inherits the persistent `--version` flag.
- [x] Flag is read via `cmd.PersistentFlags().GetBool(FlagVersion().Name)` rather than the literal
      `"version"`, so a future rename via the shared flag descriptor can't silently break the
      bypass. Uses `PersistentFlags()` (not `Flags()`) so the path is reachable in unit tests that
      bypass cobra's `Execute()` flag-merge step.
- [x] Error returned by `GetBool` is intentionally discarded (`_`) — if the flag isn't registered,
      `v` is the zero value (`false`) and the bypass simply doesn't fire, falling through to the
      normal checks. This matches the safety profile of the existing flag lookups in this file.

### `cmd/cli/commands/root.go`

- [x] `versionCmd := version.Cmd()` is captured once; the same `*cobra.Command` is passed to both
      `common.SkipGlobalChecks(versionCmd)` and `rootCmd.AddCommand(versionCmd)` — no double-call to
      `version.Cmd()`.
- [x] Annotation happens **before** `AddCommand` so the command tree never observes an unannotated
      `version` subcommand.

### `cmd/cli/commands/common/run_test.go`

- [x] Test creates a minimal `*cobra.Command` with no `Run`/`RunE` — the PreRun must return cleanly
      without trying to build a workflow. If the short-circuit regressed, the test would hit the
      workflow-build path and panic.
- [x] Flag is registered on `PersistentFlags()` (matching production) and set via
      `PersistentFlags().Set`, which exercises the same lookup path the production code uses.

### `cmd/cli/commands/root_test.go`

- [x] Test walks the real `rootCmd.Commands()` registered by `init()`, not a mock — proves the
      annotation reaches the registered subcommand, not just an in-memory copy.
- [x] Fails loudly with a clear message if the `version` subcommand is removed or renamed
      (`require.NotNil` with explanatory message before the annotation assertion).

### Scope

- [x] `selfUninstallCmd` is **not** in the `SkipGlobalChecks` list either (`root.go:91`). That is
      intentionally out of scope for this PR — not raised in #615.
- [x] `block node check --version`, `block node install --version`, etc. still go through the
      installation check. The flag is inherited but inert on subcommands (only the root has a
      print path bound to `RunE`), so preserving the check matches current behavior.

---

## Running the Tests

### Unit tests for the changed packages

The `cmd/cli/commands/common` package transitively imports `internal/mount`, which is Linux-only.
On macOS the package will not compile; run the tests inside the UTM VM:

```bash
task vm:test:unit
```

Or, scoped to the two packages exercised by this PR:

```bash
go test -race -tags='!integration' \
  ./cmd/cli/commands/common/... \
  ./cmd/cli/commands/...
```

Expected new lines in the output:

```
=== RUN   TestRunPersistentPreRun_VersionFlagShortCircuits
--- PASS: TestRunPersistentPreRun_VersionFlagShortCircuits

=== RUN   TestVersionSubcommandSkipsGlobalChecks
--- PASS: TestVersionSubcommandSkipsGlobalChecks
```

### Integration tests

No integration test was added — the bypass is fully exercised at the unit level and via manual UAT.
Existing integration tests are unaffected (this PR does not change any workflow step or check
behavior for installed hosts).

---

## Manual UAT Steps

UAT must be done with a **freshly built binary on a host without `solo-provisioner` installed**, so
that the installation check would fail if the bypass were missing. The easiest such host is the
UTM VM with no prior `task vm:install` or `sudo .../solo-provisioner install` run.

```bash
task build:cli GOOS=linux GOARCH=arm64    # or GOARCH=amd64
scp -i .ssh/id_rsa_vm bin/solo-provisioner-linux-arm64 provisioner@<vm-ip>:/tmp/solo-provisioner-test
ssh -i .ssh/id_rsa_vm provisioner@<vm-ip>
chmod +x /tmp/solo-provisioner-test
```

Then on the VM:

```bash
# 1) --version flag on the root command
/tmp/solo-provisioner-test --version
```

Expected output (single line, exit 0):

```
{"version":"0.0.0","commit":"<short-sha>","goversion":"go1.26.0"}
```

```bash
# 2) Short form of the flag
/tmp/solo-provisioner-test -v
```

Same JSON output, exit 0.

```bash
# 3) version subcommand (default output)
/tmp/solo-provisioner-test version
```

Same JSON output, exit 0.

```bash
# 4) version subcommand with explicit JSON output
/tmp/solo-provisioner-test version -o json
```

Same JSON output, exit 0.

```bash
# 5) Negative case — subcommand that should still trigger the installation check
/tmp/solo-provisioner-test block node check --profile=local
```

Expected output (exit 1):

```
Solo Provisioner installation check failed: current executable is not in the expected bin directory
  expectedPath=/opt/solo/weaver/bin/solo-provisioner

✗ Error: solo-provisioner installation or re-installation required

Resolution:
  install or re-install solo-provisioner binary; run
  `sudo /tmp/solo-provisioner-test install` to install and then run
  `solo-provisioner block node check --profile=local`.
```

This last case proves the bypass is narrowly scoped — only `--version` and `version` skip the
installation check; every other path on an uninstalled host still fails loudly.

---

## Risks / Rollback

- **Risk:** the short-circuit accidentally bypasses checks for some other invocation path.
  Mitigation: the condition is `cmd.Parent() == nil` **AND** `--version` flag set — only matches
  `./solo-provisioner --version` (or `-v`), which is exactly the targeted case. The UAT step 5
  above verifies this.
- **Risk:** `cmd.PersistentFlags().GetBool(FlagVersion().Name)` errors if the flag isn't defined
  on some caller. Mitigation: the function ignores the error; on the production root command the
  flag always exists (registered in `init()` at `cmd/cli/commands/root.go` via the same
  `FlagVersion()` descriptor).
- **Rollback:** revert the two-file change in `cmd/cli/commands/`. Behavior returns to pre-fix.
  No state, schema, or wire-format changes.
