// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"encoding/json"
	"net/http"
)

// writeJSON serialises v as JSON and writes it with the given status code.
// All handlers must use this helper to guarantee a consistent Content-Type.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error body with the given status code.
func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, ErrorResponse{Error: msg})
}

// handleHealth is the process-level liveness probe.
// Always returns 200 {"status":"ok"} as long as the process is alive.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

// handleStatus returns the full daemon status: all enabled components and
// their per-monitor runtime states.
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	if s.statusFn == nil {
		writeJSON(w, http.StatusOK, StatusResponse{Components: map[string]ComponentStatus{}})
		return
	}
	writeJSON(w, http.StatusOK, s.statusFn())
}
