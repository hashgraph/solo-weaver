// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
)

// substrateStepIDs builds the substrate safety-check workflow and returns the
// ordered IDs of its immediate child steps.
func substrateStepIDs(t *testing.T, skipHardwareChecks bool) []string {
	t.Helper()
	built, err := NewSubstrateSafetyCheckWorkflow(skipHardwareChecks).Build()
	require.NoError(t, err)

	wf, ok := built.(automa.Workflow)
	require.True(t, ok, "expected built substrate preflight to be an automa.Workflow")

	ids := make([]string, 0, len(wf.Steps()))
	for _, s := range wf.Steps() {
		ids = append(ids, s.Id())
	}
	return ids
}

// TestNewSubstrateSafetyCheckWorkflow_OmitsHostProfileStep verifies the substrate
// preflight runs privilege + weaver-user + the same four per-resource hardware steps as
// the node preflight, and deliberately omits the node-type/profile host-profile validation.
func TestNewSubstrateSafetyCheckWorkflow_OmitsHostProfileStep(t *testing.T) {
	ids := substrateStepIDs(t, false)

	require.Contains(t, ids, "validate-privileges")
	require.Contains(t, ids, "validate-weaver-user")
	// Per-resource hardware steps, each surfacing independently (matches node preflight).
	require.Contains(t, ids, "validate-os")
	require.Contains(t, ids, "validate-cpu")
	require.Contains(t, ids, "validate-memory")
	require.Contains(t, ids, "validate-storage")
	require.NotContains(t, ids, "validate-host-profile",
		"substrate preflight must not validate node type / profile")
}

// TestNewSubstrateSafetyCheckWorkflow_SkipsHardwareWhenRequested verifies that
// --skip-hardware-checks excludes the per-resource hardware steps while keeping the
// non-hardware safety checks.
func TestNewSubstrateSafetyCheckWorkflow_SkipsHardwareWhenRequested(t *testing.T) {
	ids := substrateStepIDs(t, true)

	require.Contains(t, ids, "validate-privileges")
	require.Contains(t, ids, "validate-weaver-user")
	for _, hw := range []string{"validate-os", "validate-cpu", "validate-memory", "validate-storage"} {
		require.NotContains(t, ids, hw,
			"hardware steps must be excluded when skipHardwareChecks is true")
	}
}
