# Review guide — #392: Migrate command-local flags to `FlagDefinition` for consistency and collision detection

## Summary

Three command-root files (`cmd/weaver/commands/block/node/node.go`,
`cmd/weaver/commands/alloy/cluster/cluster.go`,
`cmd/weaver/commands/teleport/cluster/cluster.go`) registered their persistent
flags directly with Cobra/pflag (`PersistentFlags().StringVar(...)`,
`BoolVar(...)`, `StringArrayVar(...)`) instead of using the repository's
`FlagDefinition[T]` helper. They are now migrated to the helper.

To support the migration, `cmd/weaver/commands/common/flags.go` gains:

- `SetVarPHidden(...)` on `FlagDefinition[T]` (one-shot register + mark hidden) —
  needed for the deprecated `--cluster-secret-store` flag.
- A new `CommaSplitStringsFlagDefinition` type (backed by pflag's
  `StringSliceVarP`/`GetStringSlice`) replacing the previous (untested in
  production) `case []string:` branch in `FlagDefinition[T]`'s switch.
- A new `RepeatableStringFlagDefinition` type (backed by pflag's
  `StringArrayVarP`/`GetStringArray`), used for `--add-prometheus-remote` and
  `--add-loki-remote` so that values containing commas (e.g.
  `name=alpha,url=https://a.example.com`) survive across repeated occurrences
  rather than being shredded into sub-tokens.

This is a **registration refactor** — flag names, descriptions, defaults, and
hidden status are preserved exactly. Read sites (`install.go`, `upgrade.go`,
etc.) continue to consume the same package-level `flagXxx` variables, so they
do not change.

`cmd/weaver/commands/common/common.go` is renamed to
`cmd/weaver/commands/common/flags_common.go` for symmetry with the new
`flags_blocknode.go` / `flags_alloy.go` / `flags_teleport.go` siblings —
purely cosmetic.

## Changed files

| File | Description |
|------|-------------|
| `cmd/weaver/commands/common/flags.go` | Remove unused `case []string:` from `FlagDefinition[T].valueFrom` and `setFlagVar`. Add `SetVarPHidden` on `FlagDefinition[T]`. Add `CommaSplitStringsFlagDefinition` and `RepeatableStringFlagDefinition` types with the full surface (`Clone`, `Value`, `ValueLocal`, `ValueOwnPersistent`, `SetVar`, `SetVarP`, `SetVarPHidden`, `MarkRequired`, `MarkRequiredP`). |
| `cmd/weaver/commands/common/common.go` → `flags_common.go` | File rename only — no content change. Aligns the existing root-level flag factories with the per-command flag-factory files that share the `flags_*.go` naming pattern. |
| `cmd/weaver/commands/common/flags_blocknode.go` (new) | 19 factories for `block node` persistent flags: `FlagChartRepo`, `FlagNamespace`, `FlagReleaseName`, `FlagBasePath`, `FlagArchivePath`, `FlagLivePath`, `FlagLogPath`, `FlagVerificationPath`, `FlagPluginsPath`, `FlagLiveSize`, `FlagArchiveSize`, `FlagLogSize`, `FlagVerificationSize`, `FlagPluginsSize`, `FlagHistoricRetention`, `FlagRecentRetention`, `FlagPluginPreset`, `FlagPlugins`, `FlagLoadBalancerEnabled`. |
| `cmd/weaver/commands/common/flags_alloy.go` (new) | 9 factories for `alloy cluster` persistent flags: `FlagClusterName`, `FlagMonitorBlockNode`, `FlagAlloyClusterSecretStore`, `FlagPrometheusRemotes` (`RepeatableStringFlagDefinition`), `FlagLokiRemotes` (`RepeatableStringFlagDefinition`), `FlagPrometheusURL`, `FlagPrometheusUsername`, `FlagLokiURL`, `FlagLokiUsername`. |
| `cmd/weaver/commands/common/flags_teleport.go` (new) | 2 factories for `teleport cluster` persistent flags: `FlagTeleportVersion`, `FlagTeleportValuesFile`. The "Teleport" prefix disambiguates `--version` from the root `--version` factory (which is a bool); the `Teleport` prefix on `FlagTeleportValuesFile` distinguishes it from `common.FlagValuesFile` which has shorthand `-f`. |
| `cmd/weaver/commands/common/flags_test.go` | Converts the existing `FlagDefinition[[]string]` test to `CommaSplitStringsFlagDefinition`. Adds a `RepeatableStringFlagDefinition` test that asserts comma-bearing values survive across two `--flag` occurrences. Adds two `SetVarPHidden` tests (one for `FlagDefinition[T]`, one for `RepeatableStringFlagDefinition`) verifying the registered flag has `Hidden == true`. |
| `cmd/weaver/commands/block/node/node.go` | Replaces 19 direct `nodeCmd.PersistentFlags().XxxVar(...)` calls with `common.FlagXxx().SetVarP(nodeCmd, &flagXxx, false)`. Package-level `flagXxx` `var`s and read sites are untouched. |
| `cmd/weaver/commands/alloy/cluster/cluster.go` | Replaces 9 direct registration calls with factory-based registration. `--cluster-secret-store` now uses `SetVarPHidden`. `--add-prometheus-remote` and `--add-loki-remote` use `RepeatableStringFlagDefinition`. |
| `cmd/weaver/commands/teleport/cluster/cluster.go` | Replaces 2 direct registration calls with factory-based registration. Drops now-unused `"fmt"` and `pkg/deps` imports (the description is built inside the factory). |
| `docs/claude/plans/00392-migrate-command-local-flags-to-flagdefinition.md` (new) | Plan document drafted before implementation per repo convention. |

