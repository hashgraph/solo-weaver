# Review Guide — 00450: Block Node Reconfigure

## Summary

**Problem:** There was no way to re-apply Helm chart configuration to a deployed block node without also changing the chart version. The downgrade guard in `UpgradeHandler` lacked a remediation hint. `helm upgrade` does not restart a pod when only ConfigMap data changes, so reconfigured values could be silently ignored.

**Solution:** Implement a new `weaver block node reconfigure` sub-command backed by `ReconfigureHandler`. It re-uses the existing `UpgradeBlockNode` step (and optionally `PurgeBlockNodeStorage`) but skips all semver comparisons, always locking to the currently-deployed chart version. By default a `RolloutRestartBlockNode` step follows the upgrade to guarantee the pod picks up ConfigMap-only changes; opt-out with `--no-restart`. Interactive prompts for `reconfigure` are tailored to omit immutable fields (namespace, release-name, chart-version) and instead surface storage path configuration.

---

## Changed Files

| File | Description |
|------|-------------|
| `pkg/models/intent.go` | Added `ActionReconfigure` constant wired into `allowedOperations` for `TargetBlockNode` |
| `pkg/models/inputs.go` | Added `NoRestart bool` to `BlockNodeInputs` |
| `internal/bll/blocknode/upgrade_handler.go` | Added `ErrPropertyResolution` hint to the downgrade error pointing to `reconfigure` |
| `internal/bll/blocknode/reconfigure_handler.go` | **NEW** — `ReconfigureHandler` with three-branch `BuildWorkflow` (with-reset / default / no-restart) |
| `internal/bll/blocknode/handler.go` | Added `reconfigure` field, instantiation, and `ForAction` routing |
| `internal/workflows/blocknode.go` | Added `NewBlockNodeReconfigureWorkflow` helper |
| `internal/workflows/steps/step_block_node.go` | Added `RolloutRestartBlockNodeStepId` and `RolloutRestartBlockNode` step |
| `cmd/weaver/commands/common/common.go` | Added `FlagNoRestart()` factory |
| `cmd/weaver/commands/common/run.go` | Added `RunWorkflowE` — like `RunWorkflow` but returns errors instead of calling `os.Exit`, enabling testable `RunE` handlers |
| `cmd/weaver/commands/block/node/reconfigure.go` | **NEW** — Cobra `reconfigure` sub-command using `RunWorkflowE`; registers `--no-restart`, `--with-reset`, `--values`, `--no-reuse-values` |
| `cmd/weaver/commands/block/node/init.go` | Propagates `flagNoRestart` to `inputs.Custom.NoRestart`; branches `promptForMissingFlags` to use `BlockNodeReconfigureInputPrompts` for the `reconfigure` command |
| `cmd/weaver/commands/block/node/node.go` | Registered `reconfigureCmd`; hides inherited `--chart-version` flag from `reconfigure` |
| `internal/state/state_reader.go` | Extended `PromptDefaultsDoc` and `BlockNodeSummary` to carry storage path fields read from the state file |
| `internal/state/state_reader_test.go` | Updated YAML fixtures and assertions to cover the new storage path fields |
| `internal/ui/prompt/blocknode.go` | Refactored into private per-field builder functions; added `BlockNodeReconfigureInputPrompts` with storage paths and retention thresholds; `BlockNodeInputPrompts` now delegates to the same builders (no duplication) |

---

## Code Review Checklist

