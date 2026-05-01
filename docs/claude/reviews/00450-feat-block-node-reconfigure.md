# Review Guide — 00450: Block Node Reconfigure

## Summary

**Problem:** There was no way to re-apply Helm chart configuration to a deployed block node without also
changing the chart version. Additionally, `reconfigure --with-reset` never deleted/recreated PVs/PVCs,
so new storage paths were silently ignored (old PVs/PVCs remained bound to old `hostPath` values).
Default `reconfigure` with changed storage paths was ambiguous and could leave the cluster in an
inconsistent state. Interactive prompts for storage paths were flat (all fields shown at once) with no
guidance on the two mutually exclusive modes (single base path vs individual paths).

**Solution:**
1. Implement `solo-provisioner block node reconfigure` sub-command backed by `ReconfigureHandler` — re-applies
   values at the currently-deployed chart version with no semver comparison.
2. Fix `--with-reset`: after purging data, delete all existing PVs/PVCs by name, create directories
   at the new paths, and create fresh PVs/PVCs before running `helm upgrade`.
3. Guard default/no-restart reconfigure: if storage paths have changed, return a clear error pointing
   to `--with-reset`.
4. Refactor storage path prompts into a two-pass `RunStoragePathPrompts` function: pass 1 asks the
   operator to choose a mode (single base path vs individual paths); pass 2 shows only the relevant
   inputs, with required-field validation enforced in individual mode. Applied to install, upgrade,
   and reconfigure commands.
5. Convert `RunWorkflow` / `RunWorkflowBuilder` to return error instead of calling `os.Exit`, so all
   CLI commands propagate workflow failures uniformly via cobra `RunE` and the existing top-level
   `doctor.CheckErr` in `main.go`.

---

## Changed Files