## Code review checklist

### Behavior preservation (the single most important thing)

- [ ] Every migrated flag preserves its **name** exactly: `--chart-repo`, `--namespace`, `--release-name`, `--base-path`, `--archive-path`, `--live-path`, `--log-path`, `--verification-path`, `--plugins-path`, `--live-size`, `--archive-size`, `--log-size`, `--verification-size`, `--plugins-size`, `--historic-retention`, `--recent-retention`, `--plugin-preset`, `--plugins`, `--load-balancer-enabled`, `--cluster-name`, `--monitor-block-node`, `--cluster-secret-store`, `--add-prometheus-remote`, `--add-loki-remote`, `--prometheus-url`, `--prometheus-username`, `--loki-url`, `--loki-username`, `--version` (teleport-scoped), `--values` (teleport-scoped).
- [ ] Every migrated flag preserves its **description** byte-for-byte (diff `--help` output against `main`; see UAT step 2).
- [ ] Every migrated flag preserves its **default value**:
  - [ ] All `string` flags default to `""` (empty string).
  - [ ] `--load-balancer-enabled` defaults to **`true`** (this is the only non-zero default among the migrated flags).
  - [ ] `--monitor-block-node` defaults to `false`.
  - [ ] `--cluster-secret-store` defaults to `"vault-secret-store"`.
  - [ ] `--add-prometheus-remote` / `--add-loki-remote` default to `nil` (empty array).
- [ ] `--cluster-secret-store` remains **hidden** in `--help` output (uses `SetVarPHidden`).
- [ ] No flag gains or loses a shorthand letter — none of the migrated flags have a shorthand, and the factories all set `ShortName: ""`.

### Slice vs. array semantics (the only correctness risk)

- [ ] `FlagPrometheusRemotes()` returns `RepeatableStringFlagDefinition` (not `CommaSplitStringsFlagDefinition`). Same for `FlagLokiRemotes()`.
- [ ] The new `RepeatableStringFlagDefinition.setFlagVar` calls `flags.StringArrayVarP(...)`, **not** `StringSliceVarP`. Same for the `valueFrom` reader (`GetStringArray`, not `GetStringSlice`).
- [ ] `runRepeatableStringFlagTest` in `flags_test.go` asserts the value `"name=alpha,url=https://a.example.com"` is preserved as one element after two `--flag` occurrences. This is the regression test for #392's primary correctness concern.

### `FlagDefinition[T]` minor changes

