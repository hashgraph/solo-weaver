// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

// InstallHandler handles the ActionInstall intent for a consensus node.
type InstallHandler struct {
	bll.BaseHandler[models.ConsensusNodeInputs]
	runtime *rsl.ConsensusNodeRuntimeResolver
	mr      software.MachineRuntime
}

// PrepareEffectiveInputs resolves the winning value for each field from the
// available sources (Reality > State > UserInput > Config > Default).
func (h *InstallHandler) PrepareEffectiveInputs(
	intent models.Intent,
	inputs models.UserInputs[models.ConsensusNodeInputs],
) (*models.UserInputs[models.ConsensusNodeInputs], error) {
	l := logx.As()

	// Orbit-qualified key for the local state/reality maps so the same nodeId in
	// different orbits does not collide (see models.ConsensusNodeStateKey).
	stateKey := models.ConsensusNodeStateKey(inputs.Custom.Namespace, inputs.Custom.NodeId)

	// Seed sources: user inputs, deployment package, and per-node state/reality.
	// State comes from persisted state.yaml; Reality from the cluster (post-RefreshState).
	h.runtime.WithUserInputs(inputs.Custom)
	h.runtime.WithDeploymentPackage(inputs.Custom.DeploymentPackageDir, inputs.Custom.NodeId)

	persisted := h.runtime.PersistedNodes()
	if ps, ok := persisted[stateKey]; ok {
		h.runtime.SetStateForNode(stateKey, ps)
	}

	realityNodes, err := h.runtime.CurrentState()
	if err != nil {
		return nil, errx.Decorate(
			errorx.IllegalState.Wrap(err, "failed to read current consensus node state"),
			reasons.PreconditionNotMet,
			"Verify the cluster is reachable (kubectl get nodes) and the solo-operator CRDs are installed")
	}
	if rs, ok := realityNodes[stateKey]; ok {
		h.runtime.SetRealityForNode(stateKey, rs)
	}

	// Resolve the identity scalars. DefaultSelector never errors, so .Get() is
	// safe without an error check.
	resolved := inputs
	custom := &resolved.Custom

	custom.ConsensusImageRepo = h.runtime.ImageRepo().Get().Val()
	custom.ConsensusImageTag = h.runtime.ImageTag().Get().Val()
	custom.LedgerId = h.runtime.LedgerId().Get().Val()
	custom.ChainId = h.runtime.ChainId().Get().Val()

	// Resolve the config-file contents (deployment package over embedded default).
	h.runtime.ResolveConfigContents(inputs.Custom.DeploymentPackageDir, inputs.Custom.NodeId, custom)

	// Validate required fields after resolution
	if custom.ConsensusImageRepo == "" || custom.ConsensusImageTag == "" {
		return nil, errx.Decorate(
			errorx.IllegalArgument.New(
				"image-repo and image-tag could not be resolved — provide --image-repo/--image-tag or --deployment-package-dir"),
			reasons.InvalidArgument,
			"Provide both --image-repo and --image-tag",
			"Or pass --deployment-package-dir pointing at an extracted HIP-1494 deployment package")
	}
	if custom.LedgerId == "" {
		return nil, errx.Decorate(
			errorx.IllegalArgument.New(
				"ledger-id could not be resolved — provide --ledger-id or --deployment-package-dir"),
			reasons.InvalidArgument,
			"Provide --ledger-id (e.g. 0x00 for mainnet)",
			"Or pass --deployment-package-dir pointing at an extracted HIP-1494 deployment package")
	}

	// Apply namespace=orbit convention
	custom.OrbitName = custom.Namespace

	l.Info().
		Str("imageRepo", custom.ConsensusImageRepo).
		Str("imageTag", custom.ConsensusImageTag).
		Str("ledgerId", custom.LedgerId).
		Str("chainId", custom.ChainId).
		Msg("Resolved effective consensus node inputs")

	return &resolved, nil
}

