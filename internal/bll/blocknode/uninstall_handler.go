// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// UninstallHandler handles the ActionUninstall intent for a block node.
// It optionally purges persistent storage before removing the Helm release.
type UninstallHandler struct {
	bll.BaseHandler[models.BlockNodeInputs]
	runtime *rsl.BlockNodeRuntimeResolver
}

// PrepareEffectiveInputs for uninstall passes inputs through — no field
// resolution is required since the goal is to remove the deployment.
func (h *UninstallHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*models.UserInputs[models.BlockNodeInputs], error) {
	return resolveBlocknodeEffectiveInputs(h.runtime, intent, inputs, nil)
}

// BuildWorkflow validates that the block node is deployed (unless --force) and
// returns the uninstall workflow.
func (h *UninstallHandler) BuildWorkflow(
	currentState state.State,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.WorkflowBuilder, error) {
	if currentState.BlockNodeState.ReleaseInfo.Status != release.StatusDeployed && !inputs.Common.Force {
		return nil, errorx.IllegalState.New(
			"block node is not installed; cannot uninstall").
			WithProperty(models.ErrPropertyResolution,
				"use 'solo-provisioner block node install' to install the block node, or pass --force to continue")
	}

	ins := inputs.Custom

	// withPlaneTeardown brackets the chart-removal steps with the static-plane
	// teardown, in the reverse of install order: the daemon's traffic-shaper
	// monitor goes off first, then the workload comes down, then the plane it
	// ran on (policy → shape).
	//
	// The monitor has to go first because the daemon reads daemon.yaml only at
	// startup, so flipping the block-node component to disabled needs a restart
	// to take effect. Doing that before the pod goes away means the monitor
	// cannot re-apply the ingress $VETH HTB or reconcile policy sets against a
	// workload that is being removed. The daemon unit itself is left installed
	// and running — it is node-agnostic and may serve other components.
	//
	// Running the plane teardown after the chart removal keeps the block node's
	// classification and shaping in place for as long as its pod is serving.
	//
	// MetalLB is deliberately not torn down here: it is installed by the
	// cluster setup workflow, not by block node install, and every later block
	// node install needs it to hand the LoadBalancer service its address —
	// removing it would leave the cluster unable to install a block node again.
	// Its teardown belongs to kube cluster uninstall, which resets the cluster.
	//
	// Each step is idempotent and skips gracefully when the plane was never
	// installed or was already removed, so this runs unconditionally.
	withPlaneTeardown := func(chartSteps ...automa.Builder) []automa.Builder {
		out := []automa.Builder{
			steps.WriteBlockNodeDaemonConfigStep(models.Paths(), ins.Namespace, daemon.StatuszConfig{}, false),
			steps.RestartDaemonServiceStep(),
		}
		out = append(out, chartSteps...)
		return append(out,
			steps.NetworkPolicyDeleteAll(),
			steps.NftServiceTeardown(),
			steps.TcEgressTeardown(),
			steps.TcEgressServiceTeardown(),
			steps.TcIngressTeardown(),
		)
	}

	var wb *automa.WorkflowBuilder
	switch {
	case ins.PurgeStorage:
		// Wipe data, delete PVCs/PVs, then uninstall the chart. Order matters:
		// local-PV reclaimPolicy: Retain leaves orphaned hostPath dirs behind if
		// data isn't wiped before the PVs disappear.
		wb = automa.NewWorkflowBuilder().WithId("block-node-uninstall-purge-storage").
			Steps(withPlaneTeardown(steps.PurgeBlockNodeStorage(ins), steps.DeleteBlockNodePersistentVolumes(ins), steps.UninstallBlockNode(ins))...)
	case ins.ResetStorage:
		wb = automa.NewWorkflowBuilder().WithId("block-node-uninstall-with-reset").
			Steps(withPlaneTeardown(steps.PurgeBlockNodeStorage(ins), steps.UninstallBlockNode(ins))...)
	default:
		wb = automa.NewWorkflowBuilder().WithId("block-node-uninstall").
			Steps(withPlaneTeardown(steps.UninstallBlockNode(ins))...)
	}
	return wb, nil
}

// HandleIntent delegates to the shared BaseHandler which orchestrates all block-node intents.
func (h *UninstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, patchBlockNodeState())
}

func NewUninstallHandler(base bll.BaseHandler[models.BlockNodeInputs],
	runtimeState *rsl.BlockNodeRuntimeResolver) (*UninstallHandler, error) {
	return &UninstallHandler{BaseHandler: base, runtime: runtimeState}, nil
}
