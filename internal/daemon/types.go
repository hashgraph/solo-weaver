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
// every enabled component and their individual monitors.
type StatusResponse struct {
	Components map[string]ComponentStatus `json:"components"`
}

// ComponentStatus holds the per-monitor states for one component.
type ComponentStatus struct {
	Monitors map[string]core.MonitorState `json:"monitors"`
}
