# Review Guide — #612 `--purge-storage` flag + `--with-reset` alignment

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/612
> **Plan:** [`docs/claude/plans/00612-purge-storage-flag.md`](../plans/00612-purge-storage-flag.md)

## Summary

`solo-provisioner` previously had no first-class way to delete a block node's PVCs and PVs without tearing down the whole cluster. The only operation that touched them — `reconfigure --with-reset` — did so as a side effect, making `--with-reset` mean different things in different subcommands (data-only everywhere except reconfigure, which also recreated PVs).

This change:

1. Adds `--purge-storage` to `block node uninstall` and `block node reconfigure`. It wipes data and deletes PVCs/PVs; implies `--with-reset`.
2. Restores `--with-reset` to a single meaning across uninstall / reset / upgrade / reconfigure: **"wipe data, keep K8s objects."** `reconfigure --with-reset` no longer deletes PV/PVCs; a path change now requires `--purge-storage` (the error message points reviewers at the right flag).
3. Refactors `DeleteAllPersistentVolumes` from a hardcoded name allowlist to label-scoped selection, with a dual selector that catches **both** solo-provisioner-managed PV/PVCs (from our templates) **and** Helm-managed ones (if a values override ever flips the chart's `persistence.create` to true). PVs are additionally filtered by `claimRef.namespace`. The legacy name list is kept as a transitional fallback for installations created before the labels were introduced.
4. Stamps the new labels (`app.kubernetes.io/managed-by=solo-provisioner`, `component=block-node-storage`, `instance=<release>`) onto every PV/PVC rendered by `internal/templates/files/block-node/{storage-config,optional-storage}.yaml`.

Behavior matrix after this PR:

| Command | Helm release | Data on disk | PV/PVC objects |
|---|---|---|---|
| `block node uninstall` | removed | kept | kept |
| `block node uninstall --with-reset` | removed | **wiped** | kept |
| `block node uninstall --purge-storage` | removed | **wiped** | **deleted** |
| `block node reset` | scaled down then up | **wiped** | kept |
| `block node upgrade --with-reset` | upgraded | **wiped** | kept |
| `block node reconfigure --with-reset` | upgraded | **wiped** | kept *(was: deleted+recreated)* |
| `block node reconfigure --purge-storage` | upgraded | **wiped** | **deleted+recreated** |

## Changed files

| Path | Description |
|---|---|
| `cmd/cli/commands/common/flags_common.go` | Add `FlagPurgeStorage()`; reword `FlagWithStorageReset` description to "Wipe block node data directories; PVs and PVCs are preserved" |
| `cmd/cli/commands/block/node/uninstall.go` | Register `--purge-storage` |
| `cmd/cli/commands/block/node/reconfigure.go` | Register `--purge-storage` |
| `cmd/cli/commands/block/node/upgrade.go` | Add package-level `flagPurgeStorage` variable (shared with siblings via the package scope) |
| `cmd/cli/commands/block/node/init.go` | Thread `PurgeStorage` into `BlockNodeInputs`; normalize so purge implies reset |
| `cmd/cli/commands/block/node/install_it_test.go` | Fix `resetFlags` to reset `flagWithReset` and the new `flagPurgeStorage` (pre-existing gap) |
| `cmd/cli/commands/block/node/uninstall_it_test.go` *(new)* | Integration test verifying `--purge-storage` is registered on `uninstall` + `reconfigure` only |
| `pkg/models/inputs.go` | Add `PurgeStorage bool` to `BlockNodeInputs` |
| `internal/bll/blocknode/helpers.go` | Pass `PurgeStorage` through `resolveBlocknodeEffectiveInputs` |
| `internal/bll/blocknode/uninstall_handler.go` | Add `--purge-storage` workflow branch: `PurgeBlockNodeStorage → DeleteBlockNodePersistentVolumes → UninstallBlockNode` |
| `internal/bll/blocknode/uninstall_handler_test.go` *(new)* | Cover all three uninstall workflow shapes; purge implies reset |
| `internal/bll/blocknode/reconfigure_handler.go` | Three-branch switch: `--purge-storage` (purge → recreate → upgrade), `--with-reset` (purge → upgrade, path change rejected), default. Error message points at `--purge-storage` |
| `internal/bll/blocknode/reconfigure_handler_test.go` | Updated for new shapes; new tests for `--purge-storage` paths and `--with-reset` path-change rejection |
| `internal/blocknode/storage.go` | Refactor `DeleteAllPersistentVolumes` to dual-selector label scoping + claimRef.namespace filter for PVs; transitional legacy-name fallback. Thread `Release` into both template render contexts |
| `internal/blocknode/storage_config_test.go` | Add `Release` to existing test data; new tests assert labels are present in rendered output for both templates |
| `internal/kube/client.go` | Add `ListPVCs(ctx, ns, labelSelector)` and `ListPVs(ctx, labelSelector)` using the dynamic client |
| `internal/workflows/steps/step_block_node.go` | New public `DeleteBlockNodePersistentVolumes` workflow step wrapping the existing `deleteBlockNodePVs` helper |
| `internal/templates/files/block-node/storage-config.yaml` | Add `managed-by` / `component` / `instance` labels to every PV and PVC |
| `internal/templates/files/block-node/optional-storage.yaml` | Same labels on the generic optional PV/PVC pair |
| `docs/quickstart.md` | Rewrite block-node uninstall section around the three-variant matrix; update `--with-reset` descriptions to match new semantics; add path-change note for reconfigure |
| `docs/dev/acceptance-tests.md` | List the new `uat:purge-storage` task |
| `taskfiles/uat.yaml` | Add `uat:purge-storage`; wire into `uat:all` |
| `.github/workflows/flow-test-uat.yaml` | Add `purge-storage` to the manual-dispatch scenario dropdown |
| `.github/workflows/zxc-uat-test.yaml` | Update input description to list `purge-storage` |
| `CLAUDE.md` | New workflow rule #6: CLI surface changes require a `docs/quickstart.md` update |
| `docs/claude/plans/00612-purge-storage-flag.md` *(new)* | Implementation plan |

## Code review checklist

- [ ] **Flag wiring**: `--purge-storage` exists on `uninstall` + `reconfigure` only — confirm `upgrade` and `reset` reject the flag with an "unknown flag" error. Covered by the new integration test in `uninstall_it_test.go`.
- [ ] **Purge implies reset**: passing `--purge-storage` without `--with-reset` still selects the `block-node-uninstall-purge-storage` workflow (purge is the superset). Confirmed in `init.go` (`ResetStorage = flagWithReset || flagPurgeStorage`) and in `TestUninstall_PurgeImpliesReset`.
- [ ] **Ordering invariant**: in `uninstall_handler.go`, the purge branch must be `PurgeBlockNodeStorage → DeleteBlockNodePersistentVolumes → UninstallBlockNode`. Wiping data before deleting PVs matters because the templates' PVs default to `reclaimPolicy: Retain` for local-PV hostPath — deleting PVs first leaves orphaned host directories.
- [ ] **Reconfigure path-change**: under `--with-reset` only, a path change is now rejected (was: silently recreated). Error message must direct the operator to `--purge-storage`. Two tests cover this: `TestBuildWorkflow_WithReset_PathsChanged_ReturnsError` and `TestBuildWorkflow_NoReset_ChangedPathsReturnsError`.
- [ ] **Label-scoped deletion correctness**:
  - `DeleteAllPersistentVolumes` issues two list calls (solo-provisioner selector + Helm selector parametrized by release), deduplicates by name, and filters PVs by `spec.claimRef.namespace == ns`.
  - Helm selector is built only when `release != ""` (defensive against empty inputs).
  - The transitional legacy-name fallback is idempotent — `DeletePVC` / `DeletePV` return nil on `NotFound`.
- [ ] **Template labels**:
  - Every PV and PVC in `storage-config.yaml` (5 core + 2 optional) carries all three labels.
  - `optional-storage.yaml` (used by `Manager.CreateOptionalStorage` during storage migrations) carries the same labels. This matters for verification/plugins PVCs created by `StorageMigration.Execute`.
  - `{{ or .Release "block-node" }}` keeps the default working if `Release` is somehow empty.
- [ ] **No behavior change for plain `uninstall`**: the no-flag branch is unchanged. Existing operators relying on "uninstall keeps everything for a future re-install" are not affected.
- [ ] **Pre-PR installations**: PV/PVCs deployed before this PR have no labels. The transitional legacy-name fallback in `DeleteAllPersistentVolumes` ensures `--purge-storage` still works on them. Once the field has rolled past this version, the fallback can be removed in a future PR (see TODO comment in `storage.go`).
- [ ] **Quickstart parity**: every flag description in `docs/quickstart.md` matches its source in `flags_common.go`.

## Tests

### Unit (macOS, runs everywhere not gated by `internal/mount`)

```bash
go test -race -tags='!integration' \
  ./internal/blocknode/... \
  ./internal/kube/... \
  ./pkg/models/...
```

Expected: all three pass. Highlights:

- `TestStorageConfigRendersLabels` / `TestOptionalStorageRendersLabels` — proves the templates stamp labels.
- `TestBuildWorkflow_WithReset_PathsUnchanged_DataOnly` — new shape: `purge → upgrade` (no Recreate).
- `TestBuildWorkflow_WithReset_PathsChanged_ReturnsError` — path-change rejected.
- `TestBuildWorkflow_PurgeStorage_IncludesRecreateStep` — purge-storage triggers the full purge → recreate → upgrade chain.
- `TestUninstall_NoFlags_HelmOnly` / `WithReset_PurgeThenUninstall` / `PurgeStorage_FullCleanup` / `PurgeImpliesReset` — full uninstall workflow matrix.

### Unit (VM, full coverage including `internal/bll/blocknode`)

```bash
task vm:test:unit
```

Required because `internal/mount` is Linux-only; the `bll/blocknode` test package transitively imports it.

### Integration (VM)

```bash
task vm:test:integration TEST_NAME='^TestPurgeStorageFlag_Registration$'
```

Verifies the flag is registered on `uninstall` + `reconfigure` only.

## Manual UAT

The repository ships a dedicated UAT for this PR. Inside the VM:

```bash
task uat:setup           # cluster + block node @ v0.26.0
task uat:purge-storage   # walks all three uninstall variants, asserts state after each
```

`uat:purge-storage` does:

1. **Sanity** — captures baseline PVC and PV counts (filtered by `claimRef.namespace == block-node`), confirms live data dir is populated.
   ```
   PVCs: 5  PVs (claimRef in block-node): 5
   ```
2. **Variant 1** — `uninstall` (no flag), expects helm release gone, PVCs/PVs retained, data preserved.
   ```
   ✓ helm gone; PVCs=5 PVs=5; data preserved
   ```
3. Re-installs, then **Variant 2** — `uninstall --with-reset`, expects PVCs/PVs retained, data wiped.
   ```
   ✓ helm gone; PVCs=5 PVs=5; data wiped
   ```
4. Re-installs, then **Variant 3** — `uninstall --purge-storage`, expects everything gone.
   ```
   ✓ helm gone; PVCs=0 PVs=0; data wiped
   ```

If any assertion fails the task exits non-zero with a `FAIL:` prefixed message; passing output ends with `✅ Block Node Uninstall Variants: PASSED`.

To exercise the reconfigure path-change error manually:

```bash
sudo solo-provisioner block node install -p local -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
sudo solo-provisioner block node reconfigure -p local --base-path /mnt/new-location --with-reset
# expect: error mentioning "storage paths have changed" and "--purge-storage"
sudo solo-provisioner block node reconfigure -p local --base-path /mnt/new-location --purge-storage
# expect: success; old PVs deleted, new PVs created at /mnt/new-location
```

## Notes for reviewers

- **Why the dual selector**: the issue body proposed a single Helm-managed selector, but block-node PV/PVCs are not Helm-managed today (they come from solo-provisioner templates). The dual selector is robust to both the current world and a hypothetical future where a values override flips `persistence.create` on the chart.
- **Why the legacy-name fallback**: PV/PVCs deployed before this PR have no labels. Without the fallback, `uninstall --purge-storage` on a pre-PR installation would no-op silently. The fallback is idempotent and self-disabling once labels are present — `seenPV` / `seenPVC` skip names already handled by the label-scoped pass. Slated for removal in a follow-up once we're confident no pre-PR installs remain.
- **Naming wart**: the existing `PurgeBlockNodeStorage` step only wipes data — its name is misleading next to the new `--purge-storage` flag, which does more. Renaming is out of scope (cosmetic, churn across several call sites). Worth a small follow-up PR.
