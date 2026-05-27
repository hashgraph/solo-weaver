# #612 — Add `--purge-storage` flag for PV/PVC deletion and align `--with-reset` semantics

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/612
> **Story branch:** `00612-purge-storage-flag`
> **PR base:** `main` (branched from `origin/main` @ `1967568`)
> **PR closes:** #612

## Summary

`solo-provisioner block node` has no first-class way to delete the block node's PVCs and PVs without also tearing down the whole Kubernetes cluster. The only existing path that touches PV/PVCs is `block node reconfigure --with-reset`, which delete/recreates them as a side effect — making `--with-reset` inconsistent across subcommands (data-only wipe everywhere except reconfigure).

This PR:
1. Adds a new `--purge-storage` flag to `block node uninstall` and `block node reconfigure` that explicitly deletes PVCs and PVs in addition to wiping data.
2. Restores `--with-reset` to a single meaning across uninstall/reset/upgrade/reconfigure: "wipe data, keep K8s objects."
3. Refactors `DeleteAllPersistentVolumes` from a hardcoded name allowlist to a label-scoped selector, so it survives version skew, chart drift, and a future move to dynamic provisioning.

`--purge-storage` implies `--with-reset` (data must be wiped before PVs are removed; otherwise local-PV `reclaimPolicy: Retain` leaves orphaned hostPath directories).

## Problem

Today's semantics (current code):

| Command | Helm release | Data on disk | PV/PVC objects |
|---|---|---|---|
| `block node uninstall` | removed | kept | kept |
| `block node uninstall --with-reset` | removed | **wiped** | kept |
| `block node reset` | scaled down then up | **wiped** | kept |
| `block node upgrade --with-reset` | upgraded | **wiped** | kept |
| `block node reconfigure --with-reset` | upgraded | **wiped** | **deleted + recreated** ← outlier |

The reconfigure outlier exists because reconfigure can change storage paths and a local-PV's `hostPath.path` is immutable (`internal/blocknode/storage.go:165`-ish; `RecreateBlockNodeStorage` at `internal/workflows/steps/step_block_node.go:684-702`). But that's an implementation detail leaking into user-facing flag semantics.

Today the only way to fully remove block-node PV/PVCs is `solo-provisioner kube cluster uninstall`, which runs `kubeadm reset` and tears down the whole cluster (`internal/workflows/cluster.go` `UninstallClusterWorkflow`). That works only because the API server is gone — it's not targeted cleanup.

Two latent fragilities in `DeleteAllPersistentVolumes` (`internal/blocknode/storage.go:173-199`):

1. **Name-allowlist drift.** Hardcoded `live-storage-pvc`, `archive-storage-pvc`, `logging-storage-pvc` + optional names derived from `GetApplicableOptionalStorages(chartVersion)` (`internal/blocknode/optional_storage.go:130-140`). If the recorded chart version doesn't match what's actually deployed (partial migration, manual `helm rollback`, version skew), optional storages leak.
2. **No coverage for future dynamic provisioning.** PV names become `pvc-<uuid>` and the name allowlist misses them entirely.

**Surprising finding from exploration** (worth flagging to reviewers): block-node PVs and PVCs are **not Helm-managed**. They're created by solo-provisioner itself from `internal/templates/files/block-node/storage-config.yaml` and `optional-storage.yaml`. The templates set **no labels at all** — no `app.kubernetes.io/managed-by`, no `instance`, nothing. The issue body's proposed selector `app.kubernetes.io/managed-by=Helm` would match zero resources today. This shapes the labels decision below.

## Decisions

