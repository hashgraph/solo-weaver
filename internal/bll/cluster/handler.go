// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

type HandlerFactory struct {
	install *InstallHandler
}

// NewHandlerFactory validates dependencies and returns a HandlerFactory with all handlers initialized.
// All dependencies are required; any nil returns an error.
func NewHandlerFactory(
	runtime *rsl.RuntimeResolver,
) (*HandlerFactory, error) {
	base, err := bll.NewBaseHandler[models.ClusterInputs](runtime)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create BaseHandler: %v", err)
	}

	if runtime == nil {
		return nil, errorx.IllegalArgument.New("RuntimeResolver cannot be nil")
	}

	cr, ok := runtime.ClusterRuntime.(*rsl.ClusterRuntimeResolver)
	if !ok {
		return nil, errorx.IllegalArgument.New("expected ClusterRuntime to be *rsl.ClusterRuntimeResolver but got %T", runtime.ClusterRuntime)
	}

	mr, ok := runtime.MachineRuntime.(*rsl.MachineRuntimeResolver)
	if !ok {
		return nil, errorx.IllegalArgument.New("expected MachineRuntime to be *rsl.MachineRuntimeResolver but got %T", runtime.MachineRuntime)
	}

	h := &HandlerFactory{
		install: NewInstallHandler(base, cr, mr),
		//upgrade:   newUpgradeHandler(base, runtime.BlockNodeRuntime),
		//uninstall: newUninstallHandler(base, runtime.BlockNodeRuntime),
	}

	return h, nil
}

// ForAction returns the appropriate IntentHandler for the given action, or an error if the action is unsupported.
func (h *HandlerFactory) ForAction(
	action models.ActionType,
) (bll.IntentHandler[models.ClusterInputs], error) {
	switch action {
	case models.ActionInstall:
		return h.install, nil
	//case models.ActionUpgrade:
	//	return h.upgrade, nil
	//case models.ActionUninstall:
	//	return h.uninstall, nil
	default:
		return nil, errorx.IllegalArgument.New("unsupported action %q for block node", action)
	}
}
