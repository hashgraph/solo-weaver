// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"errors"
	"time"
)

// SoakStartRequest is the payload for POST /migration/consensus/soak/start.
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
		return errors.New("node_id is required")
	}
	if r.MigrationPlanPath == "" {
		return errors.New("migration_plan_path is required")
	}
	if r.CutoverTimestamp.IsZero() {
		return errors.New("cutover_timestamp is required")
	}
	return nil
}

// SoakStatusResponse is returned by GET /migration/consensus/soak/status.
type SoakStatusResponse struct {
	Active  bool              `json:"active"`
	Request *SoakStartRequest `json:"request,omitempty"`
}

// SoakStartResponse is returned by POST /migration/consensus/soak/start on accept.
type SoakStartResponse struct {
	Accepted bool `json:"accepted"`
}
