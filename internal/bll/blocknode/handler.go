// SPDX-License-Identifier: Apache-2.0

// Package blocknode implements the business logic layer for block-node intents.
//
// The routing Handler receives a models.Intent, delegates preparation of
// effective inputs and workflow construction to the appropriate per-action
// handler, executes the workflow, and flushes state to disk.
//
// Extending with a new action: implement IntentHandler[models.BlockNodeInputs]
// in a new file and add a case to Handler.HandleIntent.  No other file changes.
package blocknode

import (
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// Handlers is the private struct that holds all per-action handlers for block-node intents.
type Handlers struct {
	install *InstallHandler
	// installWithCluster also stands up Kubernetes, so it owns the system
	// record as well — see ForAction.
	installWithCluster *InstallHandler
	upgrade            *UpgradeHandler
	reconfigure        *ReconfigureHandler
	reset              *ResetHandler
	uninstall          *UninstallHandler
	runtime            *rsl.BlockNodeRuntimeResolver
}

// NewHandlerFactory validates dependencies and returns a Handlers with all handlers initialized.
// All dependencies are required; any nil returns an error.
func NewHandlerFactory(
	runtime *rsl.RuntimeResolver,
) (*Handlers, error) {
	// Each intent owns only the files it writes: install records the profile
	// and the host-firewall decision (and the system record too when it also
	// stands up Kubernetes), reconfigure re-asserts the firewall, and upgrade,
	// reset and uninstall touch the block node alone.
	profile := bll.WithProfileExtractor(func(i models.BlockNodeInputs) string { return i.Profile })
	managed := bll.WithManagedComponents[models.BlockNodeInputs]

	installBase, err := bll.NewBaseHandler(runtime, models.TargetBlockNode, profile,
		managed(bll.BlockNode, bll.Profile, bll.Firewall))
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create install BaseHandler: %v", err)
	}
	clusterInstallBase, err := bll.NewBaseHandler(runtime, models.TargetBlockNode, profile,
		managed(bll.BlockNode, bll.Profile, bll.Firewall, bll.System))
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create cluster install BaseHandler: %v", err)
	}
	reconfigureBase, err := bll.NewBaseHandler(runtime, models.TargetBlockNode,
		managed(bll.BlockNode, bll.Firewall))
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create reconfigure BaseHandler: %v", err)
	}
	base, err := bll.NewBaseHandler(runtime, models.TargetBlockNode, managed(bll.BlockNode))
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create BaseHandler: %v", err)
	}

	if runtime == nil {
		return nil, errorx.IllegalArgument.New("RuntimeResolver cannot be nil")
	}

	if runtime.BlockNodeRuntime == nil {
		return nil, errorx.IllegalArgument.New("runtime.BlockNodeRuntime is nil")
	}

	bnr, ok := runtime.BlockNodeRuntime.(*rsl.BlockNodeRuntimeResolver)
	if !ok {
		return nil, errorx.IllegalArgument.New("expected BlockNodeRuntime to be *rsl.BlockNodeRuntimeResolver but got %T", runtime.BlockNodeRuntime)
	}

	mr, ok := runtime.MachineRuntime.(*rsl.MachineRuntimeResolver)
	if !ok {
		return nil, errorx.IllegalArgument.New("expected MachineRuntime to be *rsl.MachineRuntimeResolver but got %T", runtime.MachineRuntime)
	}

	installHandler, err := NewInstallHandler(installBase, bnr, mr)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create InstallHandler: %v", err)
	}

	installWithClusterHandler, err := NewInstallHandler(clusterInstallBase, bnr, mr)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create InstallHandler: %v", err)
	}

	upgradeHandler, err := NewUpgradeHandler(base, bnr)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create UpgradeHandler: %v", err)
	}

	reconfigureHandler, err := NewReconfigureHandler(reconfigureBase, bnr)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create ReconfigureHandler: %v", err)
	}

	resetHandler, err := NewResetHandler(base, bnr)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create ResetHandler: %v", err)
	}

	uninstallHandler, err := NewUninstallHandler(base, bnr)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create UninstallHandler: %v", err)
	}

	h := &Handlers{
		install:            installHandler,
		installWithCluster: installWithClusterHandler,
		upgrade:            upgradeHandler,
		reconfigure:        reconfigureHandler,
		reset:              resetHandler,
		uninstall:          uninstallHandler,
		runtime:            bnr,
	}

	return h, nil
}

// Runtime returns the concrete block-node resolver the handlers were built on.
// `block node check` reads Storage() and ChartVersion() from it, which the
// generic rsl.Resolver interface does not expose.
func (h *Handlers) Runtime() *rsl.BlockNodeRuntimeResolver {
	return h.runtime
}

// ForAction returns the appropriate IntentHandler for the given action, or an error if the action is unsupported.
func (h *Handlers) ForAction(
	action models.ActionType,
) (bll.IntentHandler[models.BlockNodeInputs], error) {
	switch action {
	case models.ActionInstall:
		// With no Kubernetes yet, install stands it up, which writes the
		// system record, so it needs the handler that owns it.
		if workflows.KubernetesInstalled() {
			return h.install, nil
		}
		return h.installWithCluster, nil
	case models.ActionUpgrade:
		return h.upgrade, nil
	case models.ActionReconfigure:
		return h.reconfigure, nil
	case models.ActionReset:
		return h.reset, nil
	case models.ActionUninstall:
		return h.uninstall, nil
	default:
		return nil, errorx.IllegalArgument.New("unsupported action %q for block node", action)
	}
}
