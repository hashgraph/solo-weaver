// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	bnpkg "github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// InstallHandler handles the ActionInstall intent for a block node.
// It resolves effective inputs (applying RequiresExplicitOverride guards so
// fields already set by a running deployment cannot be silently overridden),
// then builds an install workflow that optionally bootstraps the cluster first.
type InstallHandler struct {
	bll.BaseHandler[models.BlockNodeInputs]
	runtime *rsl.BlockNodeRuntimeResolver
	mr      software.MachineRuntime
}

// PrepareEffectiveInputs resolves the winning value for every block-node field.
// For each field the priority is: StrategyCurrent > StrategyUserInput > StrategyConfig.
// RequiresExplicitOverride fires when the user supplied a value but the current
// deployed state already owns that field and --force is set — preventing silent
// overwrites during a plain install.
func (h *InstallHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*models.UserInputs[models.BlockNodeInputs], error) {
	return resolveBlocknodeEffectiveInputs(h.runtime, intent, inputs, nil)
}

// BuildWorkflow validates install preconditions and returns the workflow.
// If the cluster has already been created only the block node setup step is
// included; otherwise the full cluster bootstrap is prepended.
func (h *InstallHandler) BuildWorkflow(
	currentState state.State,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.WorkflowBuilder, error) {
	if currentState.BlockNodeState.ReleaseInfo.Status == release.StatusDeployed && !inputs.Common.Force {
		return nil, errorx.IllegalState.New(
			"block node is already installed; cannot install again").
			WithProperty(models.ErrPropertyResolution,
				"use 'solo-provisioner block node reset' or 'solo-provisioner block node upgrade', or pass --force to continue")
	}

	// Fail fast if storage paths can't be resolved — don't set up a cluster for nothing.
	if err := bnpkg.ValidateStorageCompleteness(inputs.Custom.Storage, inputs.Custom.ChartVersion); err != nil {
		return nil, err
	}

	ins := inputs.Custom

	var wb *automa.WorkflowBuilder
	if currentState.ClusterState.Created {
		wb = automa.NewWorkflowBuilder().WithId("block-node-install").
			Steps(
				// Backwards-compat guard: older released binaries did not create
				// weaver:2500 during self-install, so the account may be absent on
				// machines upgraded from an old binary. The global pre-run check only
				// verifies the binary exists, not the user account. This step is
				// idempotent and safe to repeat on up-to-date installations.
				steps.EnsureWeaverOwnerStep(),
				// Static network plane (host firewall + weaver policy persistence +
				// $EGRESS/$VETH tc shape config), grouped as the "Network Setup" phase.
				// The host firewall is owned by the block-node workflow (not the generic
				// kube cluster install); nftables is already installed/enabled by prior
				// cluster provisioning, so it's safe to apply here.
				workflows.NetworkSetupWorkflow(ins.EgressInterface, ins.LinkRate),
				steps.SetupBlockNode(ins),
				// Enable the traffic-shaper monitor in daemon.yaml so it starts
				// reconciling statusz-driven nft membership once the daemon runs.
				workflows.BlockNodeDaemonConfigWorkflow(ins.Namespace),
			)
	} else {
		wb = automa.NewWorkflowBuilder().WithId("block-node-install").
			Steps(
				// Same backwards-compat guard as above: ensure weaver:2500 exists
				// before the preflight check validates it. Older binaries did not
				// create this account during self-install.
				steps.EnsureWeaverOwnerStep(),
				// Block node install owns its workload-sized preflight (block provider,
				// profile, plugin preset) plus system setup, then stands up Kubernetes.
				// InstallClusterWorkflow is intentionally not reused here: it validates
				// only the substrate floor, which is weaker than the block-node floor.
				workflows.NodeSetupWorkflow(models.NodeTypeBlock, ins.Profile, ins.PluginPreset, ins.SkipHardwareChecks),
				workflows.KubernetesSetupWorkflow(h.mr),
				// Static network plane (host firewall + weaver policy persistence +
				// $EGRESS/$VETH tc shape config), grouped as the "Network Setup" phase.
				// nftables was just installed/enabled by NodeSetupWorkflow's
				// systemSetupWorkflow, so applying the host firewall here (rather than
				// in that generic, node-type-agnostic workflow) is safe.
				workflows.NetworkSetupWorkflow(ins.EgressInterface, ins.LinkRate),
				steps.SetupBlockNode(ins),
				// Enable the traffic-shaper monitor in daemon.yaml so it starts
				// reconciling statusz-driven nft membership once the daemon runs.
				workflows.BlockNodeDaemonConfigWorkflow(ins.Namespace),
			)
	}
	return wb, nil
}

// HandleIntent delegates to the shared BaseHandler which orchestrates all block-node intents.
func (h *InstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, patchBlockNodeState())
}

func NewInstallHandler(
	base bll.BaseHandler[models.BlockNodeInputs],
	runtime *rsl.BlockNodeRuntimeResolver,
	mr software.MachineRuntime) (*InstallHandler, error) {
	return &InstallHandler{BaseHandler: base, runtime: runtime, mr: mr}, nil
}
