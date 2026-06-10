// SPDX-License-Identifier: Apache-2.0

package daemon

import "github.com/hashgraph/solo-weaver/internal/daemon/consensus"

// HealthResponse is returned by GET /health.
type HealthResponse struct {
	Status string `json:"status"`
}

// ErrorResponse is returned by all error paths to keep Content-Type consistent.
type ErrorResponse struct {
	Error string `json:"error"`
}

// StatusResponse is returned by GET /status. It reports the runtime state of
// every enabled component and their individual monitors.
type StatusResponse struct {
	// Components maps component name (e.g. "consensus-node") to its status.
	Components map[string]ComponentStatus `json:"components"`
}

// ComponentStatus holds the per-monitor states for one component.
type ComponentStatus struct {
	// Monitors maps monitor name (e.g. "upgrade-monitor") to its current state.
	Monitors map[string]MonitorState `json:"monitors"`
}

// ConsensusMigrationStatusResponse is returned by GET /consensus_node/migration/status.
// It combines the migration monitor's supervisor health with the current soak
// run state so callers can inspect both in a single request.
//
// The type is prefixed with "Consensus" so that a future BlockNodeMigrationStatusResponse
// (or similar) can be added without ambiguity.
//
// Future per-resource granularity (monitor health only, soak only) is available
// via the more specific sub-paths:
//   - GET /consensus_node/migration/soak/status    — soak state only
//   - GET /consensus_node/migration/monitor/status — monitor health only (planned)
type ConsensusMigrationStatusResponse struct {
	// Monitor is the supervisor-level health of the migration monitor goroutine
	// (e.g. "running", "backoff:5s", "stopped").
	Monitor MonitorState `json:"monitor"`

	// Soak is the current soak-run state (active flag + accepted request fields).
	Soak consensus.SoakStatusResponse `json:"soak"`
}
