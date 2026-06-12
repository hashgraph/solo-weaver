// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"encoding/json"
	"net/http"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon/core"
	"github.com/joomcode/errorx"
)

// ConsensusMigrationStatusResponse is the combined view returned by
// GET /consensus_node/migration/status: supervisor health + current soak state.
type ConsensusMigrationStatusResponse struct {
	Monitor core.MonitorState  `json:"monitor"`
	Soak    SoakStatusResponse `json:"soak"`
}

// ConsensusNodeHandler implements core.ComponentHandler for all consensus-node
// HTTP routes under the /consensus_node/ prefix.
//
// To add a new monitor for the consensus-node component:
//  1. Add a field for the monitor and its state func to this struct.
//  2. Register the new route(s) in RegisterRoutes.
//  3. Implement the handler method(s) on *ConsensusNodeHandler.
type ConsensusNodeHandler struct {
	// mm is the migration monitor. Nil when the monitor is disabled;
	// the migration routes return 503 in that case.
	mm *MigrationMonitor

	// migrationStateFn returns the supervisor-level health of the migration
	// monitor goroutine (from its StatusTracker). May be nil when mm is nil.
	migrationStateFn func() core.MonitorState
}

// NewConsensusNodeHandler constructs a ConsensusNodeHandler.
// mm and migrationStateFn may be nil when the migration monitor is disabled.
func NewConsensusNodeHandler(mm *MigrationMonitor, migrationStateFn func() core.MonitorState) *ConsensusNodeHandler {
	return &ConsensusNodeHandler{mm: mm, migrationStateFn: migrationStateFn}
}

// RegisterRoutes implements core.ComponentHandler.
// All routes are prefixed with /consensus_node/.
//
// Current routes:
//
//	GET    /consensus_node/migration/status       — combined: monitor health + soak state
//	GET    /consensus_node/migration/soak/status  — soak-run state only
//	POST   /consensus_node/migration/soak/start   — enqueue a new soak run
//	DELETE /consensus_node/migration/soak         — stop the running soak watcher
func (h *ConsensusNodeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /consensus_node/migration/status", h.handleConsensusMigrationStatus)
	mux.HandleFunc("GET /consensus_node/migration/soak/status", h.handleConsensusSoakStatus)
	mux.HandleFunc("POST /consensus_node/migration/soak/start", h.handleConsensusSoakStart)
	mux.HandleFunc("DELETE /consensus_node/migration/soak", h.handleConsensusSoakStop)
}

// handleConsensusMigrationStatus returns the combined view of the migration
// monitor: supervisor health (running/backoff/stopped) + current soak state.
//
// For finer-grained access:
//   - GET /consensus_node/migration/soak/status   — soak state only
//   - GET /consensus_node/migration/monitor/status — monitor health only (planned)
func (h *ConsensusNodeHandler) handleConsensusMigrationStatus(w http.ResponseWriter, _ *http.Request) {
	if h.mm == nil {
		writeError(w, http.StatusServiceUnavailable, "migration monitor not enabled")
		return
	}

	var state core.MonitorState
	if h.migrationStateFn != nil {
		state = h.migrationStateFn()
	}

	writeJSON(w, http.StatusOK, ConsensusMigrationStatusResponse{
		Monitor: state,
		Soak:    *h.mm.Status(),
	})
}

func (h *ConsensusNodeHandler) handleConsensusSoakStatus(w http.ResponseWriter, _ *http.Request) {
	if h.mm == nil {
		writeError(w, http.StatusServiceUnavailable, "migration monitor not enabled")
		return
	}
	writeJSON(w, http.StatusOK, h.mm.Status())
}

func (h *ConsensusNodeHandler) handleConsensusSoakStart(w http.ResponseWriter, r *http.Request) {
	if h.mm == nil {
		writeError(w, http.StatusServiceUnavailable, "migration monitor not enabled")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10) // 16 KiB

	var req SoakStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := req.Validate(); err != nil {
		// Surface the plain validation message in the HTTP 400 body; the errorx
		// namespace prefix is internal and should not leak to API clients.
		msg := err.Error()
		if ex := errorx.Cast(err); ex != nil {
			msg = ex.Message()
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	if !h.mm.TryEnqueue(req) {
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

// handleConsensusSoakStop stops the running soak watcher.
// Query param: delete_state=false preserves cutover-state.jsonl so the daemon
// resumes the soak on the next restart (default: true — state is deleted).
func (h *ConsensusNodeHandler) handleConsensusSoakStop(w http.ResponseWriter, r *http.Request) {
	if h.mm == nil {
		writeError(w, http.StatusServiceUnavailable, "migration monitor not enabled")
		return
	}

	deleteState := r.URL.Query().Get("delete_state") != "false"

	if !h.mm.TryStop(deleteState) {
		writeError(w, http.StatusConflict, "no soak watcher is currently active")
		return
	}

	logx.As().Info().
		Str("reason", "SoakStopAccepted").
		Bool("delete_state", deleteState).
		Msg("Soak stop request accepted")

	w.WriteHeader(http.StatusNoContent)
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
