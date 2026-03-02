// SPDX-License-Identifier: Apache-2.0

package bll

// Package bll (business logic layer) owns intent routing, effective-value
// resolution, and workflow orchestration.  It sits between the CLI commands
// (cmd layer) and the workflow / step layer.
//
// Shared infrastructure lives in NodeHandlerBase so that every per-node-type
// handler (BlockNode, MirrorNode, RelayNode, …) inherits it without duplication.

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// ── ActionHandler ─────────────────────────────────────────────────────────────

// ActionHandler[I] is the contract every per-action, per-node-type handler must
// satisfy.  I is the node-specific inputs struct (e.g. models.BlocknodeInputs).
//
// Splitting at this boundary means:
//   - Each handler is independently unit-testable with zero routing boilerplate.
//   - Adding a new action (e.g. ActionMigrate) is a new file, not a new switch arm.
//   - Adding a new node type (MirrorNode) is a new package, not a new God struct.
type ActionHandler[I any] interface {
	// PrepareEffectiveInputs resolves the winning value for each field from the
	// three sources (config default, current deployed state, user input) and
	// applies all field-level validators (immutability, override guards, etc.).
	// The returned inputs are fully resolved and safe to pass to BuildWorkflow.
	PrepareEffectiveInputs(inputs *models.UserInputs[I]) (*models.UserInputs[I], error)

	// BuildWorkflow validates action-level preconditions (e.g. "must be deployed
	// before upgrade") and returns the ready-to-execute WorkflowBuilder.
	BuildWorkflow(
		nodeState state.BlockNodeState,
		clusterState state.ClusterState,
		inputs *models.UserInputs[I],
	) (*automa.WorkflowBuilder, error)
}

// ── NodeHandlerBase ───────────────────────────────────────────────────────────

// NodeHandlerBase holds the three dependencies that every node-type handler
// needs: a StateManager for reading and writing state, and the rsl Registry.
//
// Embed this struct in any concrete handler to inherit RefreshRuntimeState and
// FlushNodeState without re-implementing them.
//
//	type BlockNodeHandler struct {
//	    NodeHandlerBase
//	    install   *BlockNodeInstallHandler
//	    upgrade   *BlockNodeUpgradeHandler
//	    reset     *BlockNodeResetHandler
//	    uninstall *BlockNodeUninstallHandler
//	}
type NodeHandlerBase struct {
	StateManager state.Manager
	RSL          *rsl.Registry
}

// NewNodeHandlerBase validates the two required dependencies and returns a
// populated NodeHandlerBase.  Both fields are required; any nil returns an error.
func NewNodeHandlerBase(sm state.Manager, reg *rsl.Registry) (NodeHandlerBase, error) {
	if sm == nil {
		return NodeHandlerBase{}, errorx.IllegalArgument.New("state.Manager cannot be nil")
	}
	if reg == nil {
		return NodeHandlerBase{}, errorx.IllegalArgument.New("rsl.Registry cannot be nil")
	}
	return NodeHandlerBase{StateManager: sm, RSL: reg}, nil
}

// RefreshRuntimeState pushes user inputs into rsl then force-refreshes cluster
// and the target node state.
func (b NodeHandlerBase) RefreshRuntimeState(
	target models.TargetType,
	setUserInputsFn func() error,
) error {
	if err := setUserInputsFn(); err != nil {
		return errorx.IllegalState.New("failed to push user inputs into rsl: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), rsl.DefaultRefreshTimeout)
	defer cancel()
	if err := b.RSL.Cluster.RefreshState(ctx, false); err != nil {
		return errorx.IllegalState.New("failed to refresh cluster state: %v", err)
	}

	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), rsl.DefaultRefreshTimeout)
	defer cancel()

	switch target {
	case models.TargetBlocknode:
		if err := b.RSL.BlockNode.RefreshState(ctx, false); err != nil {
			return errorx.IllegalState.New("failed to refresh block node state: %v", err)
		}
	default:
		return errorx.IllegalArgument.New("unsupported target type for runtime refresh: %s", target)
	}
	return nil
}

// FlushNodeState is the exported generic flush used by all node handlers.
func FlushNodeState[I any](
	base NodeHandlerBase,
	report *automa.Report,
	intent models.Intent,
	effectiveInputs *models.UserInputs[I],
	patchState func(full *state.State) error,
) (*automa.Report, error) {
	if report == nil {
		return nil, errorx.IllegalArgument.New("workflow report cannot be nil")
	}

	// Skip state persistence when the workflow failed in ContinueOnError mode.
	// In that mode steps may have been skipped or partially applied, so the
	// resulting cluster state is indeterminate — writing it would produce a
	// misleading state.yaml.
	//
	// StopOnError failures are NOT skipped: rollback will have run, and the
	// final state after rollback should still be persisted.
	if report.IsFailed() && effectiveInputs.Common.ExecutionOptions.ExecutionMode == automa.ContinueOnError {
		logx.As().Warn().Msg("workflow execution failed in ContinueOnError mode; skipping state persistence")
		return report, nil
	}

	base.StateManager.AddActionHistory(state.ActionHistory{
		Intent: intent,
		Inputs: effectiveInputs,
	})

	ctx, cancel := context.WithTimeout(context.Background(), rsl.DefaultRefreshTimeout)
	defer cancel()
	if base.RSL.Cluster == nil {
		return nil, errorx.IllegalState.New("rsl cluster runtime is not initialized")
	}
	if err := base.RSL.Cluster.RefreshState(ctx, true); err != nil {
		return nil, errorx.IllegalState.New("failed to refresh cluster state after workflow: %v", err)
	}

	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), rsl.DefaultRefreshTimeout)
	defer cancel()

	switch intent.Target {
	case models.TargetBlocknode:
		if base.RSL.BlockNode == nil {
			return nil, errorx.IllegalState.New("rsl block node runtime is not initialized")
		}
		if err := base.RSL.BlockNode.RefreshState(ctx, true); err != nil {
			return nil, errorx.IllegalState.New("failed to refresh block node state after workflow: %v", err)
		}
	default:
		return nil, errorx.IllegalArgument.New("unsupported target type for post-workflow refresh: %s", intent.Target)
	}

	clusterState, err := base.RSL.Cluster.CurrentState()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to read cluster state after workflow: %v", err)
	}

	fullState := base.StateManager.State()
	fullState.ClusterState = clusterState

	if patchState != nil {
		if err = patchState(&fullState); err != nil {
			return nil, err
		}
	}

	if err = base.StateManager.Set(fullState).Flush(); err != nil {
		return nil, errorx.IllegalState.New("failed to persist state after workflow: %v", err)
	}

	logx.As().Info().
		Str("state_file", fullState.StateFile).
		Any("full-state", fullState).
		Msg("Persisted state after workflow execution")

	return report, nil
}

// BlockNodePatchState returns the patchState function for block-node flushes.
func BlockNodePatchState(base NodeHandlerBase, chartRef string) func(*state.State) error {
	return func(full *state.State) error {
		bnState, err := base.RSL.BlockNode.CurrentState()
		if err != nil {
			return errorx.IllegalState.New("failed to read block node state after workflow: %v", err)
		}
		if bnState.ReleaseInfo.Status == release.StatusDeployed {
			bnState.ReleaseInfo.ChartRef = chartRef
		}
		full.BlockNodeState = bnState
		return nil
	}
}
