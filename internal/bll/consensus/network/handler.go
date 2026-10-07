// SPDX-License-Identifier: Apache-2.0

// Package network holds the business-logic handlers for orbit/network-level
// consensus intents (TargetConsensusNetwork), as distinct from the per-node
// handlers in the parent consensus package. Every network-level verb (genesis,
// and future freeze/upgrade) routes through BaseHandler.HandleIntent so it
// inherits out-of-band drift reporting and the scoped state flush.
package network

import (
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// Handlers holds per-action handlers for consensus network intents.
type Handlers struct {
	genesis *GenesisHandler
}

// NewHandlerFactory validates dependencies and returns a Handlers with all
// handlers initialized.
func NewHandlerFactory(runtime *rsl.RuntimeResolver) (*Handlers, error) {
	base, err := bll.NewBaseHandler[models.ConsensusNetworkInputs](runtime, models.TargetConsensusNetwork)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to create BaseHandler")
	}

	genesisHandler, err := NewGenesisHandler(base)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to create GenesisHandler")
	}

	return &Handlers{genesis: genesisHandler}, nil
}

// ForAction returns the appropriate IntentHandler for the given action.
func (h *Handlers) ForAction(action models.ActionType) (bll.IntentHandler[models.ConsensusNetworkInputs], error) {
	switch action {
	case models.ActionGenesis:
		return h.genesis, nil
	default:
		return nil, errx.Decorate(
			errorx.IllegalArgument.New("unsupported action %q for consensus network", action),
			reasons.InvalidArgument,
			"Supported actions for consensus network: genesis")
	}
}
