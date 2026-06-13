// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"time"

	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
)

// MigrationPlanBaseDir is the only directory from which a consensus-node
// migration plan may be loaded. SoakStartRequest.Validate anchors
// MigrationPlanPath within this directory so a socket client (any weaver-group
// member) cannot point the daemon at an arbitrary file on the host. Keep this
// in sync with where the migration workflow stages plans on disk.
const MigrationPlanBaseDir = "/opt/solo/weaver/migration/consensus"

// SoakStartRequest is the payload for POST /consensus_node/migration/soak/start.
type SoakStartRequest struct {
	NodeID            string    `json:"node_id"`
	CutoverTimestamp  time.Time `json:"cutover_timestamp"`
	MigrationPlanPath string    `json:"migration_plan_path"`
}

// Validate checks that all required fields are present and that
// MigrationPlanPath is a traversal-free path anchored within MigrationPlanBaseDir.
// Called by the HTTP handler and by resumeIfNeeded (story #520) before
// activating a watcher from a persisted request — the path arrives over the
// daemon socket, so validation here is the trust boundary against arbitrary
// file reads / path traversal.
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
	// Anchor the plan path within the staging dir; rejects "..", shell
	// metacharacters, and any path that escapes MigrationPlanBaseDir.
	if _, err := sanity.ValidatePathWithinBase(MigrationPlanBaseDir, r.MigrationPlanPath); err != nil {
		return errorx.IllegalArgument.Wrap(err, "migration_plan_path is invalid")
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
