// SPDX-License-Identifier: Apache-2.0

package daemon

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