| Question | Decision |
|---|---|
| What label selector to use for PV/PVC selection? | Solo-provisioner adds its own labels to the PV/PVC templates: `app.kubernetes.io/managed-by=solo-provisioner`, `app.kubernetes.io/component=block-node-storage`, `app.kubernetes.io/instance=<release-name>`. Select on the first two — the third disambiguates if two releases ever coexist. |
| How to handle PVs (cluster-scoped) so we don't accidentally delete unrelated resources? | List cluster-wide, then filter by **both** label selector AND `spec.claimRef.namespace == <block-node-ns>`. Belt-and-suspenders: labels identify "ours", namespace pins the release. |
| What about pre-existing deployments whose PVs/PVCs don't carry the new labels? | Add a block-node migration (`internal/migration/`) that patches missing labels onto existing block-node PV/PVCs on first run after upgrade. CLAUDE.md confirms block-node migrations run during upgrades — this is the right hook. |
| Does `--purge-storage` imply `--with-reset`? | Yes. Passing `--purge-storage` alone is equivalent to passing both; passing both together does not error. The implementation treats `purgeStorage` as a strict superset of `reset`. |
| Should `--purge-storage` be added to `reset` and `upgrade` too? | No. The issue only adds it to `uninstall` and `reconfigure`. `reset` and `upgrade` keep K8s objects by design — purging PVs would invalidate their next-step logic (reset scales back up; upgrade does in-place chart upgrade). |
| Should we rename the existing `PurgeBlockNodeStorage` step (which only wipes data, not PVs)? | **No** — out of scope. The name is misleading after this PR (a flag called `--purge-storage` does more than the step called `PurgeBlockNodeStorage`), but renaming touches several call sites for cosmetic gain. Flag in the review guide as a possible follow-up. |
| Reconfigure path-change behavior under the new semantics? | If storage paths change, the operator must pass `--purge-storage` (not `--with-reset`). The current error message at `reconfigure_handler.go:80-83` ("re-run with `--with-reset` to delete existing PVs/PVCs") changes to "re-run with `--purge-storage`". |

## Scope

### 1. New flag

- `cmd/cli/commands/common/flags_common.go` — add `FlagPurgeStorage()` next to the existing `FlagWithStorageReset()`. Description: "Delete persistent volumes and claims in addition to wiping data (implies --with-reset)".
- `pkg/models/inputs.go` (or wherever `BlockNodeInputs.Custom`/equivalent lives — confirm during implementation) — add a `PurgeStorage bool` field alongside the existing `ResetStorage bool`.
- `cmd/cli/commands/block/node/uninstall.go` — register `FlagPurgeStorage()`; bind into inputs.
- `cmd/cli/commands/block/node/reconfigure.go` — same.
- Flag is **not** added to `reset.go` or `upgrade.go`.

### 2. Uninstall handler

- `internal/bll/blocknode/uninstall_handler.go:36-57` `BuildWorkflow`:
  - If `ins.PurgeStorage`: `PurgeBlockNodeStorage(ins)` → `DeleteBlockNodePersistentVolumes(ins)` (new step) → `UninstallBlockNode(ins)`.
  - Else if `ins.ResetStorage`: unchanged — `PurgeBlockNodeStorage(ins)` → `UninstallBlockNode(ins)`.
  - Else: unchanged — `UninstallBlockNode(ins)`.
- Normalize at handler entry: if `PurgeStorage` is set, also set `ResetStorage = true` so any downstream reads see the implication.

### 3. Reconfigure handler

- `internal/bll/blocknode/reconfigure_handler.go:60-80`:
  - Replace the existing `--with-reset` branch (which currently composes `PurgeBlockNodeStorage(oldIns), RecreateBlockNodeStorage(ins), UpgradeBlockNode(ins)`).
  - New behavior:
    - `--purge-storage`: `PurgeBlockNodeStorage(oldIns)` → `RecreateBlockNodeStorage(ins)` → `UpgradeBlockNode(ins)`. Unchanged composition; only the gating flag name is different. `RecreateBlockNodeStorage` already calls `deleteBlockNodePVs` internally (`step_block_node.go:657-678`) — that helper is what gets refactored to use the label-scoped selector.
    - `--with-reset` alone: only wipe data — `PurgeBlockNodeStorage(oldIns)` → `UpgradeBlockNode(ins)`. (Path changes still error out; see below.)
    - No flag: unchanged (current `noRestart`/default branches).
  - Storage-path-change check (`reconfigure_handler.go:75-83`): the error now says "re-run with **`--purge-storage`** to delete existing PVs/PVCs and recreate them at the new paths." The check itself is unchanged — only the message.

