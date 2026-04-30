# Fix: MetalLB External IP Lost After Block Node Upgrade

## Context

**Root cause:** The `SetupBlockNode` (install) workflow in
`internal/workflows/steps/step_block_node.go` includes an `annotateBlockNodeService` step that
patches the live Kubernetes Service after Helm install with:

```yaml
metallb.io/address-pool: public-address-pool
```

This tells MetalLB which pool to assign an external IP from. The `UpgradeBlockNode` workflow **does
not include this step**.

During `helm upgrade`, the three-way merge sees the annotation on the live Service but not in the
chart's `service.yaml` template (which renders `service.annotations: {}`). The annotation is
stripped, MetalLB loses its signal and stops ARP-announcing the external IP — even though the pod
is perfectly healthy.

The `public-address-pool` pool is defined in `internal/templates/files/metallb/metallb.yaml` and is
provisioner-managed. No nftables or firewall rule was involved.

---

## Short-Term Fix

Add `annotateBlockNodeService` as a step in `UpgradeBlockNode`
(`internal/workflows/steps/step_block_node.go` ~line 344), between `upgradeBlockNode` and
`waitForBlockNode`:

```
UpgradeBlockNode workflow steps:
  1. upgradeBlockNode          (existing)
+ 2. annotateBlockNodeService  (add — reuses the existing builder function)
  3. waitForBlockNode          (existing)
```

This mirrors what the install workflow does and is a one-line addition to the `.Steps(...)` call.
The `annotateBlockNodeService` builder function already exists and is shared with the install
workflow.

---

## Long-Term Fix — Native Chart Annotations via hiero-block-node PR #1938

[PR #1938](https://github.com/hiero-ledger/hiero-block-node/pull/1938) (merged Dec 2025) added
`service.annotations` support to the block node Helm chart. This means the annotation can be
declared as a Helm value instead of requiring a post-deploy `kubectl patch`.

Both built-in values templates (`full-values.yaml` for production profiles and `nano-values.yaml`
for local) currently have no `service.annotations` key. The `AnnotateService` method in
`internal/blocknode/chart.go` and the `annotateBlockNodeService` workflow step in
`step_block_node.go` exist solely to work around this gap.

### How the long-term fix works inside provisioner

Following the existing pattern of `injectRetentionConfig` and `injectPersistenceOverrides` in
`internal/blocknode/values.go`, add a new `injectServiceAnnotations(profile string, valuesContent
[]byte) ([]byte, error)` method on `Manager` that:

1. Returns `valuesContent` unchanged if `profile == models.ProfileLocal` (MetalLB annotation not
   needed for local profile).
2. Navigates to `service.annotations` in the parsed values map, creating the key if absent.
3. Merges `metallb.io/address-pool: public-address-pool` without overwriting other annotations an
   operator may have set.
4. Returns the updated YAML bytes.

Then call `injectServiceAnnotations` from `ComputeValuesFile` in `values.go` (after
`injectRetentionConfig`), passing `profile` as a parameter.

Once this is in place:
- The `annotateBlockNodeService` step is removed from both `SetupBlockNode` and `UpgradeBlockNode`
  in `step_block_node.go`.
- `manager.AnnotateService()` and its step builder are removed from `chart.go` and
  `step_block_node.go`.
- `AnnotateBlockNodeServiceStepId` constant is removed.

### Should setting the external IP be optional?

**Yes.** The MetalLB annotation is only meaningful on nodes where MetalLB manages a
`public-address-pool`. This is true for production profiles (`mainnet`, `perfnet`, `testnet`,
`previewnet`) but **not for `local`**, where no public IP assignment is needed.

Optionality model:
- **`local` profile** → no annotation injected (`nano-values.yaml` already has
  `loadBalancer.enabled: false`, consistent with this)
- **All other profiles** → inject `metallb.io/address-pool: public-address-pool` into values

### Can it be done outside provisioner scope?

Partially. If a node operator supplies a custom `--values-file` that already contains:

```yaml
service:
  annotations:
    metallb.io/address-pool: public-address-pool
```

then provisioner's `readCustomValues` path preserves it through every `helm upgrade` with no
additional steps — the chart handles it natively. In that scenario, provisioner has zero work to do.

However, when operators rely on provisioner's built-in values templates (the common case, no
`--values-file`), provisioner must inject the annotation itself. So the long-term fix is squarely
within provisioner's scope — it moves the concern from a post-deploy `kubectl patch` step into the
Helm values layer, which is where it belongs.

