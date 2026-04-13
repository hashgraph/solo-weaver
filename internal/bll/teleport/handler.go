// SPDX-License-Identifier: Apache-2.0

// Package teleport implements the business logic layer for teleport intents.
//
// The HandlerRegistry receives a models.Intent, delegates workflow construction
// to the appropriate per-action handler, executes the workflow, and flushes
// state to disk via BaseHandler.FlushState.
package teleport

import (
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// HandlerRegistry holds all per-action handlers for teleport intents.
// It dispatches based on TargetType (node vs cluster) and ActionType.
type HandlerRegistry struct {
	nodeInstall      *NodeInstallHandler
	nodeUninstall    *NodeUninstallHandler
	clusterInstall   *ClusterInstallHandler
	clusterUninstall *ClusterUninstallHandler
}

// NewHandlerFactory validates dependencies and returns a HandlerRegistry.
func NewHandlerFactory(
	sm state.Manager,
	runtime *rsl.RuntimeResolver,
) (*HandlerRegistry, error) {
	if sm == nil {
		return nil, errorx.IllegalArgument.New("state.Manager cannot be nil")
	}
	if runtime == nil {
		return nil, errorx.IllegalArgument.New("rsl.RuntimeResolver cannot be nil")
	}
	if runtime.TeleportRuntime == nil {
		return nil, errorx.IllegalArgument.New("rsl.RuntimeResolver.TeleportRuntime cannot be nil")
	}

	tr, ok := runtime.TeleportRuntime.(*rsl.TeleportRuntimeResolver)
	if !ok {
		return nil, errorx.IllegalArgument.New("expected TeleportRuntime to be *rsl.TeleportRuntimeResolver but got %T", runtime.TeleportRuntime)
	}

	nodeBase, err := bll.NewBaseHandler[models.TeleportNodeInputs](runtime, models.TargetTeleportNode)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create node BaseHandler: %v", err)
	}

	clusterBase, err := bll.NewBaseHandler[models.TeleportClusterInputs](runtime, models.TargetTeleportCluster)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create cluster BaseHandler: %v", err)
	}

	return &HandlerRegistry{
		nodeInstall:      NewNodeInstallHandler(nodeBase, tr),
		nodeUninstall:    NewNodeUninstallHandler(nodeBase, tr),
		clusterInstall:   NewClusterInstallHandler(clusterBase, tr),
		clusterUninstall: NewClusterUninstallHandler(clusterBase, tr),
	}, nil
}

// ForNodeAction returns the handler for a teleport node action.
func (h *HandlerRegistry) ForNodeAction(
	action models.ActionType,
) (bll.IntentHandler[models.TeleportNodeInputs], error) {
	switch action {
	case models.ActionInstall:
		return h.nodeInstall, nil
	case models.ActionUninstall:
		return h.nodeUninstall, nil
	default:
		return nil, errorx.IllegalArgument.New("unsupported action %q for teleport node", action)
	}
}

// ForClusterAction returns the handler for a teleport cluster action.
func (h *HandlerRegistry) ForClusterAction(
	action models.ActionType,
) (bll.IntentHandler[models.TeleportClusterInputs], error) {
	switch action {
	case models.ActionInstall:
		return h.clusterInstall, nil
	case models.ActionUninstall:
		return h.clusterUninstall, nil
	default:
		return nil, errorx.IllegalArgument.New("unsupported action %q for teleport cluster", action)
	}
}