- [ ] `ActionReconfigure` appears in `allowedOperations` only for `TargetBlockNode`
- [ ] `ReconfigureHandler.BuildWorkflow` does **not** call `semver.NewVersion` — no version comparison
- [ ] `ReconfigureHandler.BuildWorkflow` checks `release.StatusDeployed` before proceeding (unless `--force`)
- [ ] `ReconfigureHandler.BuildWorkflow` calls `bnpkg.ValidateStorageCompleteness`
- [ ] `--with-reset` branch: `PurgeBlockNodeStorage` + `UpgradeBlockNode` — **no** `RolloutRestartBlockNode` (restart implicit via scale-up)
- [ ] Default branch: `UpgradeBlockNode` + `RolloutRestartBlockNode`
- [ ] `--no-restart` branch: `UpgradeBlockNode` only
- [ ] Workflow IDs are `"block-node-reconfigure"`, `"block-node-reconfigure-with-reset"`, `"block-node-reconfigure-no-restart"`
- [ ] `RolloutRestartBlockNode` composes `scaleDownBlockNode` + `waitForBlockNodeTerminated` + `scaleUpBlockNode` + `waitForBlockNode`
- [ ] `reconfigure` command does **not** expose `--chart-version` to end users (flag is hidden)
- [ ] `RunWorkflowE` returns errors instead of calling `os.Exit` — verify `TestReconfigure_BlockNodeNotInstalled` passes
- [ ] `reconfigure` interactive prompts do **not** show `namespace`, `release-name`, or `chart-version`
- [ ] `reconfigure` interactive prompts show `base-path`, `archive-path`, `live-path`, `log-path`, `verification-path`, `plugins-path`, `historic-retention`, `recent-retention`
- [ ] Storage path prompt descriptions explain the base-path-or-all rule
- [ ] `BlockNodeInputPrompts` (install/upgrade) is unchanged in what it prompts — only refactored to use builders
- [ ] `UpgradeHandler` downgrade error includes resolution hint mentioning `reconfigure`
- [ ] All new files have SPDX license header

---

## Unit & Integration Tests

```bash
# Unit tests for state reader (covers new storage path fields)
go test -tags='!integration' -count=1 ./internal/state/...

# Unit tests for prompt builders
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

2. **Verify --chart-version flag is hidden and --no-restart is visible:**
   ```bash
   solo-provisioner block node reconfigure --help
   # Expected: --chart-version NOT listed; --no-restart IS listed
   # Expected: --base-path, --archive-path, --live-path, --log-path,
   #           --verification-path, --plugins-path are listed
   ```

3. **Interactive prompts show storage paths, not namespace/release-name:**
   ```bash
   solo-provisioner block node reconfigure --profile testnet
   # Expected (interactive TTY): prompts for base-path, archive-path, live-path,
   #   log-path, verification-path, plugins-path, historic-retention, recent-retention
   # NOT prompted: namespace, release-name, chart-version
   ```

4. **Reconfigure (default — with rollout-restart):**
   ```bash
   solo-provisioner block node reconfigure --profile testnet --force
   # Expected: workflow "block-node-reconfigure" runs
   #   Steps: upgrade-block-node -> rollout-restart-block-node -> wait-for-block-node
   # Verify restart annotation was set:
   kubectl get statefulset -n <namespace> -o jsonpath='{.spec.template.metadata.annotations}'
   # Expected: contains "kubectl.kubernetes.io/restartedAt"
   ```

5. **Verify chart version unchanged:**
   ```bash
   helm list -n <namespace>
   # Expected: CHART column shows same version as before reconfigure
   ```

6. **Reconfigure with --no-restart:**
   ```bash
   solo-provisioner block node reconfigure --profile testnet --no-restart --force
   # Expected: workflow "block-node-reconfigure-no-restart" runs
   #   Steps: upgrade-block-node -> wait-for-block-node (no rollout-restart)
   ```

7. **Reconfigure with storage reset:**
   ```bash
   solo-provisioner block node reconfigure --profile testnet --with-reset --force
   # Expected: workflow "block-node-reconfigure-with-reset" runs
   #   Steps: purge-block-node-storage -> upgrade-block-node (no rollout-restart)
   ```

8. **Block node not installed returns a clear error:**
   ```bash
   solo-provisioner block node reconfigure --profile testnet --force
   # (on a machine with no deployed block node)
   # Expected: error "block node is not installed; cannot reconfigure" printed to stderr
   #           exit code non-zero
   ```

9. **Verify downgrade error hint on upgrade:**
   ```bash
   solo-provisioner block node upgrade --profile testnet --chart-version 0.0.1
   # Expected: error message mentions "use 'weaver block node reconfigure'..."
   ```