| File | Description |
|------|-------------|
| `pkg/models/intent.go` | Added `ActionReconfigure` constant wired into `allowedOperations` for `TargetBlockNode` |
| `pkg/models/inputs.go` | Added `NoRestart bool` to `BlockNodeInputs` |
| `pkg/models/config.go` | `BlockNodeStorage.MergeFrom` now respects "base-path mode": when the receiver already carries a `BasePath`, lower-priority sources cannot fill in individual `*Path` fields (those are derived from `BasePath` at resolution time). `BasePath` and size fields are still merged unconditionally |
| `internal/bll/blocknode/upgrade_handler.go` | Added `ErrPropertyResolution` hint to the downgrade error pointing to `reconfigure` |
| `internal/bll/blocknode/reconfigure_handler.go` | **NEW** — `ReconfigureHandler`: three-branch workflow (with-reset / default / no-restart); storage-path-changed guard blocks non-reset reconfigure when paths differ |
| `internal/bll/blocknode/handler.go` | Added `reconfigure` field, instantiation, and `ForAction` routing |
| `internal/bll/blocknode/{install,reset,uninstall}_handler.go` | Updated to call the renamed `patchBlockNodeState` (was `patchBlockNodeChartRef`) — no behavioral change for these handlers |
| `internal/bll/blocknode/reconfigure_handler_test.go` | **NEW** — unit tests covering `ReconfigureHandler`'s three branches and the storage-paths-changed guard |
| `internal/bll/blocknode/helpers.go` | Renamed `patchBlockNodeChartRef` to `patchBlockNodeState` and extended it to also persist `Storage.BasePath`. Added `storagePathsChanged` helper that resolves both sides through `bnpkg.ResolveStoragePaths` (no `Manager` allocation). Added `NoRestart` to the pass-through fields in `resolveBlocknodeEffectiveInputs` so `--no-restart` reaches `BuildWorkflow` |
| `internal/blocknode/storage.go` | Replaced manifest-based `DeletePersistentVolumes` with name-based `DeleteAllPersistentVolumes` (no temp-file dependency). Extracted the path-derivation body of `Manager.GetStoragePaths` into a free `ResolveStoragePaths(storage, chartVersion)` so callers (e.g. `storagePathsChanged`) can canonicalize/compare paths without constructing fsx/helm/kube clients. Optional-storage handling in `CreatePersistentVolumes` now reads `path` and `size` per registered storage entry |
| `internal/workflows/blocknode.go` | Removed the now-unused `NewBlockNodeInstallWorkflow` / `NewBlockNodeUpgradeWorkflow` / `NewBlockNodeResetWorkflow` helpers — handlers build their own workflows directly via the step builders below |
| `internal/workflows/steps/step_block_node.go` | Added new step IDs (`DeleteBlockNodePVsStepId`, `RecreateBlockNodeStorageStepId`, `RolloutRestartBlockNodeStepId`) and their builders (`deleteBlockNodePVs`, `RecreateBlockNodeStorage`, `RolloutRestartBlockNode`). Extracted `restartBlockNodeSteps` helper (mirrors the existing `purgeBlockNodeStorageSteps` pattern) used by `RolloutRestartBlockNode`. `createBlockNodePVs` rollback now calls `DeleteAllPersistentVolumes` |
| `cmd/weaver/commands/common/common.go` | Added `FlagNoRestart()` factory |
| `cmd/weaver/commands/common/run.go` | Reworked `RunWorkflow` / `RunWorkflowBuilder` to return error; replaced the `os.Exit`-based `handleWorkflowResult` with `finalizeWorkflowReport`, which still saves the YAML report and prints the summary table but returns the first failed step's error (preserving its `errorx` properties so `doctor.CheckErr` in `main.go` still renders the resolution text) |
| `cmd/weaver/commands/block/node/reconfigure.go` | **NEW** — Cobra `reconfigure` sub-command; registers `--no-restart`, `--with-reset`, `--values`, `--no-reuse-values` |
| `cmd/weaver/commands/block/node/init.go` | Propagates `flagNoRestart`; routes `reconfigure` to trimmed input prompts; calls `RunStoragePathPrompts` for all storage-modifying commands |
| `cmd/weaver/commands/block/node/node.go` | Registered `reconfigureCmd`; hides inherited `--chart-version` flag from `reconfigure` |
| `internal/state/state_reader.go` | Extended `PromptDefaultsDoc` and `BlockNodeSummary` with storage path fields |
| `internal/state/state_reader_test.go` | Updated YAML fixtures and assertions to cover new storage path fields |
| `internal/ui/prompt/blocknode.go` | Major refactor: private per-field builders; `BlockNodeReconfigureInputPrompts` now only returns retention prompts; added `StoragePathTargets` struct, `storagePathModeBasePath`/`storagePathModeIndividual` constants, `validateRequiredPath`, `RunStoragePathPrompts` (two-pass mode select + conditional path inputs); individual path builders accept `required bool`; state values used directly for pre-filling with base-path-derived compile-time defaults as last resort |
| `internal/ui/prompt/prompt_test.go` | Added tests for `RunStoragePathPrompts` skip-on-flag-set, `BlockNodeReconfigureInputPrompts` retention-only, `validateRequiredPath`, and `archivePathInputPrompt` required-mode validation |
| `internal/ui/prompt/prompt.go` | `RunInputPrompts` now skips a prompt when its `FlagName` is registered on neither the local nor inherited flag set — lets `reconfigure` reuse the shared prompt infrastructure without trying to write through to flags it doesn't expose |
| `internal/rsl/block_node_runtime.go` | Documentation update on the `storageResolver` doc comment to reflect "base-path mode" merge semantics |
| `internal/rsl/block_node_runtime_test.go` | **NEW** — table-driven tests for the `storageResolver`, exercising base-path mode and individual-paths mode merge precedence |
| `cmd/weaver/commands/block/node/reconfigure_it_test.go` | **NEW** — integration tests for the `reconfigure` Cobra command (covers all three branches and the storage-paths-changed guard) |
| `cmd/weaver/commands/block/node/install_it_test.go` | One-line update to align with the new prompt skip-when-flag-not-registered behavior |
| `cmd/weaver/commands/{alloy,kube,teleport,self_install,demo,block/node/check}.go` (and siblings) | Mechanical update for the `RunWorkflow` signature change: every `RunE` now propagates the returned error via `if err := …; err != nil { return err }` (or `return …` for the trailing call). Touches `alloy/cluster/{install,uninstall}.go`, `kube/cluster/{install,uninstall}.go`, `teleport/node/{install,uninstall}.go`, `teleport/cluster/{install,uninstall}.go`, `block/node/{check,install,upgrade,reset,uninstall}.go`, `self_install.go`, `demo.go`. No behavioral change at the binary boundary — failures still terminate via `main.go`'s top-level `doctor.CheckErr` |

---

## Code Review Checklist

