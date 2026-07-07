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
// preflight runs privilege + weaver-user + the single substrate hardware step, and
// deliberately omits the node-type/profile host-profile validation.
func TestNewSubstrateSafetyCheckWorkflow_OmitsHostProfileStep(t *testing.T) {
	ids := substrateStepIDs(t, false)

	require.Contains(t, ids, "validate-privileges")
	require.Contains(t, ids, "validate-weaver-user")
	require.Contains(t, ids, "validate-substrate-hardware")
	require.NotContains(t, ids, "validate-host-profile",
		"substrate preflight must not validate node type / profile")
}

// TestNewSubstrateSafetyCheckWorkflow_SkipsHardwareWhenRequested verifies that
// --skip-hardware-checks excludes the substrate hardware step while keeping the
// non-hardware safety checks.
func TestNewSubstrateSafetyCheckWorkflow_SkipsHardwareWhenRequested(t *testing.T) {
	ids := substrateStepIDs(t, true)

	require.Contains(t, ids, "validate-privileges")
	require.Contains(t, ids, "validate-weaver-user")
	require.NotContains(t, ids, "validate-substrate-hardware",
		"hardware step must be excluded when skipHardwareChecks is true")
}