- [ ] `case []string:` is removed from both the `valueFrom` and `setFlagVar` switches in `flags.go`. No production caller exists for `FlagDefinition[[]string]` post-PR (`grep -rn 'FlagDefinition\[\[\]string\]' cmd internal pkg` returns nothing).
- [ ] `SetVarPHidden` registers the flag with `SetVarP` first, then calls `cmd.PersistentFlags().MarkHidden(fp.Name)` — ordering matters; `MarkHidden` errors if the flag does not yet exist.
- [ ] The two new types (`CommaSplitStringsFlagDefinition`, `RepeatableStringFlagDefinition`) each expose the same method surface as `FlagDefinition[T]`: `Clone`, `Value`, `ValueLocal`, `ValueOwnPersistent`, `SetVar`, `SetVarP`, `SetVarPHidden`, `MarkRequired`, `MarkRequiredP`, and the unexported `varP`/`varNP`/`setFlagVar`/`valueFrom` used by tests.

### Read sites

- [ ] `cmd/weaver/commands/block/node/init.go`, `install.go`, `upgrade.go`, `reconfigure.go`, `reset.go`, `uninstall.go`, `check.go` continue to read the package-level `flagXxx` variables (e.g. `flagChartRepo`, `flagBasePath`, `flagLoadBalancerEnabled`) — `SetVarP` binds the same pointer, so no signature change cascades through.
- [ ] `cmd/weaver/commands/alloy/cluster/install.go` continues to read `flagClusterName`, `flagPrometheusRemotes`, `flagLokiRemotes`, etc.
- [ ] `cmd/weaver/commands/teleport/cluster/install.go` continues to read `flagVersion` and `flagValuesFile`.

### Out of scope (verify nothing else changed)