- [ ] `ActionReconfigure` appears in `allowedOperations` only for `TargetBlockNode`
- [ ] `ReconfigureHandler.BuildWorkflow` does **not** call `semver.NewVersion` — no version comparison
- [ ] `ReconfigureHandler.BuildWorkflow` checks `release.StatusDeployed` before proceeding (unless `--force`)
- [ ] `ReconfigureHandler.BuildWorkflow` calls `bnpkg.ValidateStorageCompleteness`
- [ ] `--with-reset` branch: `PurgeBlockNodeStorage` → `RecreateBlockNodeStorage` → `UpgradeBlockNode`
- [ ] `RecreateBlockNodeStorage` sequences: `deleteBlockNodePVs` → `setupBlockNodeStorage` → `createBlockNodePVs`
- [ ] `deleteBlockNodePVs` calls `DeleteAllPersistentVolumes` (by name, no temp-file dependency)
- [ ] Default branch: `UpgradeBlockNode` → `RolloutRestartBlockNode`
- [ ] `--no-restart` branch: `UpgradeBlockNode` only
- [ ] Default/no-restart branch: `storagePathsChanged` guard returns `errorx.IllegalArgument` with `--with-reset` hint when paths differ
- [ ] `storagePathsChanged` resolves both sides through `ResolveStoragePaths` — no `Manager`, fsx, helm, or kube client constructed for path comparison
- [ ] Workflow IDs are `"block-node-reconfigure"`, `"block-node-reconfigure-with-reset"`, `"block-node-reconfigure-no-restart"`
- [ ] `reconfigure` command does **not** expose `--chart-version` to end users (flag is hidden)
- [ ] `RunWorkflow` and `RunWorkflowBuilder` return error; every cobra `RunE` handler propagates via return (no `os.Exit`-bound paths in the workflow runner)
- [ ] `BlockNodeReconfigureInputPrompts` returns **only** `historic-retention` and `recent-retention` — no storage path fields
- [ ] `reconfigure` interactive prompts do **not** show `namespace`, `release-name`, or `chart-version`
- [ ] `RunStoragePathPrompts` is called for install, upgrade, **and** reconfigure commands
- [ ] `RunStoragePathPrompts` returns immediately (no-op) when any storage flag is already set on the CLI
- [ ] Pass 1 of `RunStoragePathPrompts` pre-selects the mode that matches the on-disk state (`BasePath != ""` → base-path mode, otherwise individual)
- [ ] Pass 2 (base-path mode) shows only `--base-path`; effective value resolved from state → config → `deps.BLOCK_NODE_STORAGE_BASE_PATH`
- [ ] Pass 2 (individual mode) shows five path inputs; effective value resolved from state (direct, no suppression) → config → base-path-derived default (`<effectiveBase>/<subdir>`)
- [ ] Individual path inputs use `validateRequiredPath` (empty value rejected); base-path input uses `validateOptionalPath`
- [ ] Subdirectory names in `indivDefault` match `GetStoragePaths`: `archive`, `live`, `logs`, `verification`, `plugins`
- [ ] `UpgradeHandler` downgrade error includes resolution hint mentioning `reconfigure`
- [ ] `ResolveStoragePaths` in `internal/blocknode/storage.go` is a free function that takes `(BlockNodeStorage, chartVersion)` — no `Manager` dependency
- [ ] `Manager.GetStoragePaths` is a one-line wrapper over `ResolveStoragePaths`
- [ ] `RolloutRestartBlockNode` composes `restartBlockNodeSteps()` — same pattern as `PurgeBlockNodeStorage` / `ResetBlockNode` using `purgeBlockNodeStorageSteps()`
- [ ] `NoRestart` is in the pass-through field list of `resolveBlocknodeEffectiveInputs` (alongside `ResetStorage`, `ReuseValues`, etc.) — verifies that `--no-restart` reaches `ReconfigureHandler.BuildWorkflow`
- [ ] `finalizeWorkflowReport` returns the first failed step's error (carries `errorx` properties) — verifies that `ErrPropertyResolution` is still rendered by `main.go`'s `doctor.CheckErr` after the `os.Exit` removal
- [ ] `BlockNodeStorage.MergeFrom` does not fill individual `*Path` fields when the receiver already has `BasePath` set — guarded by a snapshot taken before any mutation
- [ ] `RunInputPrompts` skips a prompt whose `FlagName` is not registered on the command (lookup against both local and inherited flag sets)
- [ ] All new files have SPDX license header

---

## Unit & Integration Tests

```bash
# Unit tests for state reader (covers new storage path fields)
go test -tags='!integration' -count=1 ./internal/state/...

# Unit tests for prompt builders (covers RunStoragePathPrompts, validateRequiredPath, etc.)
go test -tags='!integration' -count=1 ./internal/ui/prompt/...

# Unit tests for models (includes intent validation)
go test -tags='!integration' -count=1 ./pkg/models/...

# Full unit suite (run in VM for Linux-only packages)
task vm:test:unit

# Reconfigure integration tests (run in VM)
task vm:test:integration TEST_NAME='^TestReconfigure_'

# Block node step integration tests (run in VM)
task vm:test:integration TEST_NAME='^Test_StepBlockNode.*'
```

