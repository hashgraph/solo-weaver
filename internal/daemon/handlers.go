// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/automa-saga/logx"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, ErrorResponse{Error: msg})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

func (s *Server) handleSoakStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.sw.Status())
}

func (s *Server) handleSoakStart(w http.ResponseWriter, r *http.Request) {
	// Cap body size to prevent oversized payloads from exhausting memory.
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10) // 16 KiB

	var req SoakStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if !s.sw.TryEnqueue(req) {
		writeError(w, http.StatusConflict, "soak already active or pending")
		return
	}

	logx.As().Info().
		Str("reason", "SoakStartAccepted").
		Str("node_id", req.NodeID).
		Str("migration_plan", req.MigrationPlanPath).
		Time("cutover_ts", req.CutoverTimestamp).
		Msg("Soak start request accepted")

	writeJSON(w, http.StatusAccepted, SoakStartResponse{Accepted: true})
}