- [ ] No non-persistent (subcommand-local) flag registrations on `installCmd`, `upgradeCmd`, `reconfigureCmd`, `resetCmd`, `uninstallCmd`, `checkCmd`, etc. are touched. (`flagChartVersion` in `install.go:57` and `upgrade.go:60` remains a direct `installCmd.Flags().StringVar(...)` call — it is on a subcommand, not a command root, and is outside this PR's scope.)
- [ ] No flag is renamed or its default changed.
- [ ] No new flag is introduced.

## Tests

Targeted unit tests for the helper extensions (run inside the UTM VM — these
files transitively import `internal/mount` via `internal/workflows`, which is
Linux-only):

```bash
task vm:test:unit
```

To exercise just the new flag types and the hidden-flag helper:

```bash
task vm:test:unit -- -run '^TestFlags_PersistentAndNonPersistent$|^TestSetVarPHidden' ./cmd/weaver/commands/common/...
```

Full unit suite in the VM:

```bash
task vm:test:unit
```

Full integration suite in the VM:

```bash
task vm:test:integration
```

The integration suite is not strictly required to validate this PR — it is a
registration refactor with no runtime semantic change — but running it
defensively catches any unanticipated cobra-flag-parsing fallout. Pay particular
attention to:

```bash
task vm:test:integration TEST_NAME='^TestReconfigure_ChartVersionFlagNotAccepted$'
```

This existing test verifies `reconfigure` rejects `--chart-version` (because
the flag is registered on `installCmd` / `upgradeCmd` only, not on `nodeCmd`).
It should continue to pass — the PR does not touch subcommand-local
registrations.

## Manual UAT

These steps verify that the user-facing CLI surface is byte-identical before
and after the PR.

1. **Build the Linux binary on the host**:

   ```bash
   cd pkg/version && bash generate_version_unix.sh && cd ../..
   task build:weaver GOOS=linux GOARCH=arm64
   ```

   Expect: `bin/solo-provisioner-linux-arm64` is produced without errors.

2. **Diff `--help` output against `main`** for the three affected command
   trees. Inside the UTM VM (or on any Linux host with the freshly built
   binary):

   ```bash
   # Capture this PR's help output
   ./bin/solo-provisioner-linux-arm64 block node --help > /tmp/help-pr-blocknode.txt
   ./bin/solo-provisioner-linux-arm64 alloy cluster --help > /tmp/help-pr-alloy.txt
   ./bin/solo-provisioner-linux-arm64 teleport cluster --help > /tmp/help-pr-teleport.txt

   # Then switch to main, rebuild, capture the same output, and diff
   git stash
   git checkout main
   cd pkg/version && bash generate_version_unix.sh && cd ../..
   task build:weaver GOOS=linux GOARCH=arm64
   ./bin/solo-provisioner-linux-arm64 block node --help > /tmp/help-main-blocknode.txt
   ./bin/solo-provisioner-linux-arm64 alloy cluster --help > /tmp/help-main-alloy.txt
   ./bin/solo-provisioner-linux-arm64 teleport cluster --help > /tmp/help-main-teleport.txt
   git checkout 00392-migrate-command-local-flags-to-flagdefinition
   git stash pop

   diff /tmp/help-main-blocknode.txt /tmp/help-pr-blocknode.txt
   diff /tmp/help-main-alloy.txt /tmp/help-pr-alloy.txt
   diff /tmp/help-main-teleport.txt /tmp/help-pr-teleport.txt
   ```

   Expected: **all three diffs produce zero output**. Any non-empty diff
   signals an unintended UX change (drifted description, lost shorthand,
   default changed, hidden flag now visible, etc.).

3. **Verify `--cluster-secret-store` remains hidden**:

   ```bash
   ./bin/solo-provisioner-linux-arm64 alloy cluster --help | grep -- 'cluster-secret-store' || echo 'OK: hidden'
   ./bin/solo-provisioner-linux-arm64 alloy cluster install --cluster-secret-store=my-store --help
   ```

   Expected: the first command prints `OK: hidden` (flag does not appear in
   `--help`). The second command's invocation must still parse the flag
   without error, confirming the flag is registered but hidden.

4. **Verify `--add-prometheus-remote` preserves commas inside one value**:

   ```bash
   ./bin/solo-provisioner-linux-arm64 alloy cluster install \
     --cluster-name=test \
     --add-prometheus-remote='name=foo,url=https://prom-a.example.com,username=u1' \
     --add-prometheus-remote='name=bar,url=https://prom-b.example.com,username=u2' \
     --help 2>&1 | head -1
   ```

   This invocation should not error on flag parsing. (The `--help` short-circuit
   prevents an actual install.) The functional behavior — two remotes parsed
   from two repeated flags, each with a comma-bearing value — is covered by
   `runRepeatableStringFlagTest` in `flags_test.go` and by the existing
   `parse_remote_test.go` once a real install runs.

5. **Verify `--load-balancer-enabled` default is `true`**:

   ```bash
   ./bin/solo-provisioner-linux-arm64 block node --help | grep -- 'load-balancer-enabled'
   ```

   Expected: the line contains `(default true)`.

6. **Sanity-check the FlagDefinition test additions**:

   ```bash
   task vm:test:unit -- -v -run '^TestFlags_PersistentAndNonPersistent$|^TestSetVarPHidden' ./cmd/weaver/commands/common/...
   ```

   Expected output contains:

   ```
   --- PASS: TestFlags_PersistentAndNonPersistent (...)
       --- PASS: TestFlags_PersistentAndNonPersistent/persistent/comma-split-strings (...)
       --- PASS: TestFlags_PersistentAndNonPersistent/persistent/repeatable-string (...)
       --- PASS: TestFlags_PersistentAndNonPersistent/non-persistent/comma-split-strings (...)
       --- PASS: TestFlags_PersistentAndNonPersistent/non-persistent/repeatable-string (...)
   --- PASS: TestSetVarPHidden_MarksFlagHidden (...)
   --- PASS: TestSetVarPHidden_RepeatableStringMarksFlagHidden (...)
   ```

## Risks and rollback

- **Pure refactor**: no schema changes, no state migrations, no on-disk
  artifacts changed. Revert the PR and the previous registration pattern is
  restored with zero migration work.
- **One correctness footgun**: if a future contributor accidentally uses
  `CommaSplitStringsFlagDefinition` for `--add-prometheus-remote` or
  `--add-loki-remote`, comma-bearing spec values will be shredded. The
  `runRepeatableStringFlagTest` regression test in `flags_test.go` is the
  guard.