---

## Manual UAT

### Prerequisites
- A running Kubernetes cluster with a deployed block node (`solo-provisioner block node install` completed)

### Steps

1. **Verify the command is registered:**
   ```bash
   solo-provisioner block node --help
   # Expected: reconfigure appears in the list of sub-commands
   ```

2. **Verify --chart-version flag is hidden:**
   ```bash
   solo-provisioner block node reconfigure --help
   # Expected: --chart-version NOT listed; --no-restart and --with-reset ARE listed
   ```

3. **Interactive install — storage path mode select appears:**
   ```bash
   solo-provisioner block node install
   # Expected: after namespace/release/chart-version prompts, a mode select appears:
   #   > Single base path  (subdirectories are created automatically)
   #     Individual paths  (archive, live, log, verification, plugins must all be provided)
   # Selecting "Single base path" shows only the --base-path input, pre-filled with /mnt/fast-storage
   # Selecting "Individual paths" shows five path inputs, each pre-filled with /mnt/fast-storage/<subdir>
   ```

4. **Individual paths mode — empty value rejected:**
   ```bash
   # In the individual paths mode prompt, clear the archive-path field and press Enter
   # Expected: inline validation error "path cannot be empty in individual paths mode"
   #           prompt does not advance until a non-empty path is provided
   ```

5. **Interactive reconfigure — mode pre-selected from state:**
   ```bash
   solo-provisioner block node reconfigure
   # (node was installed with --base-path /mnt/fast-storage)
   # Expected: mode select pre-selects "Single base path"
   #           --base-path input pre-filled with /mnt/fast-storage from state
   # (node was installed with individual paths)
   # Expected: mode select pre-selects "Individual paths"
   #           each path input pre-filled with the value stored in state
   ```

6. **CLI flags bypass prompts entirely:**
   ```bash
   solo-provisioner block node reconfigure --base-path /mnt/fast-storage
   # Expected: no storage path prompts shown at all (flag already set)
   solo-provisioner block node install --archive-path /data/archive ...
   # Expected: no storage path prompts shown at all
   ```

7. **Default reconfigure (same paths) — succeeds with rollout-restart:**
   ```bash
   solo-provisioner block node reconfigure --profile testnet --force
   # Expected: workflow "block-node-reconfigure" runs
   #   Steps: upgrade-block-node -> rollout-restart-block-node -> wait-for-block-node
   ```

8. **Default reconfigure with changed paths — blocked with clear error:**
   ```bash
   solo-provisioner block node reconfigure --profile testnet --base-path /mnt/new-storage --force
   # Expected: error "storage paths have changed; PVs/PVCs cannot be updated without clearing existing data"
   #           hint: "re-run with --with-reset to delete existing PVs/PVCs and recreate them at the new paths"
   #           exit code non-zero
   ```

9. **Reconfigure --with-reset with new paths — PVs/PVCs recreated:**
   ```bash
   solo-provisioner block node reconfigure --profile testnet --base-path /mnt/new-storage --with-reset --force
   # Expected: workflow "block-node-reconfigure-with-reset" runs
   #   Steps: purge-block-node-storage -> recreate-block-node-storage -> upgrade-block-node
   kubectl get pv live-storage-pv -o jsonpath='{.spec.hostPath.path}'
   # Expected: /mnt/new-storage/live
   kubectl get pvc -n <namespace>
   # Expected: all PVCs show Bound status
   ls /mnt/new-storage/
   # Expected: archive/ live/ logs/ (and verification/ plugins/ if applicable)
   ```

10. **Verify chart version unchanged after reconfigure:**
    ```bash
    helm list -n <namespace>
    # Expected: CHART column shows same version as before reconfigure
    ```

11. **Reconfigure with --no-restart:**
    ```bash
    solo-provisioner block node reconfigure --profile testnet --no-restart --force
    # Expected: workflow "block-node-reconfigure-no-restart" runs
    #   Steps: upgrade-block-node only (no rollout-restart)
    ```

12. **Block node not installed returns a clear error:**
    ```bash
    solo-provisioner block node reconfigure --profile testnet --force
    # (on a machine with no deployed block node)
    # Expected: error "block node is not installed; cannot reconfigure"
    #           exit code non-zero
    ```

13. **Verify downgrade error hint on upgrade:**
    ```bash
    solo-provisioner block node upgrade --profile testnet --chart-version 0.0.1
    # Expected: error message mentions "use 'weaver block node reconfigure'..."
    ```

