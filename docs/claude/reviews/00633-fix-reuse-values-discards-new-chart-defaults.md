# 00633 — Fix Helm `--reuse-values` discarding new chart defaults on `block node upgrade`

## Summary

`solo-provisioner block node upgrade` was failing whenever the operator crossed a
Block Node chart version that introduced a new default key in the chart's
`values.yaml`. The failure surfaced as a nil-pointer template error during Helm
rendering (e.g. chart `0.33.0`'s `statefulset.yaml:215` referencing
`.Values.blockNode.metrics.port`).

Root cause: Helm's `--reuse-values` (`action.Upgrade.ReuseValues=true`) overwrites
the **new** chart's `values.yaml` defaults with the **old** release's coalesced
values — see `vendor/helm.sh/helm/v3/pkg/action/upgrade.go` `reuseValues()`. Any
key the new chart added in defaults is silently dropped, and templates that
reference it panic.

Fix: in `pkg/helm/manager.go` `UpgradeChart`, map our wrapper's `ReuseValues`
intent ("keep previously supplied user values") onto Helm's
`ResetThenReuseValues` instead. That keeps the new chart's `values.yaml`
as the base, then overlays the old release's user-supplied `Config` on top —
which is what the user-facing `--no-reuse-values` flag has always semantically
promised. The CLI flag name, default, and `UpgradeChartOptions.ReuseValues`
field name are unchanged.

## Files changed

| File | Change |
|---|---|
| `pkg/helm/manager.go` | Replace inline `upgradeClient.ReuseValues = …` block with a new `applyReuseValues` helper that sets `ResetThenReuseValues` instead. |
| `pkg/helm/manager_test.go` | New unit test `TestApplyReuseValues` asserting the mapping; explicitly fails if `client.ReuseValues` is ever set to true again. |
| `docs/claude/reviews/00633-fix-reuse-values-discards-new-chart-defaults.md` | This review guide. |

## Review checklist

- [ ] `applyReuseValues` is called in both upgrade entry points: the direct `UpgradeChart` path, and the `DeployChart → UpgradeChart` re-entry (line 540 area). Verify by `grep -n 'applyReuseValues\|ReuseValues\|ResetThenReuseValues' pkg/helm/manager.go`.
- [ ] `upgradeClient.ReuseValues` is never assigned anywhere in `pkg/helm/manager.go`.
- [ ] When `o.ValueOpts == nil`, `applyReuseValues` still sets `ResetThenReuseValues = true` so we don't fall through to a no-values upgrade.
- [ ] `UpgradeChartOptions.ReuseValues` field is preserved (callers in `internal/bll/blocknode/helpers.go`, `internal/blocknode/chart.go`, `internal/blocknode/migrations.go` still compile and behave the same at the API level).
- [ ] `--no-reuse-values` CLI flag default (`false`) and description are unchanged in `cmd/cli/commands/common/flags_common.go` and `cmd/cli/commands/block/node/upgrade.go`.
- [ ] `docs/quickstart.md` `block node upgrade` section does not need changes (no flag surface change).

## Unit tests

```bash
go test -race -count=1 -run TestApplyReuseValues ./pkg/helm/...
```

For the broader helm-package unit suite (macOS-friendly):

```bash
go test -race -count=1 -tags='!integration' ./pkg/helm/...
```

Full unit suite must be run in the UTM VM because parts of the repo are Linux-only:

```bash
task vm:test:unit
```

## Integration tests

The existing `TestHelmManager_Integration` in `pkg/helm/manager_it_test.go` already
exercises the upgrade path with `ReuseValues: true` against the `podinfo` chart
and should continue to pass unchanged:

```bash
task vm:test:integration TEST_NAME='^TestHelmManager_Integration$'
```

## Manual UAT

Run inside the UTM VM. The pre-condition is the same install that reproduced the
bug.

### Step 1 — Install with the default Block Node chart version (0.30.2) and a values file lacking `blockNode.metrics`

```bash
cat > /tmp/shape-a.yaml <<'YAML'
service:
  type: ClusterIP
loadBalancer:
  enabled: true
  loadBalancerIP: "192.168.68.200"
  annotations:
    metallb.io/address-pool: public-address-pool
blockNode:
  config:
    JAVA_OPTS: '-Xms1G -Xmx1G'
  persistence:
    live:
      create: false
      existingClaim: live-storage-pvc
      subPath: ""
    archive:
      create: false
      existingClaim: archive-storage-pvc
      subPath: ""
    logging:
      create: false
      existingClaim: logging-storage-pvc
      subPath: ""
YAML

sudo solo-provisioner block node install --values /tmp/shape-a.yaml -p local -V
```

Expected: install completes successfully.

### Step 2 — Upgrade to 0.33.0 without `--no-reuse-values`

```bash
sudo solo-provisioner block node upgrade -p local --chart-version 0.33.0 -V
```

Expected before this PR (regression):

```text
✗ Failed to upgrade Block Node chart
    common.illegal_state: failed to upgrade block node chart, cause:
    helm.upgrade_failed: failed to run upgrade action, cause:
    template: block-node-server/templates/statefulset.yaml:215:37:
      executing "block-node-server/templates/statefulset.yaml"
      at <.Values.blockNode.metrics.port>: nil pointer evaluating interface {}.port
```

Expected with this PR:

```text
✓ Block Node chart upgraded successfully
```

### Step 3 — Confirm previously-supplied user values are still honored

```bash
helm -n block-node get values block-node | grep -A2 JAVA_OPTS
```

Expected: `-Xms1G -Xmx1G` (the value supplied at install time, preserved on upgrade).

### Step 4 — Confirm `--no-reuse-values` still discards user values

```bash
sudo solo-provisioner block node upgrade -p local --chart-version 0.33.0 --no-reuse-values -V
helm -n block-node get values block-node | grep -A2 JAVA_OPTS
```

Expected: `JAVA_OPTS` reverts to whatever the rendered nano-values template sets (`-Xms1G -Xmx1G -XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath="/tmp/dump.hprof"`).

### Step 5 — Cleanup

```bash
sudo solo-provisioner block node uninstall -p local -V
```