### 4. Label-scoped PV/PVC deletion

- `internal/blocknode/storage.go` `DeleteAllPersistentVolumes` (lines 173-199) — rewrite:
  - PVCs: `kubeClient.ListPVCs(ctx, ns, labelSelector)` where `labelSelector = "app.kubernetes.io/managed-by=solo-provisioner,app.kubernetes.io/component=block-node-storage"`. For each, `DeletePVC`.
  - PVs: `kubeClient.ListPVs(ctx, labelSelector)` (cluster-scoped). Filter results by `spec.claimRef.namespace == ns`. For each, `DeletePV`.
  - Function name stays the same (callers don't change).
- `internal/kube/client.go` (after the existing `DeletePV`/`DeletePVC` at lines 398-424) — add:
  - `ListPVCs(ctx context.Context, ns, labelSelector string) ([]corev1.PersistentVolumeClaim, error)` (or `[]unstructured.Unstructured` to match the dynamic-client pattern already in use).
  - `ListPVs(ctx context.Context, labelSelector string) ([]corev1.PersistentVolume, error)` (same).
  - Pattern: mirror the existing dynamic-client usage; pass `metav1.ListOptions{LabelSelector: selector}`.
- Drop the dependency on `GetApplicableOptionalStorages(chartVersion)` from this code path. The function itself stays (still used by storage setup/creation).

### 5. PV/PVC label injection in templates

- `internal/templates/files/block-node/storage-config.yaml` — add `metadata.labels` to every PV and PVC:
  ```yaml
  metadata:
    name: live-storage-pv
    labels:
      app.kubernetes.io/managed-by: solo-provisioner
      app.kubernetes.io/component: block-node-storage
      app.kubernetes.io/instance: {{ or .Release "block-node" }}
  ```
  Repeat for archive, logging, verification, plugins. Same labels on the corresponding PVCs.
- `internal/templates/files/block-node/optional-storage.yaml` — same labels on the generic optional PV/PVC pair.
- Confirm the template's render context exposes `.Release`. If not, thread the release name through from `BlockNodeInputs.Release`.

### 6. Block-node migration: label existing PV/PVCs

- New migration under `internal/migration/blocknode/` (or wherever block-node migrations live — confirm during implementation). Runs on upgrade per CLAUDE.md.
- For each known PVC name (`live-storage-pvc`, `archive-storage-pvc`, `logging-storage-pvc`, plus the version-applicable optionals — this is the **last** use of the name allowlist, justified because migrations are inherently version-aware): patch metadata.labels with the three labels if missing.
- For the corresponding PVs: same patch.
- Idempotent: re-running is a no-op if labels are already present.

### 7. New workflow step

- `internal/workflows/steps/step_block_node.go` — add `DeleteBlockNodePersistentVolumes(inputs)` that returns an `automa.WorkflowBuilder` wrapping the existing `deleteBlockNodePVs(managerProvider)` helper (lines 657-678). This is just the public entry-point for `uninstall_handler.go` to use; the internal helper stays.

### 8. Tests

- Unit:
  - `internal/blocknode/storage_test.go` (or new file) — `DeleteAllPersistentVolumes` happy path with a fake kube client returning labeled PV/PVCs; verifies label selector construction and `claimRef.namespace` filtering. Cases: zero matches; one PV in a different namespace (ignored); release-A vs release-B isolation.
  - `internal/bll/blocknode/uninstall_handler_test.go` — three workflow shapes (no flag, `--with-reset`, `--purge-storage`).
  - `internal/bll/blocknode/reconfigure_handler_test.go` — extend to cover `--purge-storage` path; `--with-reset` no-path-change new shape (no PV recreate); path-change error message refers to `--purge-storage`.
  - Migration: label-patch idempotency and PV/PVC pairs.
- Integration (UTM VM): `task vm:test:integration TEST_NAME='^Test_BlockNode_UninstallPurgeStorage_Integration$'`. End-to-end: `block node install` → assert PV/PVCs exist and carry the new labels → `block node uninstall --purge-storage` → assert `kubectl get pvc -n block-node` and `kubectl get pv` (filtered by `claimRef`) return zero block-node resources.

## Out of scope

- Renaming the existing `PurgeBlockNodeStorage` step. Noted as confusing post-PR but cosmetic; defer to a follow-up.
- Adding `--purge-storage` to `block node reset` or `block node upgrade`. Issue limits it to `uninstall` and `reconfigure`.
- Removing the now-untouched `GetApplicableOptionalStorages(chartVersion)` callers from non-deletion code paths. Still needed for storage setup.
- Updating chart-side labels for any future Helm-managed PVs — block-node PVs are not Helm-managed today.

## Test plan

- [ ] Unit: `task test:unit` covering the three packages above.
- [ ] VM unit: `task vm:test:unit` (block-node Linux-only paths).
- [ ] Integration (UTM VM):
  - `task vm:test:integration TEST_NAME='^Test_BlockNode_UninstallPurgeStorage_Integration$'`
  - `task vm:test:integration TEST_NAME='^Test_BlockNode_Reconfigure'` — both `--with-reset` (no PV touch) and `--purge-storage` (PV recreate) shapes.
- [ ] Manual UAT in UTM VM:
  1. `solo-provisioner kube cluster install --profile local`
  2. `solo-provisioner block node install`
  3. `kubectl get pvc -n block-node -o jsonpath='{.items[*].metadata.labels}'` → confirms new labels present.
  4. `solo-provisioner block node uninstall` → `kubectl get pvc -n block-node` still shows the PVCs.
  5. `solo-provisioner block node install` (re-install reuses the kept PVs).
  6. `solo-provisioner block node uninstall --with-reset` → PVCs still present, data dirs empty.
  7. `solo-provisioner block node install`, then `solo-provisioner block node uninstall --purge-storage` → `kubectl get pvc -n block-node` and `kubectl get pv` filtered by claimRef both empty.
- [ ] Upgrade-path UAT: install a pre-PR build, then upgrade to this PR's build via `block node upgrade`, then verify labels are now present on the existing PV/PVCs (migration ran).
- [ ] Docs: confirm the operator guide (likely under `docs/`) distinguishes the three uninstall variants and notes `kube cluster uninstall` is no longer the only path for PV/PVC removal.

## Risks / rollbacks

- **Migration risk**: the label-patch migration runs on every block-node upgrade. If a PVC has finalizers stuck, the patch could fail and block upgrades. Mitigation: patch with `client.Patch` using `types.MergePatchType` and short timeout; on failure, log a warning and continue (labels can be added by re-running the next time; failure to label is not a hard fault).
- **Label collisions**: if any future Helm chart for block-node-adjacent infrastructure also sets `app.kubernetes.io/component=block-node-storage`, our selector would match it. Mitigation: the `managed-by=solo-provisioner` half scopes us to provisioner-created resources only.
- **Rollback**: pure code rollback (revert the PR). Existing labeled PV/PVCs from this build remain harmless under the old code (extra labels are ignored by name-based deletion). The migration is idempotent and not destructive.
- **User confusion (`--with-reset` semantic change in reconfigure)**: operators who previously used `reconfigure --with-reset` to delete PV/PVCs will find that flag no longer does so. The error message at the storage-path-change check now explicitly directs them to `--purge-storage`. Call this out in the changelog/PR description.
