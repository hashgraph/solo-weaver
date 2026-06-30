// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"encoding/json"
	"net/http"

	"github.com/automa-saga/daemonkit"
)

// TrafficShaperStatusResponse is the view returned by
// GET /block_node/traffic_shaper/status. For #746 it carries only the
// supervisor-level health of the monitor goroutine (running/backoff/stopped).
// Per-subsystem and degraded-state reporting is added by #750.
type TrafficShaperStatusResponse struct {
	Monitor daemonkit.MonitorState `json:"monitor"`
}

// BlockNodeHandler implements daemonkit.ComponentHandler for all block-node
// HTTP routes under the /block_node/ prefix.
//
// To add a new monitor for the block-node component:
//  1. Add a field for the monitor and its state func to this struct.
//  2. Register the new route(s) in RegisterRoutes.
//  3. Implement the handler method(s) on *BlockNodeHandler.
type BlockNodeHandler struct {
	// mon is the traffic-shaper monitor. Nil when the monitor is disabled;
	// the traffic-shaper routes return 503 in that case.
	mon *TrafficShaperMonitor

	// trafficShaperStateFn returns the supervisor-level health of the
	// traffic-shaper monitor goroutine (from its StatusTracker). May be nil
	// when mon is nil.
	trafficShaperStateFn func() daemonkit.MonitorState
}

// NewBlockNodeHandler constructs a BlockNodeHandler. mon and trafficShaperStateFn
// may be nil when the traffic-shaper monitor is disabled.
func NewBlockNodeHandler(mon *TrafficShaperMonitor, trafficShaperStateFn func() daemonkit.MonitorState) *BlockNodeHandler {
	return &BlockNodeHandler{mon: mon, trafficShaperStateFn: trafficShaperStateFn}
}

// RegisterRoutes implements daemonkit.ComponentHandler.
// All routes are prefixed with /block_node/.
//
// Current routes:
//
//	GET /block_node/traffic_shaper/status — traffic-shaper monitor health
func (h *BlockNodeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /block_node/traffic_shaper/status", h.handleTrafficShaperStatus)
}

// handleTrafficShaperStatus returns the supervisor-level health of the
// traffic-shaper monitor (running/backoff/stopped). Returns 503 when the
// monitor is disabled.
func (h *BlockNodeHandler) handleTrafficShaperStatus(w http.ResponseWriter, _ *http.Request) {
	if h.mon == nil {
		writeError(w, http.StatusServiceUnavailable, "traffic-shaper monitor not enabled")
		return
	}

	var state daemonkit.MonitorState
	if h.trafficShaperStateFn != nil {
		state = h.trafficShaperStateFn()
	}

	writeJSON(w, http.StatusOK, TrafficShaperStatusResponse{Monitor: state})
}

// writeJSON serialises v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON {"error":"..."} body with the given status code.
func writeError(w http.ResponseWriter, code int, msg string) {
	type errorBody struct {
		Error string `json:"error"`
	}
	writeJSON(w, code, errorBody{Error: msg})
}