The cleanest long-term state: provisioner injects the annotation via `injectServiceAnnotations` for
production profiles → `annotateBlockNodeService` step is deleted → install and upgrade are
idempotent by default with no post-deploy patches needed.

---

## UAT Changes

`uat:core` in `taskfiles/uat.yaml` verifies chart version and images after each upgrade but does
not verify the MetalLB annotation. Add an annotation check after each upgrade step's verify block:

```bash
echo '▶ Verify MetalLB annotation present after upgrade'
ANNOTATION=$(kubectl get svc -n block-node -o jsonpath='{.items[0].metadata.annotations.metallb\.io/address-pool}')
[ "$ANNOTATION" = "public-address-pool" ] || { echo "FAIL: metallb annotation missing, got: '$ANNOTATION'"; exit 1; }
echo "   metallb.io/address-pool: $ANNOTATION ✓"
```

Add this check after the v0.29.0 verify block and after the v0.30.2 verify block in `uat:core`.

---

## GitHub Issue

**Title:** `fix(blocknode): MetalLB annotation stripped on helm upgrade causes external IP loss`

**Labels:** `bug`, `block-node`

---

### Summary

Running `solo-provisioner block node upgrade` silently strips the MetalLB annotation
(`metallb.io/address-pool: public-address-pool`) from the block node Kubernetes Service. MetalLB
stops advertising the external IP, making the block node unreachable on port 40840 from outside the
cluster — even though the pod itself remains healthy.

### Root Cause

`SetupBlockNode` (install workflow) includes an `annotateBlockNodeService` step that patches the
live Service with `metallb.io/address-pool: public-address-pool` post-install. `UpgradeBlockNode`
does **not** include this step.

During `helm upgrade`, the three-way merge strips the annotation because the block node chart's
`service.yaml` renders with `service.annotations: {}`. MetalLB loses its signal and stops
ARP-announcing the external IP.

### Short-Term Fix

Add `annotateBlockNodeService` to the `UpgradeBlockNode` step sequence in
`internal/workflows/steps/step_block_node.go`, between `upgradeBlockNode` and `waitForBlockNode`.
The builder function already exists and is reused from the install workflow.

### Long-Term Fix

Use the native chart annotation support added in
[hiero-ledger/hiero-block-node#1938](https://github.com/hiero-ledger/hiero-block-node/pull/1938).
Add an `injectServiceAnnotations(profile, valuesContent)` method to
`internal/blocknode/values.go` (following the `injectRetentionConfig` pattern) that merges
`metallb.io/address-pool: public-address-pool` into the Helm values for non-local profiles. This
makes Helm manage the annotation natively across install and upgrade, removing the need for the
post-deploy kubectl patch step entirely.

### Acceptance Criteria

- [ ] `block node upgrade` preserves the MetalLB annotation on the Service after each upgrade
- [ ] External traffic on port 40840 is not interrupted after a successful upgrade
- [ ] `uat:core` verifies the MetalLB annotation is present after each upgrade step
- [ ] Long-term: `annotateBlockNodeService` step and `AnnotateService()` are removed in favour of
  chart-native annotation injection via `injectServiceAnnotations`

### References

- Chart annotation support: hiero-ledger/hiero-block-node#1938
- Affected code: `internal/workflows/steps/step_block_node.go` — `UpgradeBlockNode`
- Related: `internal/blocknode/chart.go` — `AnnotateService`,
  `internal/blocknode/values.go` — `ComputeValuesFile`




