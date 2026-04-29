# Pre-Implementation Review: Block Node Reconfigure (GH #450)

## Plan reviewed

[`docs/claude/plans/00450-block-node-reconfigure.md`](../plans/00450-block-node-reconfigure.md)

---

## Plan correctness

### ✅ Handler pattern is correct

`ReconfigureHandler` follows the exact same interface contract as `UpgradeHandler`, `ResetHandler`, and `UninstallHandler`:
- Implements `bll.IntentHandler[models.BlockNodeInputs]`
- `PrepareEffectiveInputs` → `resolveBlocknodeEffectiveInputs`
- `BuildWorkflow` → validates preconditions, returns `*automa.WorkflowBuilder`
- `HandleIntent` → delegates to `BaseHandler.HandleIntent` with `patchBlockNodeChartRef()`

### ✅ ChartVersion lock is handled by the runtime, not the handler

`resolveBlocknodeEffectiveInputs` calls `runtime.ChartVersion()`, which uses `StrategyCurrent` as the highest-priority source. When `reconfigure` passes no user-supplied `ChartVersion`, the runtime falls back to the current deployed version from state (`st.ReleaseInfo.ChartVersion`). No manual override in the handler is needed — this is the correct approach.

### ✅ `steps.UpgradeBlockNode` is safe to reuse

`UpgradeBlockNode` in `step_block_node.go` calls `manager.UpgradeChart(ctx, valuesFilePath, inputs.ReuseValues)` which is a plain `helm upgrade`. It does not perform any version validation itself — all semver logic lives in `UpgradeHandler.BuildWorkflow`. Reusing it for reconfigure is correct.

### ✅ Downgrade error improvement is self-contained

Adding `WithProperty(models.ErrPropertyResolution, ...)` to the existing `desiredVer.LessThan(currentVer)` guard in `UpgradeHandler` is a pure UX improvement with no behaviour change. Downgrades remain unconditionally blocked.

### ✅ `ActionReconfigure` in `allowedOperations` is sufficient

`Intent.IsValid()` gates all intent routing. Adding `ActionReconfigure: {TargetBlockNode}` is the only model change required — no other validation or serialisation concerns.

---

## Edge cases

### ⚠️ `--chart-version` flag is inherited and silently ignored

`--chart-version` is a persistent flag on `nodeCmd`, so it appears in `reconfigure --help`. If a user passes it, the value is populated into `inputs.Custom.ChartVersion`, but the runtime's `StrategyCurrent` will override it before `BuildWorkflow` is called — the flag has no effect.

**Resolution in plan**: hide the flag via `reconfigureCmd.Flag("chart-version").Hidden = true` in `init()`. ✅ Correct approach.

**Caveat**: `cobra` only hides the flag from `--help` output; it still parses and accepts the value without error. A user who explicitly passes `--chart-version` will not get an error — it is silently ignored. This is acceptable behaviour (reconfigure is version-agnostic by design) but could be confusing.

**Optional improvement** (not blocking): log a `WARN` in `PrepareEffectiveInputs` if `inputs.Custom.ChartVersion` is non-empty when action is `ActionReconfigure`, telling the user the flag is ignored.

### ⚠️ `BlockNodeState.ReleaseInfo.ChartVersion` may be empty

If the block node was installed before state tracking of `ChartVersion` was added, `currentState.BlockNodeState.ReleaseInfo.ChartVersion` could be an empty string. The runtime's `StrategyCurrent` would then propagate an empty string as `ChartVersion`, and `bnpkg.ValidateStorageCompleteness` or a downstream Helm call may fail with an unhelpful error.

**Mitigation**: `BuildWorkflow` should check that `inputs.Custom.ChartVersion` is non-empty after `PrepareEffectiveInputs` and return a clear `errorx.IllegalState` with a resolution hint (e.g. "chart version could not be determined from state; run 'weaver block node upgrade --chart-version <version>' to set it").

**Status**: Not covered in plan — **should be added**.

### ✅ `--with-reset` on reconfigure is safe

`PurgeBlockNodeStorage` + `UpgradeBlockNode` is the same sequence used by upgrade-with-reset. Storage paths are validated by `ValidateStorageCompleteness` before the workflow runs.

### ✅ `patchBlockNodeChartRef()` is a no-op when ChartRef is unchanged

The patch function only writes to state if `effectiveInputs.Custom.Chart != ""` and `ReleaseInfo.Status == StatusDeployed`. On reconfigure the chart ref does not change, so the patch is idempotent. ✅

---

## Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| Empty `ChartVersion` in state causes silent Helm failure | Medium | Add non-empty guard in `BuildWorkflow` (see edge case above) |
| `--chart-version` silently ignored confuses users | Low | Flag is hidden; optional WARN log acceptable |
| `NewBlockNodeReconfigureWorkflow` helper never called | Low | Handler calls `steps.UpgradeBlockNode` directly (same as upgrade) — helper is for symmetry only, not a correctness concern |
| Cobra registers `reconfigureCmd` with a different variable scope for `flagWithReset` / `flagNoReuseValues` than `upgradeCmd` | Low | Both commands share package-level vars — confirm `init()` in `reconfigure.go` uses `&flagWithReset` / `&flagNoReuseValues` (already declared in `upgrade.go`) and does NOT redeclare them |

---

## Open questions

1. **Shared flag variables**: `flagWithReset`, `flagNoReuseValues`, and `flagValuesFile` are declared as package-level `var` in `upgrade.go`. `reconfigure.go` must reference the same variables, not redeclare them. Confirm `upgrade.go` declarations are accessible (same package `node`) — they are, since both files are in `package node`. ✅

2. **`NewBlockNodeReconfigureWorkflow` placement**: the helper is listed as "optional but recommended". Given it is not called by the handler, it is purely documentary. Worth adding for `CLAUDE.md` discoverability but not a correctness requirement.

3. **TUI messages**: `UpgradeBlockNodeStepId` is reused as the step ID inside `steps.UpgradeBlockNode`. The TUI will display "Upgrading Block Node chart" for a reconfigure operation. This is technically inaccurate. Consider adding a `ReconfigureBlockNodeStepId` constant and a `ReconfigureBlockNode` step wrapper in `step_block_node.go` with corrected messages — or accept the current wording as-is since the underlying operation is identical.

---

## Checklist

- [ ] `ActionReconfigure` added to `pkg/models/intent.go` with `TargetBlockNode` in `allowedOperations`
- [ ] Downgrade error in `upgrade_handler.go` has `WithProperty(ErrPropertyResolution, ...)` hint
- [ ] `reconfigure_handler.go` created; `BuildWorkflow` guards for empty `ChartVersion` from state
- [ ] `handler.go` wires `reconfigure` field, factory, and `ForAction` case
- [ ] `reconfigure.go` Cobra command created; `--chart-version` flag hidden in `init()`
- [ ] `reconfigure.go` `init()` references existing package-level flag vars (no redeclaration)
- [ ] `reconfigureCmd` registered in `node.go` `AddCommand`
- [ ] `task lint` passes after all changes
- [ ] `task test:unit` passes (run in VM)

