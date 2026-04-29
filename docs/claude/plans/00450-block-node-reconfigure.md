# Block Node Reconfigure (GH #450)

## Context

`block node reconfigure` re-applies Helm chart configuration to a deployed block node **without changing the chart version**. It is the counterpart to `upgrade`: same Helm mechanism, same workflow steps, but `ChartVersion` is always locked to the currently-deployed value from state — no user-supplied version is accepted or needed.

A secondary goal from the issue is to add a clear resolution hint to the existing downgrade guard in `UpgradeHandler` (downgrades remain unconditionally blocked, no `--force` bypass).

## Architecture

The feature follows the identical handler pattern to every other block-node action:

```
cmd/weaver/commands/block/node/reconfigure.go   (new Cobra command)
    ↓
internal/bll/blocknode/reconfigure_handler.go   (new ReconfigureHandler)
    ↓
internal/workflows/steps/step_block_node.go     (reuses UpgradeBlockNode + optional PurgeBlockNodeStorage)
```

## Changes by layer (bottom-up)

### 1. Models — `pkg/models/intent.go`

Add new `ActionType` constant after `ActionUpgrade`:
```go
// ActionReconfigure re-applies configuration to an already-deployed component without changing its version.
ActionReconfigure ActionType = "reconfigure"
```

Add to `allowedOperations`:
```go
ActionReconfigure: {TargetBlockNode},
```

### 2. Upgrade handler improvement — `internal/bll/blocknode/upgrade_handler.go`

Add a resolution hint to the downgrade error. The current code (lines ~73–76):
```go
if desiredVer.LessThan(currentVer) {
    return nil, errorx.IllegalArgument.New(
        "block node chart version cannot be downgraded from %q to %q",
        currentVer, desiredVer)
}
```
Change to:
```go
if desiredVer.LessThan(currentVer) {
    return nil, errorx.IllegalArgument.New(
        "block node chart version cannot be downgraded from %q to %q",
        currentVer, desiredVer).
        WithProperty(models.ErrPropertyResolution,
            "version downgrade is not supported; use 'weaver block node reconfigure' to re-apply configuration at the current version")
}
```

### 3. BLL handler — `internal/bll/blocknode/reconfigure_handler.go` (NEW)

Model on `upgrade_handler.go`. Key differences:

- **Type comment**: "Unlike UpgradeHandler it does not perform any semver comparison — it always re-applies values at the currently-deployed chart version."
- **`PrepareEffectiveInputs`**: calls `resolveBlocknodeEffectiveInputs` unchanged. The runtime's `StrategyCurrent` will already prefer the deployed `ChartVersion` because reconfigure passes no user-supplied version.
- **`BuildWorkflow`**:
  - Requires `currentState.BlockNodeState.ReleaseInfo.Status == release.StatusDeployed` (or `--force`) with hint `"use 'weaver block node install' to install the block node first, or pass --force to continue"`
  - **No** `semver.NewVersion` calls — skip all version comparison
  - Calls `bnpkg.ValidateStorageCompleteness(inputs.Custom.Storage, inputs.Custom.ChartVersion)`
  - Supports `ins.ResetStorage` (same `PurgeBlockNodeStorage` + `UpgradeBlockNode` pattern as upgrade)
  - Workflow IDs: `"block-node-reconfigure"` or `"block-node-reconfigure-with-reset"`
- **`HandleIntent`**: `return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, patchBlockNodeChartRef())`
- **`NewReconfigureHandler(base, bnr)`**: same signature as `NewUpgradeHandler`

### 4. BLL router — `internal/bll/blocknode/handler.go`

Four changes:

1. Add field to `Handlers` struct: `reconfigure *ReconfigureHandler`
2. Instantiate in `NewHandlerFactory` after `upgradeHandler`:
```go
reconfigureHandler, err := NewReconfigureHandler(base, bnr)
if err != nil {
    return nil, errorx.IllegalArgument.New("failed to create ReconfigureHandler: %v", err)
}
```
3. Add to `h` struct literal: `reconfigure: reconfigureHandler`
4. Add case to `ForAction`:
```go
case models.ActionReconfigure:
    return h.reconfigure, nil
```

### 5. Workflow helper — `internal/workflows/blocknode.go`

Add after `NewBlockNodeUpgradeWorkflow` for symmetry:
```go
// NewBlockNodeReconfigureWorkflow creates a reconfigure workflow for a block node
// (same chart version, new values applied).
func NewBlockNodeReconfigureWorkflow(inputs models.BlockNodeInputs, withReset bool) *automa.WorkflowBuilder {
    if withReset {
        return automa.NewWorkflowBuilder().WithId("block-node-reconfigure-with-reset").Steps(
            steps.PurgeBlockNodeStorage(inputs),
            steps.UpgradeBlockNode(inputs),
        )
    }
    return automa.NewWorkflowBuilder().WithId("block-node-reconfigure").Steps(
        steps.UpgradeBlockNode(inputs),
    )
}
```

> Note: `ReconfigureHandler.BuildWorkflow` may call `steps.UpgradeBlockNode` directly (as `UpgradeHandler` does) rather than going through this helper — either is consistent with the existing pattern.

### 6. Cobra command — `cmd/weaver/commands/block/node/reconfigure.go` (NEW)

Model directly on `upgrade.go`. Key differences:

- `Use: "reconfigure"`, `Short: "Reconfigure a Hedera Block Node"`, `Long: "Re-apply configuration to an existing Hedera Block Node deployment without changing its chart version"`
- Action is `models.ActionReconfigure`
- Log messages: `"Reconfiguring Hedera Block Node"` / `"Successfully reconfigured Hedera Block Node"`
- `init()` registers: `common.FlagWithStorageReset()`, `common.FlagValuesFile()`, `common.FlagNoReuseValues()`
- **Hide the inherited `--chart-version` persistent flag** (declared on `nodeCmd`, has no effect on reconfigure):
```go
func init() {
    common.FlagWithStorageReset().SetVarP(reconfigureCmd, &flagWithReset, false)
    common.FlagValuesFile().SetVarP(reconfigureCmd, &flagValuesFile, false)
    common.FlagNoReuseValues().SetVarP(reconfigureCmd, &flagNoReuseValues, false)
    // chart-version is inherited from nodeCmd but has no effect on reconfigure
    reconfigureCmd.Flag("chart-version").Hidden = true
}
```

### 7. Register command — `cmd/weaver/commands/block/node/node.go`

```go
nodeCmd.AddCommand(checkCmd, installCmd, upgradeCmd, reconfigureCmd, resetCmd, uninstallCmd)
```

## Files summary

| File | Action |
|------|--------|
| `pkg/models/intent.go` | Add `ActionReconfigure`, wire `allowedOperations` |
| `internal/bll/blocknode/upgrade_handler.go` | Add resolution hint to downgrade error |
| `internal/bll/blocknode/reconfigure_handler.go` | **NEW** |
| `internal/bll/blocknode/handler.go` | Add field, instantiate, route |
| `internal/workflows/blocknode.go` | Add `NewBlockNodeReconfigureWorkflow` |
| `cmd/weaver/commands/block/node/reconfigure.go` | **NEW** |
| `cmd/weaver/commands/block/node/node.go` | Register `reconfigureCmd` |


Commit convention: `feat(block-node): implement reconfigure command`

