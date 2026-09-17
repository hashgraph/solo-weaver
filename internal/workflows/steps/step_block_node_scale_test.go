// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetStepIDs builds the reset workflow for the given inputs and returns its
// child step IDs. Building does not touch the cluster — the manager provider is
// lazy and only resolves inside Execute.
func resetStepIDs(t *testing.T, inputs models.BlockNodeInputs) []string {
	t.Helper()

	stp, err := ResetBlockNode(inputs).Build()
	require.NoError(t, err)

	wf, ok := stp.(automa.Workflow)
	require.True(t, ok, "expected a Workflow, got %T", stp)

	ids := make([]string, 0, len(wf.Steps()))
	for _, s := range wf.Steps() {
		ids = append(ids, s.Id())
	}
	return ids
}

// TestResetBlockNode_LeaveScaledDown pins both halves of --no-scale-up on the reset
// workflow. The zero value has to keep the scale-up steps: BlockNodeInputs is
// built in several places that never set LeaveScaledDown, and an inverted default
// would silently leave operators with a stopped node.
func TestResetBlockNode_LeaveScaledDown(t *testing.T) {
	inputs := models.BlockNodeInputs{
		Namespace: "block-node-ns",
		Release:   "block-node",
		Storage:   models.BlockNodeStorage{BasePath: "/mnt/storage"},
	}

	t.Run("default scales back up", func(t *testing.T) {
		assert.Equal(t, []string{
			ScaleDownBlockNodeStepId,
			WaitForBlockNodeTerminatedStepId,
			ClearBlockNodeStorageStepId,
			ScaleUpBlockNodeStepId,
			WaitForBlockNodeStepId,
		}, resetStepIDs(t, inputs))
	})

	t.Run("LeaveScaledDown stops after clearing storage", func(t *testing.T) {
		down := inputs
		down.LeaveScaledDown = true

		assert.Equal(t, []string{
			ScaleDownBlockNodeStepId,
			WaitForBlockNodeTerminatedStepId,
			ClearBlockNodeStorageStepId,
		}, resetStepIDs(t, down))
	})
}

// TestScaleDownBlockNodeAfterUpgrade_DistinctStepId guards the reason the
// after-upgrade scale-down does not reuse ScaleDownBlockNodeStepId: a
// reconfigure/upgrade that ends at 0 replicas puts both scale-downs in one
// workflow, and two steps sharing an id collide.
func TestScaleDownBlockNodeAfterUpgrade_DistinctStepId(t *testing.T) {
	stp, err := ScaleDownBlockNodeAfterUpgrade(models.BlockNodeInputs{
		Namespace: "block-node-ns",
		Release:   "block-node",
	}).Build()
	require.NoError(t, err)

	assert.Equal(t, ScaleDownAfterUpgradeStepId, stp.Id())
	assert.NotEqual(t, ScaleDownBlockNodeStepId, ScaleDownAfterUpgradeStepId)
}
