// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// Handlers holds per-action handlers for consensus node intents.
type Handlers struct {
	install *InstallHandler
}

// NewHandlerFactory validates dependencies and returns a Handlers with all handlers initialized.
func NewHandlerFactory(runtime *rsl.RuntimeResolver) (*Handlers, error) {
	base, err := bll.NewBaseHandler[models.ConsensusNodeInputs](runtime, models.TargetConsensusNode)
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

	return &Handlers{install: installHandler}, nil
}

// ForAction returns the appropriate IntentHandler for the given action.
func (h *Handlers) ForAction(action models.ActionType) (bll.IntentHandler[models.ConsensusNodeInputs], error) {
	switch action {
	case models.ActionInstall:
		return h.install, nil
	default:
		return nil, errorx.IllegalArgument.New("unsupported action %q for consensus node", action)
	}
}
