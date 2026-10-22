// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// UninstallHandler handles the ActionUninstall intent for a consensus node.
// It removes the node's CRs and keeps its Secrets and volume data.
type UninstallHandler struct {
	bll.BaseHandler[models.ConsensusNodeInputs]
}

// PrepareEffectiveInputs needs only the namespace and node ID, so it skips the
// image/ledger resolution that install does.
func (h *UninstallHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.ConsensusNodeInputs],
) (*models.UserInputs[models.ConsensusNodeInputs], error) {
	resolved := inputs
	resolved.Custom.OrbitName = resolved.Custom.Namespace
	return &resolved, nil
}

// BuildWorkflow returns the uninstall workflow. Each step succeeds when its
// resource is already gone, so a re-run is a no-op.
func (h *UninstallHandler) BuildWorkflow(
	currentState state.State,
	inputs models.UserInputs[models.ConsensusNodeInputs],
) (*automa.WorkflowBuilder, error) {
	ins := inputs.Custom

	// Capsule first: it references the config CRs, and the Orbit check needs it gone.
	remove := phaseWorkflow("consensus-node-removal", "Consensus Node Removal",
		steps.DeleteConsensusCapsule(ins, steps.DefaultCapsuleKubeProvider),
		steps.DeleteConsensusConfigCRs(ins, steps.DefaultCapsuleKubeProvider),
		steps.DeleteOrbitIfLast(ins, steps.DefaultCapsuleKubeProvider),
	)

	return automa.NewWorkflowBuilder().WithId("consensus-node-uninstall").Steps(remove), nil
}

// HandleIntent delegates to the shared BaseHandler orchestration.
func (h *UninstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.ConsensusNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, nil)
}

// NewUninstallHandler creates a new UninstallHandler.
func NewUninstallHandler(base bll.BaseHandler[models.ConsensusNodeInputs]) (*UninstallHandler, error) {
	return &UninstallHandler{BaseHandler: base}, nil
}
