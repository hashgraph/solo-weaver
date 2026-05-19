# #392 — Migrate command-local flags to FlagDefinition for consistency and collision detection

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/392
> **Branch:** 00392-migrate-command-local-flags-to-flagdefinition
> **Base:** origin/main @ 484a0aa

## Summary

Three command files register their persistent flags directly against Cobra/pflag (`PersistentFlags().StringVar(...)`, `BoolVar(...)`, `StringArrayVar(...)`) instead of using the repository's `FlagDefinition[T]` helper in `cmd/weaver/commands/common/`. This makes them bypass the typed registration path, miss the test scaffolding that exercises `Value`/`Clone`/required semantics, and risk going unseen by `DetectShortNameCollisions` if it's ever scoped to `FlagDefinition` introspection. The PR moves all of those registrations behind `FlagDefinition[T]` factories.

## Problem

`PersistentFlags().StringVar(...)` calls bypass `FlagDefinition[T]` in:
- `cmd/weaver/commands/block/node/node.go:66-94` — 19 persistent flags (chart, namespace, release-name, storage paths, sizes, retention, plugin preset, plugin list, load-balancer toggle).
- `cmd/weaver/commands/alloy/cluster/cluster.go:36-57` — 9 persistent flags including two repeatable `StringArrayVar` flags (`--add-prometheus-remote`, `--add-loki-remote`) and the hidden `--cluster-secret-store`.
- `cmd/weaver/commands/teleport/cluster/cluster.go:32-33` — 2 persistent flags (`--version`, `--values`).

The contrast with the working pattern is visible elsewhere in the same files: `node.go:56-58` already uses `common.FlagStopOnError().SetVarP(...)` for execution-mode flags. The migration is to extend that pattern to the rest.

`FlagDefinition[T]` in `cmd/weaver/commands/common/flags.go:19-274` already supports `string`, `bool`, `int`, `int64`, `uint`, `uint64`, `[]string` (as `StringSlice`), and `time.Duration`. Read sites use package-level `var`s; `SetVarP` already binds those vars, so call sites in `install.go`/`upgrade.go`/etc. do not need to change.

## Decisions

| Question | Decision |
|---|---|
| Where do new `FlagXxx()` factories live? | New file `cmd/weaver/commands/common/flags_blocknode.go`, `flags_alloy.go`, `flags_teleport.go` — keep `common.go` from ballooning, mirror the per-command grouping that already exists implicitly. |
| `StringArrayVar` vs `StringSliceVar` for `--add-prometheus-remote` / `--add-loki-remote`? | **Keep `StringArrayVar` semantics.** The remote spec format (`name=x,url=y,username=z,labelProfile=eng`) embeds commas as separators inside one value — `StringSlice` would shred those into separate remotes. |
| How to expose slice-vs-array in the helper, given `FlagDefinition[[]string]` already exists? | The current `FlagDefinition[[]string]` branch has **zero production callers** (only one unit test). Replace it with two parallel, behavior-named types — `CommaSplitStringsFlagDefinition` (was the implicit `case []string:` branch, uses `StringSliceVarP`) and `RepeatableStringFlagDefinition` (new, uses `StringArrayVarP`). Avoids pflag's slice/array jargon and makes the user-facing semantics obvious at the call site. Remove `case []string:` from `FlagDefinition[T]`'s switch and update its single test to the new type. |
| How to support `MarkHidden` (used on `--cluster-secret-store`)? | Add `(FlagDefinition[T]).SetVarPHidden(cmd, p, required)` thin wrapper that calls `SetVarP` then `cmd.PersistentFlags().MarkHidden(fp.Name)`. Keeps the descriptor surface minimal — hidden-ness is a registration option, not a flag attribute. |
| Do read sites change? | **No.** `SetVarP` still binds the same package-level `var`, so `flagClusterName` etc. continue to be read directly. Only the `init()` block changes. |
| Subcommand short-name collisions? | Run `DetectShortNameCollisions` (or a one-off unit test) after migration. None of the migrated flags currently set a `ShortName`, so the risk surface is zero — but verifying is cheap. |
| Backward-compatibility for flag names/defaults? | **Preserve exactly.** Same names, same descriptions, same defaults — this is a registration refactor, not a UX change. Diff `--help` output before/after as a check. |

## Scope

### `cmd/weaver/commands/common/` — helper extensions
- [ ] Remove `case []string:` from `FlagDefinition[T]`'s `valueFrom` and `setFlagVar` switches in `flags.go` (zero production callers).
- [ ] Add `CommaSplitStringsFlagDefinition` (uses `StringSliceVarP` / `GetStringSlice`) — replacement for the removed generic `[]string` branch.
- [ ] Add `RepeatableStringFlagDefinition` (uses `StringArrayVarP` / `GetStringArray`) — for `--add-prometheus-remote` and `--add-loki-remote`.
- [ ] Both new types expose the same surface as `FlagDefinition[T]`: `SetVarP`, `SetVar`, `Value`, `ValueLocal`, `ValueOwnPersistent`, `Clone`, `MarkRequired`, `MarkRequiredP`.
- [ ] Add `SetVarPHidden(...)` helper on `FlagDefinition[T]` (and on the two new types) for the `--cluster-secret-store` case — registers the flag, then calls `MarkHidden`.
- [ ] Update `flags_test.go`: convert the existing `FlagDefinition[[]string]` test (`flags_test.go:102`) to `CommaSplitStringsFlagDefinition`; add a parallel test for `RepeatableStringFlagDefinition` that asserts comma-bearing values are preserved across two `--flag` occurrences; add a test for `SetVarPHidden` that confirms the flag is registered and `Hidden == true`.

