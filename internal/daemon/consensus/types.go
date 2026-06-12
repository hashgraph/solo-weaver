// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"time"

	"github.com/joomcode/errorx"
)

// SoakStartRequest is the payload for POST /consensus_node/migration/soak/start.
type SoakStartRequest struct {
	NodeID            string    `json:"node_id"`
	CutoverTimestamp  time.Time `json:"cutover_timestamp"`
	MigrationPlanPath string    `json:"migration_plan_path"`
}

// Validate checks that all required fields are present.
// Called by the HTTP handler and by resumeIfNeeded (story #520) before
// activating a watcher from a persisted request.
func (r SoakStartRequest) Validate() error {
	if r.NodeID == "" {
		return errorx.IllegalArgument.New("node_id is required")
	}
	if r.MigrationPlanPath == "" {
		return errorx.IllegalArgument.New("migration_plan_path is required")
	}
	if r.CutoverTimestamp.IsZero() {
		return errorx.IllegalArgument.New("cutover_timestamp is required")
	}
	return nil
}

// SoakStatusResponse is returned by GET /consensus_node/migration/soak/status.
type SoakStatusResponse struct {
	Active  bool              `json:"active"`
	Request *SoakStartRequest `json:"request,omitempty"`
}

// SoakStartResponse is returned by POST /consensus_node/migration/soak/start on accept.
type SoakStartResponse struct {
	Accepted bool `json:"accepted"`
}
