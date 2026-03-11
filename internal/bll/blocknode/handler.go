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
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// HandlerRegistry is the private struct that holds all per-action handlers for block-node intents.
type HandlerRegistry struct {
	install   *InstallHandler
	upgrade   *UpgradeHandler
	reset     *ResetHandler
	uninstall *UninstallHandler
}

// NewHandlerFactory validates dependencies and returns a HandlerRegistry with all handlers initialized.
// All dependencies are required; any nil returns an error.
func NewHandlerFactory(
	sm state.Manager,
	runtime *rsl.RuntimeResolver,
) (*HandlerRegistry, error) {
	base, err := bll.NewBaseHandler[models.BlockNodeInputs](runtime)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create BaseHandler: %v", err)
	}
	if sm == nil {
		return nil, errorx.IllegalArgument.New("state.Manager cannot be nil")
	}

	if runtime == nil {
		return nil, errorx.IllegalArgument.New("rsl.RuntimeResolver cannot be nil")
	}

	if runtime.BlockNodeRuntime == nil {
		return nil, errorx.IllegalArgument.New("runtime.BlockNodeRuntime is nil")
	}

	bnr, ok := runtime.BlockNodeRuntime.(*rsl.BlockNodeRuntimeResolver)
	if !ok {
		return nil, errorx.IllegalArgument.New("expected BlockNodeRuntime to be *rsl.BlockNodeRuntimeResolver but got %T", runtime.BlockNodeRuntime)
	}

	installHandler, err := NewInstallHandler(base, *bnr, sm)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create InstallHandler: %v", err)
	}

	upgradeHandler, err := NewUpgradeHandler(base, *bnr)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create UpgradeHandler: %v", err)
	}

	resetHandler, err := NewResetHandler(base, *bnr)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create ResetHandler: %v", err)
	}

	uninstallHandler, err := NewUninstallHandler(base, *bnr)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to create UninstallHandler: %v", err)
	}

	h := &HandlerRegistry{
		install:   installHandler,
		upgrade:   upgradeHandler,
		reset:     resetHandler,
		uninstall: uninstallHandler,
	}

	return h, nil
}

// ForAction returns the appropriate IntentHandler for the given action, or an error if the action is unsupported.
func (h *HandlerRegistry) ForAction(
	action models.ActionType,
) (bll.IntentHandler[models.BlockNodeInputs], error) {
	switch action {
	case models.ActionInstall:
		return h.install, nil
	case models.ActionUpgrade:
		return h.upgrade, nil
	case models.ActionReset:
		return h.reset, nil
	case models.ActionUninstall:
		return h.uninstall, nil
	default:
		return nil, errorx.IllegalArgument.New("unsupported action %q for block node", action)
	}
}
