# PR Review Guide — fix(blocknode): inject MetalLB annotation via Helm values

**Issue:** [#477](https://github.com/hashgraph/solo-weaver/issues/477)  
**Branch:** `477-fix-metallb-annotation-on-upgrade`

---

## What Was Done

### Problem

The MetalLB `metallb.io/address-pool: public-address-pool` annotation was applied to the block-node
`LoadBalancer` service via a **post-deploy `kubectl patch`** step (`annotateBlockNodeService`) in the
`SetupBlockNode` workflow. This step was **never added to `UpgradeBlockNode`**, so the annotation was
silently dropped after every upgrade, breaking external connectivity.

### Solution

Moved the annotation into the **Helm values** so that Helm manages it natively for both install and
upgrade — the same pattern used for retention-config and persistence overrides.

### Files Changed

| File | Change |
|------|--------|
| `internal/blocknode/values.go` | Added `injectServiceAnnotations()` method gated on `LoadBalancerEnabled`; skips injection if key already present in values file; called from `ComputeValuesFile` |
| `internal/blocknode/chart.go` | Removed `AnnotateService()` method |
| `internal/workflows/steps/step_block_node.go` | Removed `AnnotateBlockNodeServiceStepId` constant, `annotateBlockNodeService` step from `SetupBlockNode`, and the entire builder function |
| `internal/workflows/steps/step_block_node_test.go` | Updated step count 6→5, removed annotate step assertions |
| `pkg/models/inputs.go` | Added `LoadBalancerEnabled bool` to `BlockNodeInputs` |
| `pkg/config/global.go` | Default `LoadBalancerEnabled: true` in `globalConfig` |
| `cmd/weaver/commands/block/node/node.go` | Added `--load-balancer-enabled` persistent flag (default `true`) |
| `cmd/weaver/commands/block/node/init.go` | Wire `flagLoadBalancerEnabled` into `prepareBlocknodeInputs` |
| `internal/bll/blocknode/helpers.go` | Pass `LoadBalancerEnabled` through in `resolveBlocknodeEffectiveInputs` |
| `internal/blocknode/manager_test.go` | Added 5 unit tests for `injectServiceAnnotations` (disabled, injects when absent, creates path, preserves existing annotation, preserves sibling annotations) |
| `taskfiles/uat.yaml` | Added MetalLB annotation checks after v0.29.0 and v0.30.2 upgrade verify blocks |

---

## Code Review Checklist

### `internal/blocknode/values.go`

- [x] `injectServiceAnnotations` gates on `m.blockNodeInputs.LoadBalancerEnabled`, not on profile — any profile with MetalLB works.
- [x] If `metallb.io/address-pool` is **already present** in the values file, the method returns early without modifying the content (no silent clobber).
- [x] If the key is absent and `LoadBalancerEnabled` is `true`, `public-address-pool` is injected.
- [x] `ComputeValuesFile` calls `injectServiceAnnotations` (no `profile` argument) after `injectRetentionConfig`.

### `pkg/models/inputs.go`

- [x] `LoadBalancerEnabled bool` added to `BlockNodeInputs`.
- [x] Field is documented with a clear comment explaining when to set it.

### `pkg/config/global.go`

- [x] `globalConfig.BlockNode` does **not** contain `LoadBalancerEnabled` — the default `true` comes from the CLI flag default instead.

### `cmd/weaver/commands/block/node/node.go`

- [x] `--load-balancer-enabled` registered as a persistent flag with default `true` and a clear description.
- [x] `flagLoadBalancerEnabled bool` declared alongside the other flag variables.

### `cmd/weaver/commands/block/node/init.go`

- [x] `LoadBalancerEnabled: flagLoadBalancerEnabled` wired into `prepareBlocknodeInputs`.

### `internal/bll/blocknode/helpers.go`

- [x] `LoadBalancerEnabled: inputs.Custom.LoadBalancerEnabled` present in the pass-through section of `resolveBlocknodeEffectiveInputs`.

### `internal/blocknode/chart.go`

- [x] `AnnotateService()` is fully removed — no orphaned references remain.

### `internal/workflows/steps/step_block_node.go`

- [x] `AnnotateBlockNodeServiceStepId` constant is removed.
- [x] `annotateBlockNodeService` function is removed.
- [x] `SetupBlockNode` steps list has 5 entries: storage, namespace, PVs, install, wait.

### `internal/workflows/steps/step_block_node_test.go`

- [x] All three `SetupBlockNode` tests assert `Len == 5`.
- [x] No remaining references to `AnnotateBlockNodeServiceStepId` or `annotateReport`.

### `internal/blocknode/manager_test.go`

- [x] `TestInjectServiceAnnotations_Disabled` — `LoadBalancerEnabled: false` returns input byte-identical with no annotation written.
- [x] `TestInjectServiceAnnotations_InjectsWhenAbsent` — `LoadBalancerEnabled: true`, no `service` key → `public-address-pool` injected.
- [x] `TestInjectServiceAnnotations_CreatesServiceAndAnnotationsPath` — `service` and `service.annotations` created from scratch when absent.
- [x] `TestInjectServiceAnnotations_PreservesExistingAnnotation` — operator's custom pool value left untouched, input bytes returned unchanged.
- [x] `TestInjectServiceAnnotations_PreservesOtherAnnotations` — sibling annotations in `service.annotations` survive alongside the injected key.
- [x] `TestInjectRetentionConfig_OverridesExistingValues` has its full body (not a `// ...existing code...` stub).

### `taskfiles/uat.yaml`

- [x] MetalLB annotation check uses `kubectl get svc block-node-block-node-server -n block-node` (explicit name, not `.items[0]`).
- [x] Check added after both the v0.29.0 and v0.30.2 upgrade verify blocks.

---

## Running the Tests

### Unit tests for `injectServiceAnnotations` (macOS safe)

```bash
go test -tags='!integration' -run 'TestInjectServiceAnnotations' ./internal/blocknode/... -v
```

Expected output — all 5 cases pass:

```
--- PASS: TestInjectServiceAnnotations_Disabled
--- PASS: TestInjectServiceAnnotations_InjectsWhenAbsent
--- PASS: TestInjectServiceAnnotations_CreatesServiceAndAnnotationsPath
--- PASS: TestInjectServiceAnnotations_PreservesExistingAnnotation
--- PASS: TestInjectServiceAnnotations_PreservesOtherAnnotations
```

To run all blocknode unit tests together (including the existing `injectRetentionConfig` suite):

```bash
go test -tags='!integration' -run 'TestInjectRetentionConfig|TestInjectServiceAnnotations' ./internal/blocknode/... -v
```

### Full unit test suite (macOS safe)

```bash
task test:unit
```


### Integration tests (must run inside the UTM VM)

```bash
# Fresh install with mainnet profile (exercises injectServiceAnnotations for non-local)
task vm:test:integration TEST_NAME='^TestSetupBlockNode_FreshInstall$'

# Fresh install with local profile (exercises skip path — no annotation injected)
task vm:test:integration TEST_NAME='^TestSetupBlockNodeLocal_FreshInstall$'

# Idempotency — run install twice, verify no errors
task vm:test:integration TEST_NAME='^TestSetupBlockNodeLocal_Idempotency$'

# Reset workflow — unaffected by this change, but good regression check
task vm:test:integration TEST_NAME='^TestResetBlockNode_Success$'
```

---

## Manual UAT Steps (inside the UTM VM)

Run the full upgrade UAT suite after building:

```bash
task build:weaver GOOS=linux GOARCH=amd64
task uat:setup        # installs cluster + block node from scratch
task uat:core         # runs install → upgrade v0.29.0 → upgrade v0.30.2 → reset
```

The two new MetalLB annotation checks in `uat:core` will print:

```
▶ Verify MetalLB annotation present after upgrade to v0.29.0
   metallb.io/address-pool: public-address-pool ✓

▶ Verify MetalLB annotation present after upgrade to v0.30.2
   metallb.io/address-pool: public-address-pool ✓
```

If either check fails you will see:

```
FAIL: metallb annotation missing after v0.29.0 upgrade, got: ''
```

### Spot-check after a manual upgrade

After any `block node upgrade`, confirm the annotation survived:

```bash
kubectl get svc block-node-block-node-server -n block-node -o jsonpath='{.metadata.annotations.metallb\.io/address-pool}'
# Expected: public-address-pool
```

Also verify the annotation appears in the Helm-managed values (not just as a kubectl annotation):

```bash
helm get values block-node -n block-node
# Expected output includes:
# service:
#   annotations:
#     metallb.io/address-pool: public-address-pool
```

