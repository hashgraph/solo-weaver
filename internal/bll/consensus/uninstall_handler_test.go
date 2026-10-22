// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMinimalUninstallHandler() *UninstallHandler {
	return &UninstallHandler{BaseHandler: bll.BaseHandler[models.ConsensusNodeInputs]{}}
}

func uninstallInputs() models.UserInputs[models.ConsensusNodeInputs] {
	return models.UserInputs[models.ConsensusNodeInputs]{
		Custom: models.ConsensusNodeInputs{Namespace: "hiero-network-1", NodeId: 2},
	}
}

// flattenStepIDs lists the leaf step IDs of a workflow, in order, descending into phases.
func flattenStepIDs(t *testing.T, stp automa.Step) []string {
	t.Helper()
	wf, ok := stp.(automa.Workflow)
	if !ok {
		return []string{stp.Id()}
	}
	var ids []string
	for _, s := range wf.Steps() {
		ids = append(ids, flattenStepIDs(t, s)...)
	}
	return ids
}

func TestUninstall_PrepareEffectiveInputs_OrbitIsNamespace(t *testing.T) {
	h := newMinimalUninstallHandler()

	eff, err := h.PrepareEffectiveInputs(models.Intent{}, uninstallInputs())

	require.NoError(t, err)
	assert.Equal(t, "hiero-network-1", eff.Custom.OrbitName)
	assert.Equal(t, int64(2), eff.Custom.NodeId)
}

// The capsule must go before the config CRs it references, and before the Orbit check.
func TestUninstall_BuildWorkflow_StepOrder(t *testing.T) {
	h := newMinimalUninstallHandler()
	eff, err := h.PrepareEffectiveInputs(models.Intent{}, uninstallInputs())
	require.NoError(t, err)

	wb, err := h.BuildWorkflow(state.State{}, *eff)
	require.NoError(t, err)
	assert.Equal(t, "consensus-node-uninstall", wb.Id())

	wf, err := wb.Build()
	require.NoError(t, err)
	assert.Equal(t, []string{
		steps.DeleteConsensusCapsuleStepId,
		steps.DeleteConsensusConfigCRsStepId,
		steps.DeleteOrbitIfLastStepId,
	}, flattenStepIDs(t, wf))
}

func TestForAction_Uninstall(t *testing.T) {
	uninstall := newMinimalUninstallHandler()
	h := &Handlers{uninstall: uninstall}

	got, err := h.ForAction(models.ActionUninstall)

	require.NoError(t, err)
	assert.Same(t, uninstall, got)
}
