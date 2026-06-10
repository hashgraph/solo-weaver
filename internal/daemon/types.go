// SPDX-License-Identifier: Apache-2.0

package daemon

import "github.com/hashgraph/solo-weaver/internal/daemon/core"

// HealthResponse is returned by GET /health.
type HealthResponse struct {
	Status string `json:"status"`
}

// ErrorResponse is the standard JSON error envelope returned on 4xx/5xx.
type ErrorResponse struct {
	Error string `json:"error"`
}

// StatusResponse is returned by GET /status. It reports the runtime state of
// every enabled component and their individual monitors, plus any unmet startup
// prerequisites detected by the background probe loop.
type StatusResponse struct {
	Components  map[string]ComponentStatus `json:"components"`
	// ProbeErrors maps component name → probe failure message for any component
	// whose disk prerequisites are not yet satisfied. Empty when all probes pass.
	ProbeErrors map[string]string `json:"probe_errors,omitempty"`
}

// ComponentStatus holds the per-monitor states for one component.
type ComponentStatus struct {
	Monitors map[string]core.MonitorState `json:"monitors"`
}