### `cmd/weaver/commands/common/flags_blocknode.go` (new)
- [ ] Factory functions for the 19 block-node persistent flags currently registered in `node.go:66-94`:
  - `FlagChartRepo`, `FlagNamespace`, `FlagReleaseName`
  - `FlagBasePath`, `FlagArchivePath`, `FlagLivePath`, `FlagLogPath`, `FlagVerificationPath`, `FlagPluginsPath`
  - `FlagLiveSize`, `FlagArchiveSize`, `FlagLogSize`, `FlagVerificationSize`, `FlagPluginsSize`
  - `FlagHistoricRetention`, `FlagRecentRetention`
  - `FlagPluginPreset`, `FlagPlugins`
  - `FlagLoadBalancerEnabled`

### `cmd/weaver/commands/common/flags_alloy.go` (new)
- [ ] Factories for `FlagClusterName`, `FlagMonitorBlockNode`, `FlagClusterSecretStore`, `FlagPrometheusURL`, `FlagPrometheusUsername`, `FlagLokiURL`, `FlagLokiUsername`.
- [ ] `FlagPrometheusRemotes`, `FlagLokiRemotes` returning `RepeatableStringFlagDefinition`.

### `cmd/weaver/commands/common/flags_teleport.go` (new)
- [ ] `FlagTeleportVersion` (note: not the same flag as the root `--version`; this one is on `teleport cluster`), `FlagTeleportValuesFile`.

### `cmd/weaver/commands/block/node/node.go` — call-site migration
- [ ] Replace lines 66-94 with `common.FlagXxx().SetVarP(nodeCmd, &flagXxx, false)` calls. Leave the `var` block intact.

### `cmd/weaver/commands/alloy/cluster/cluster.go` — call-site migration
- [ ] Replace lines 36-57 with factory-based registration: `--cluster-secret-store` via `SetVarPHidden`, repeatable remotes via `RepeatableStringFlagDefinition`, rest via standard `FlagDefinition[T].SetVarP`.

### `cmd/weaver/commands/teleport/cluster/cluster.go` — call-site migration
- [ ] Replace lines 32-33. Note: `flagVersion` here shadows the root `--version` only because it's a subtree-level persistent flag with no shorthand — verify `--version` on `teleport cluster install` still resolves correctly post-change (it should: same name, same scope).

### Verification
- [ ] Build: `task build:weaver`
- [ ] Lint: `task lint`
- [ ] `solo-provisioner --help`, `solo-provisioner block node --help`, `solo-provisioner alloy cluster --help`, `solo-provisioner teleport cluster --help` — diff against `main` baseline; expect zero text diffs.
- [ ] `DetectShortNameCollisions` returns `false` post-change (existing root-level test, if any; otherwise add one).

## Out of scope

- Renaming, deprecating, or changing defaults of any migrated flag.
- Adding new flags or new behavior to any of the three commands.
- Migrating non-persistent (local) flags on subcommands — issue scope is specifically the three command-root files listed.
- Refactoring `init.go`/`install.go`/`upgrade.go` read sites — they keep using the package-level `var`.
- Generalizing `FlagDefinition` beyond what's needed for these three files (e.g. `Float64`, `IP`, etc.) — defer until a real need exists.

## Test plan

- [ ] Unit: `task test:unit` — focus on `./cmd/weaver/commands/common/...` (existing `flags_test.go` plus the new array + hidden tests).
- [ ] Targeted unit test: a small test that registers a representative set of the migrated flags against a stub `cobra.Command` and asserts the resulting `--help` output matches the legacy registration string-for-string (defensive against accidental description drift).
- [ ] Manual smoke (macOS, no VM needed): build the binary, run `solo-provisioner block node install --help`, `… alloy cluster install --help`, `… teleport cluster install --help` and confirm flags appear with expected defaults / descriptions / hidden status.
- [ ] Integration: not required — this is a registration refactor with no runtime semantic change. Standard `task vm:test:integration` is sufficient as a sanity check, no targeted test additions.

## Risks / rollbacks

- **Risk:** Switching `--add-prometheus-remote` / `--add-loki-remote` from `StringArrayVar` to `StringSliceVar` would silently corrupt user inputs that contain commas inside the spec (the documented format does). **Mitigation:** explicit decision above — these flags use `RepeatableStringFlagDefinition` (backed by `StringArrayVarP`); covered by a dedicated unit test that asserts comma-bearing values survive across two `--flag` occurrences.
- **Risk:** `MarkHidden` ordering — Cobra requires the flag to exist before it's marked hidden. **Mitigation:** the new `SetVarPHidden` runs `SetVarP` first, then `MarkHidden`, in that order.
- **Risk:** Subtle `--help` text drift if a description string is retyped slightly differently. **Mitigation:** copy-paste exact strings from current source; add a help-output diff check as part of manual verification.
- **Rollback:** Pure refactor with no schema or on-disk-state changes. Revert the PR and the previous registration pattern is restored — no migration needed.
