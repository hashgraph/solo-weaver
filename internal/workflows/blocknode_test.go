// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockNodePreflightStepIDs builds the block-node preflight workflow and
// returns the ordered IDs of its immediate child steps.
func blockNodePreflightStepIDs(t *testing.T, spec hardware.DeploymentSpec, storage models.BlockNodeStorage, chartVersion string) []string {
	t.Helper()
	built, err := NewBlockNodePreflightCheckWorkflow(spec, storage, chartVersion).Build()
	require.NoError(t, err)

	wf, ok := built.(automa.Workflow)
	require.True(t, ok, "expected built block node preflight to be an automa.Workflow")

	ids := make([]string, 0, len(wf.Steps()))
	for _, s := range wf.Steps() {
		ids = append(ids, s.Id())
	}
	return ids
}

// TestNewBlockNodePreflightCheckWorkflow_RunsStorageMediaCheckFirst verifies
// `block node check` runs the warn-only storage media check before the shared
// node safety checks: the workflow stops on the first failure, so a failed
// hardware check must not skip the warning.
func TestNewBlockNodePreflightCheckWorkflow_RunsStorageMediaCheckFirst(t *testing.T) {
	ids := blockNodePreflightStepIDs(t, hardware.DeploymentSpec{}, models.BlockNodeStorage{BasePath: "/opt/hedera/blocknode"}, "0.37.0")

	require.NotEmpty(t, ids)
	assert.Equal(t, steps.CheckStorageMediaStepId, ids[0],
		"storage media check must run before the shared node safety checks")
	assert.Contains(t, ids, "validate-privileges", "must still run the shared node safety checks")
}

// TestNewNodeSafetyCheckWorkflow_HasNoStorageMediaCheck verifies the shared
// workflow used by cluster/consensus/alloy checks stays free of the block-node step.
func TestNewNodeSafetyCheckWorkflow_HasNoStorageMediaCheck(t *testing.T) {
	built, err := NewNodeSafetyCheckWorkflow(hardware.DeploymentSpec{NodeType: models.NodeTypeBlock}, false).Build()
	require.NoError(t, err)
	wf, ok := built.(automa.Workflow)
	require.True(t, ok)

	for _, s := range wf.Steps() {
		assert.NotEqual(t, steps.CheckStorageMediaStepId, s.Id())
	}
}

// TestNewBlockNodePreflightCheckWorkflow_ZeroStorageStillBuilds covers the
// degrade path documented on the function: `check` passes the zero storage
// when it could not resolve the effective values, and the workflow must still
// build — the media step itself is responsible for skipping cleanly, not the
// workflow construction.
func TestNewBlockNodePreflightCheckWorkflow_ZeroStorageStillBuilds(t *testing.T) {
	ids := blockNodePreflightStepIDs(t, hardware.DeploymentSpec{}, models.BlockNodeStorage{}, "")

	assert.Contains(t, ids, steps.CheckStorageMediaStepId)
}

// TestNewBlockNodePreflightCheckWorkflow_DefaultsNodeType verifies an unset
// NodeType defaults to block: the host-profile step accepts it, where an
// empty NodeType would fail hardware.IsValidNodeType.
func TestNewBlockNodePreflightCheckWorkflow_DefaultsNodeType(t *testing.T) {
	built, err := NewBlockNodePreflightCheckWorkflow(
		hardware.DeploymentSpec{Profile: models.ProfileLocal},
		models.BlockNodeStorage{}, "").Build()
	require.NoError(t, err)
	wf, ok := built.(automa.Workflow)
	require.True(t, ok)

	var hostProfile automa.Step
	for _, s := range wf.Steps() {
		if s.Id() == "validate-host-profile" {
			hostProfile = s
		}
	}
	require.NotNil(t, hostProfile, "expected a validate-host-profile step")

	report := hostProfile.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status,
		"an empty NodeType would fail hardware.IsValidNodeType; success means it defaulted to block")
}
