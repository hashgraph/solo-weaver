# Review Guide — 00450: Block Node Reconfigure

## Summary

**Problem:** There was no way to re-apply Helm chart configuration to a deployed block node without also changing the chart version. Additionally, the downgrade guard in `UpgradeHandler` lacked a clear remediation hint pointing users toward the new command.

**Solution:** Implement a new `weaver block node reconfigure` sub-command backed by `ReconfigureHandler`. It re-uses the existing `UpgradeBlockNode` step (and optionally `PurgeBlockNodeStorage`) but skips all semver comparisons, always locking to the currently-deployed chart version. The downgrade error in `UpgradeHandler` now includes a resolution hint pointing to `reconfigure`.

---

## Changed Files

| File | Description |
|------|-------------|
| `pkg/models/intent.go` | Added `ActionReconfigure` constant and wired it into `allowedOperations` for `TargetBlockNode` |
| `internal/bll/blocknode/upgrade_handler.go` | Added `ErrPropertyResolution` hint to the downgrade error |
| `internal/bll/blocknode/reconfigure_handler.go` | **NEW** — `ReconfigureHandler` implementing `IntentHandler[BlockNodeInputs]` |
| `internal/bll/blocknode/handler.go` | Added `reconfigure` field, instantiation, and `ForAction` routing |
| `internal/workflows/blocknode.go` | Added `NewBlockNodeReconfigureWorkflow` helper |
| `cmd/weaver/commands/block/node/reconfigure.go` | **NEW** — Cobra `reconfigure` sub-command |
| `cmd/weaver/commands/block/node/node.go` | Registered `reconfigureCmd`; hides inherited `--chart-version` flag |

---

## Code Review Checklist

- [ ] `ActionReconfigure` appears in `allowedOperations` only for `TargetBlockNode`
- [ ] `ReconfigureHandler.BuildWorkflow` does **not** call `semver.NewVersion` — no version comparison
- [ ] `ReconfigureHandler.BuildWorkflow` checks `release.StatusDeployed` before proceeding (unless `--force`)
- [ ] `ReconfigureHandler.BuildWorkflow` calls `bnpkg.ValidateStorageCompleteness`
- [ ] `ReconfigureHandler.BuildWorkflow` supports `--with-reset` (PurgeBlockNodeStorage + UpgradeBlockNode)
- [ ] Workflow IDs are `"block-node-reconfigure"` and `"block-node-reconfigure-with-reset"`
- [ ] `reconfigure` command does **not** expose `--chart-version` to end users (flag is hidden)
- [ ] `UpgradeHandler` downgrade error includes resolution hint mentioning `reconfigure`
- [ ] All new files have SPDX license header

---

## Unit & Integration Tests

```bash
# Unit tests for models (includes intent validation)
go test -tags='!integration' ./pkg/models/...

# Unit tests must be run in the VM (Linux-only mount package)
task vm:test:unit

# Single integration test (run in VM)
task vm:test:integration TEST_NAME='^Test_StepBlockNode.*'
```

---

## Manual UAT

### Prerequisites
- A running Kubernetes cluster with a deployed block node (`weaver block node install` completed)

### Steps

1. **Verify the command is registered:**
   ```bash
   solo-provisioner block node --help
   # Expected: reconfigure appears in the list of sub-commands
   ```

2. **Reconfigure without storage reset:**
   ```bash
   solo-provisioner block node reconfigure --profile testnet
   # Expected: workflow "block-node-reconfigure" runs, Helm upgrade applied at current chart version
   ```

3. **Verify chart version unchanged:**
   ```bash
   helm list -n <namespace>
   # Expected: CHART column shows same version as before reconfigure
   ```

4. **Reconfigure with storage reset:**
   ```bash
   solo-provisioner block node reconfigure --profile testnet --with-reset
   # Expected: workflow "block-node-reconfigure-with-reset" runs (purge + upgrade)
   ```

5. **Verify downgrade error hint on upgrade:**
   ```bash
   solo-provisioner block node upgrade --profile testnet --chart-version 0.0.1
   # Expected: error message mentions "use 'weaver block node reconfigure' to re-apply configuration..."
   ```

6. **Verify chart-version flag is hidden:**
   ```bash
   solo-provisioner block node reconfigure --help
   # Expected: --chart-version is NOT listed in the output
   ```

