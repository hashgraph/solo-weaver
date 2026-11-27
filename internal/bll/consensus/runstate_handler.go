// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"

	"github.com/automa-saga/automa"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// RunStateHandler handles ActionStart and ActionStop for a consensus node. It
// sets the capsule's spec.desiredRunState; the operator starts or stops the process.
type RunStateHandler struct {
	bll.BaseHandler[models.ConsensusNodeInputs]
	desired  operatorv1alpha1.RunState
	provider steps.RunStateKubeProvider
}

// NewRunStateHandler returns the handler for start (Running) or stop (Stopped).
func NewRunStateHandler(base bll.BaseHandler[models.ConsensusNodeInputs], desired operatorv1alpha1.RunState) (*RunStateHandler, error) {
	if desired != operatorv1alpha1.RunStateRunning && desired != operatorv1alpha1.RunStateStopped {
		return nil, errorx.IllegalArgument.New("unsupported desired run state %q", desired)
	}
	return &RunStateHandler{BaseHandler: base, desired: desired, provider: steps.DefaultRunStateKubeProvider}, nil
}

// PrepareEffectiveInputs needs only the namespace and node ID.
func (h *RunStateHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.ConsensusNodeInputs],
) (*models.UserInputs[models.ConsensusNodeInputs], error) {
	resolved := inputs
	resolved.Custom.OrbitName = resolved.Custom.Namespace
	return &resolved, nil
}

// BuildWorkflow returns precheck → set desiredRunState → wait and report.
func (h *RunStateHandler) BuildWorkflow(
	currentState state.State,
	inputs models.UserInputs[models.ConsensusNodeInputs],
) (*automa.WorkflowBuilder, error) {
	ins := inputs.Custom
	id, name := "consensus-node-start", "Consensus Node Start"
	if h.desired == operatorv1alpha1.RunStateStopped {
		id, name = "consensus-node-stop", "Consensus Node Stop"
	}

	phase := phaseWorkflow(id+"-phase", name,
		steps.PrecheckConsensusRunState(ins, h.desired, h.provider),
		steps.SetConsensusRunState(ins, h.desired, h.provider),
		steps.WaitConsensusRunState(ins, h.desired, ins.ReadyTimeout, h.provider),
	)
	return automa.NewWorkflowBuilder().WithId(id).Steps(phase), nil
}

// HandleIntent delegates to the shared BaseHandler orchestration.
func (h *RunStateHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.ConsensusNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, nil)
}
