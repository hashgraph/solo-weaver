// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// UninstallHandler handles the ActionUninstall intent for a consensus node.
// It removes the node's CRs and keeps its Secrets and volume data.
type UninstallHandler struct {
	bll.BaseHandler[models.ConsensusNodeInputs]
	provider steps.CapsuleKubeProvider
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
		steps.DeleteConsensusCapsule(ins, h.provider),
		steps.DeleteConsensusConfigCRs(ins, h.provider),
		steps.DeleteOrbitIfLast(ins, h.provider),
	)

	return automa.NewWorkflowBuilder().WithId("consensus-node-uninstall").Steps(remove), nil
}

// HandleIntent delegates to the shared BaseHandler orchestration.
func (h *UninstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.ConsensusNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, pruneUninstalledConsensusState(ctx, h.provider))
}

// pruneUninstalledConsensusState returns a callback that drops the node, and its Orbit
// baseline, from state only when the cluster confirms they are gone. On any doubt it keeps state.
func pruneUninstalledConsensusState(
	ctx context.Context,
	provider steps.CapsuleKubeProvider,
) func(full *state.State, effInputs models.UserInputs[models.ConsensusNodeInputs]) error {
	return func(full *state.State, effInputs models.UserInputs[models.ConsensusNodeInputs]) error {
		ins := effInputs.Custom
		stateKey := models.ConsensusNodeStateKey(ins.Namespace, ins.NodeId)
		capsuleName := models.ConsensusCapsuleName(ins.OrbitName, ins.NodeId)
		l := logx.As().With().Str("stateKey", stateKey).Logger()

		kc, err := provider(ctx)
		if err != nil {
			l.Warn().Err(err).Msg("Cannot reach the cluster; keeping consensus node in state")
			return nil
		}

		capsuleGone, err := resourceGone(ctx, kc, kube.KindConsensusCapsule, ins.Namespace, capsuleName)
		if err != nil || !capsuleGone {
			l.Warn().Err(err).Str("capsule", capsuleName).Msg("ConsensusCapsule not confirmed gone; keeping consensus node in state")
			return nil
		}
		delete(full.ConsensusNodes, stateKey)
		l.Info().Msg("Removed consensus node from state")

		orbitGone, err := resourceGone(ctx, kc, kube.KindOrbit, "", ins.OrbitName)
		if err != nil || !orbitGone {
			return nil
		}
		delete(full.ConsensusOrbits, ins.OrbitName)
		l.Info().Str("orbit", ins.OrbitName).Msg("Removed Orbit from state")
		return nil
	}
}

// resourceGone reports whether a solo-operator CR is absent. An absent CRD counts as absent.
func resourceGone(ctx context.Context, kc steps.CapsuleKubeClient, kind kube.ResourceKind, namespace, name string) (bool, error) {
	exists, err := kc.ResourceExists(ctx, kube.SoloOperatorGroup+"/"+kube.SoloOperatorVersion, string(kind), namespace, name)
	if err != nil {
		if kube.IsKindNotServed(err) {
			return true, nil
		}
		return false, err
	}
	return !exists, nil
}

// NewUninstallHandler creates a new UninstallHandler backed by the live cluster.
func NewUninstallHandler(base bll.BaseHandler[models.ConsensusNodeInputs]) (*UninstallHandler, error) {
	return &UninstallHandler{BaseHandler: base, provider: steps.DefaultCapsuleKubeProvider}, nil
}