// BuildWorkflow constructs the install workflow from resolved inputs.
func (h *InstallHandler) BuildWorkflow(
	currentState state.State,
	inputs models.UserInputs[models.ConsensusNodeInputs],
) (*automa.WorkflowBuilder, error) {
	ins := inputs.Custom

	// The consensus node is deployed via solo-operator CRs into a running
	// Kubernetes cluster. When no cluster exists yet, bootstrap it first —
	// mirroring block node install — so a fresh host reaches a running consensus
	// node in one command. When a cluster is already present, skip straight to
	// the operator/CR steps.
	var stepList []automa.Builder
	if !currentState.ClusterState.Created {
		stepList = append(stepList,
			// Consensus-sized preflight (hardware floor for the resolved profile)
			// plus system setup, then stand up Kubernetes.
			workflows.NodeSetupWorkflow(models.NodeTypeConsensus, ins.Profile, "", ins.SkipHardwareChecks),
			workflows.KubernetesSetupWorkflow(h.mr),
		)
	}

	stepList = append(stepList,
		// Secret references depend only on the cluster (not the operator), so check
		// them first — this fails fast on missing secrets before the ~21s operator
		// install and operator prechecks. In the bootstrap case it runs right after
		// the cluster comes up (the earliest point a cluster query is meaningful).
		steps.PrecheckConsensusSecrets(ins),
		steps.InstallSoloOperator(ins.UpgradeOperator),
		steps.PrecheckOperatorCRDs(steps.ConsensusNodeCRDs...),
		steps.PrecheckOperatorRunning(),
		steps.PrecheckOperatorVersion(),
		steps.EnsureOrbit(ins, steps.DefaultCapsuleKubeProvider),
		steps.EnsureConfigCRs(ins, inputs.Common.Force, steps.DefaultCapsuleKubeProvider),
		steps.CreateConsensusCapsule(ins, steps.DefaultCapsuleKubeProvider),
		// Report the node's current status (non-blocking) so the operator sees the
		// phase and next step. Install does not wait for Running: a fresh network is
		// unblocked out-of-band by `consensus network genesis`, and Manual nodes stay
		// Stopped until `consensus node start`. Live readiness is `node status`' job.
		steps.ReportConsensusCapsuleStatus(ins, steps.DefaultCapsuleKubeProvider),
	)

	wb := automa.NewWorkflowBuilder().WithId("consensus-node-install").Steps(stepList...)

	return wb, nil
}

// HandleIntent delegates to the shared BaseHandler orchestration.
func (h *InstallHandler) HandleIntent(
	ctx context.Context,
	intent models.Intent,
	inputs models.UserInputs[models.ConsensusNodeInputs],
) (*automa.Report, error) {
	return h.BaseHandler.HandleIntent(ctx, intent, inputs, h, patchConsensusNodeState())
}

// patchConsensusNodeState returns a callback that persists the consensus node
// state into the full state map after workflow execution.
func patchConsensusNodeState() func(full *state.State, effInputs models.UserInputs[models.ConsensusNodeInputs]) error {
	return func(full *state.State, effInputs models.UserInputs[models.ConsensusNodeInputs]) error {
		ins := effInputs.Custom
		stateKey := models.ConsensusNodeStateKey(ins.Namespace, ins.NodeId)

		if full.ConsensusNodes == nil {
			full.ConsensusNodes = make(map[string]state.ConsensusNodeState)
		}

		configHashes := make(map[string]state.ConfigHashEntry)
		now := htime.Now()

		// Record each config's hash tagged with its actual per-file source
		// (package vs embedded), so a later run can tell why a hash changed.
		addHash := func(key, content string) {
			if content == "" {
				return
			}
			source := ins.ConfigSources[key]
			if source == "" {
				source = models.ConfigSourceEmbedded
			}
			h := sha256.Sum256([]byte(content))
			configHashes[key] = state.ConfigHashEntry{
				Hash:       fmt.Sprintf("%x", h),
				Source:     source,
				LastUpdate: now,
			}
		}

		addHash(models.ConfigKeyLog4j2, ins.ConfigLog4j2)
		addHash(models.ConfigKeySettings, ins.ConfigSettings)
		addHash(models.ConfigKeyAppProperties, ins.ConfigAppProperties)
		addHash(models.ConfigKeyAppOverride, ins.ConfigAppOverrideProperties)
		addHash(models.ConfigKeyApiPermission, ins.ConfigApiPermission)
		addHash(models.ConfigKeyBootstrap, ins.ConfigBootstrap)
		addHash(models.ConfigKeyNodeProperties, ins.ConfigNodeProperties)
		addHash(models.ConfigKeyFeeSchedules, ins.ConfigFeeSchedules)
		addHash(models.ConfigKeySimpleFeesSchedules, ins.ConfigSimpleFeesSchedules)
		addHash(models.ConfigKeyThrottles, ins.ConfigThrottles)
		addHash(models.ConfigKeyBlockNodes, ins.ConfigBlockNodes)

		full.ConsensusNodes[stateKey] = state.ConsensusNodeState{
			Namespace:     ins.Namespace,
			OrbitName:     ins.OrbitName,
			NodeId:        ins.NodeId,
			AccountId:     ins.AccountId,
			Weight:        ins.Weight,
			ImageRepo:     ins.ConsensusImageRepo,
			ImageTag:      ins.ConsensusImageTag,
			LedgerId:      ins.LedgerId,
			ChainId:       ins.ChainId,
			DeploymentPkg: ins.DeploymentPackageDir,
			GrpcTlsSecret: ins.GrpcTlsSecret,
			SigningSecret: ins.SigningSecret,
			ConfigHashes:  configHashes,
			LastSync:      now,
		}

		logx.As().Info().Str("stateKey", stateKey).Msg("Persisted consensus node state")
		return nil
	}
}

// NewInstallHandler creates a new InstallHandler.
func NewInstallHandler(
	base bll.BaseHandler[models.ConsensusNodeInputs],
	runtime *rsl.ConsensusNodeRuntimeResolver,
	mr software.MachineRuntime,
) (*InstallHandler, error) {
	return &InstallHandler{BaseHandler: base, runtime: runtime, mr: mr}, nil
}
