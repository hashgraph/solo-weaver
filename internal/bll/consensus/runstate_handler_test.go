// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus

import (
	"testing"

	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestRunStateIntentsAreValidOnlyForConsensusNode(t *testing.T) {
	for _, action := range []models.ActionType{models.ActionStart, models.ActionStop} {
		ok := models.Intent{Action: action, Target: models.TargetConsensusNode}
		require.True(t, ok.IsValid(), "%s consensus", action)
		bad := models.Intent{Action: action, Target: models.TargetBlockNode}
		require.False(t, bad.IsValid(), "%s blocknode", action)
	}
}

func TestNewRunStateHandler_RejectsUnknownState(t *testing.T) {
	_, err := NewRunStateHandler(newTestHandlers(t).uninstall.BaseHandler, operatorv1alpha1.RunState("Paused"))
	require.Error(t, err)
}

func TestRunStateHandler_PrepareEffectiveInputsSetsOrbit(t *testing.T) {
	h := newTestHandlers(t)
	in := models.UserInputs[models.ConsensusNodeInputs]{Custom: models.ConsensusNodeInputs{Namespace: "hiero-network-2", NodeId: 1}}
	eff, err := h.start.PrepareEffectiveInputs(models.Intent{Action: models.ActionStart, Target: models.TargetConsensusNode}, in)
	require.NoError(t, err)
	require.Equal(t, "hiero-network-2", eff.Custom.OrbitName)
}

func TestRunStateHandler_BuildWorkflow(t *testing.T) {
	h := newTestHandlers(t)
	tests := []struct {
		handler *RunStateHandler
		id      string
	}{
		{h.start, "consensus-node-start"},
		{h.stop, "consensus-node-stop"},
	}
	for _, tt := range tests {
		wb, err := tt.handler.BuildWorkflow(state.State{}, models.UserInputs[models.ConsensusNodeInputs]{
			Custom: models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns"},
		})
		require.NoError(t, err)
		wf, err := wb.Build()
		require.NoError(t, err)
		require.Equal(t, tt.id, wf.Id())
	}
}
