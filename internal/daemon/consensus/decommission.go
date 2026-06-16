// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"

	"github.com/automa-saga/logx"
)

// Decommissioner triggers node decommission once all soak criteria are met.
type Decommissioner interface {
	Decommission(ctx context.Context, nodeID string) error
}

// NoopDecommissioner logs the call and returns nil. Used in this story while
// the real K8s decommission API call is deferred to a subsequent story.
type NoopDecommissioner struct{}

func (*NoopDecommissioner) Decommission(_ context.Context, nodeID string) error {
	logx.As().Info().
		Str("reason", "SoakDecommissionCalled").
		Str("node_id", nodeID).
		Msg("NoopDecommissioner: decommission called — no action taken (stub)")
	return nil
}
