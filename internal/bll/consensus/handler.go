// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"github.com/automa-saga/errx"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// Handlers holds per-action handlers for consensus node intents.
type Handlers struct {
	install   *InstallHandler
	uninstall *UninstallHandler
	start     *RunStateHandler
	stop      *RunStateHandler
}

// NewHandlerFactory validates dependencies and returns a Handlers with all handlers initialized.
func NewHandlerFactory(runtime *rsl.RuntimeResolver) (*Handlers, error) {
	base, err := bll.NewBaseHandler[models.ConsensusNodeInputs](runtime, models.TargetConsensusNode,
		bll.WithProfileExtractor(func(i models.ConsensusNodeInputs) string { return i.Profile }),
		bll.WithManagedComponents[models.ConsensusNodeInputs](bll.ConsensusNode, bll.Profile),
	)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to create BaseHandler")
	}

	if runtime.ConsensusRuntime == nil {
		return nil, errorx.IllegalArgument.New("runtime.ConsensusRuntime is nil")
	}

	cr, ok := runtime.ConsensusRuntime.(*rsl.ConsensusNodeRuntimeResolver)
	if !ok {
		return nil, errorx.IllegalArgument.New("expected ConsensusRuntime to be *rsl.ConsensusNodeRuntimeResolver but got %T", runtime.ConsensusRuntime)
	}

	installHandler, err := NewInstallHandler(base, cr)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to create InstallHandler")
	}

	// Uninstall, start and stop write only the consensus state, so they do not lock or rewrite profile.yaml.
	consensusOnlyBase, err := bll.NewBaseHandler[models.ConsensusNodeInputs](runtime, models.TargetConsensusNode,
		bll.WithManagedComponents[models.ConsensusNodeInputs](bll.ConsensusNode),
	)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to create consensus-only BaseHandler")
	}

	uninstallHandler, err := NewUninstallHandler(consensusOnlyBase)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to create UninstallHandler")
	}

	startHandler, err := NewRunStateHandler(consensusOnlyBase, operatorv1alpha1.RunStateRunning)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to create start handler")
	}
	stopHandler, err := NewRunStateHandler(consensusOnlyBase, operatorv1alpha1.RunStateStopped)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to create stop handler")
	}

	return &Handlers{install: installHandler, uninstall: uninstallHandler, start: startHandler, stop: stopHandler}, nil
}

// ForAction returns the appropriate IntentHandler for the given action.
func (h *Handlers) ForAction(action models.ActionType) (bll.IntentHandler[models.ConsensusNodeInputs], error) {
	switch action {
	case models.ActionInstall:
		return h.install, nil
	case models.ActionUninstall:
		return h.uninstall, nil
	case models.ActionStart:
		return h.start, nil
	case models.ActionStop:
		return h.stop, nil
	default:
		return nil, errx.Decorate(
			errorx.IllegalArgument.New("unsupported action %q for consensus node", action),
			reasons.InvalidArgument,
			"Supported actions for consensus node: install, uninstall, start, stop")
	}
}
