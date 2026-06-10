// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
)

// ConsensusNodeHandler implements ComponentHandler for all consensus-node HTTP
// routes under the /consensus_node/ prefix.
//
// To add a new monitor for the consensus-node component:
//  1. Add a field for the monitor and its state func to this struct.
//  2. Register the new route(s) in RegisterRoutes.
//  3. Implement the handler method(s) on *ConsensusNodeHandler.
type ConsensusNodeHandler struct {
	// mm is the migration monitor. Nil when the monitor is disabled;
	// the migration routes return 503 in that case.
	mm *consensus.MigrationMonitor

	// migrationStateFn returns the supervisor-level health of the migration
	// monitor goroutine (from its StatusTracker). May be nil when mm is nil.
	migrationStateFn func() MonitorState
}

// NewConsensusNodeHandler constructs a ConsensusNodeHandler.
// mm and migrationStateFn may be nil when the migration monitor is disabled.
func NewConsensusNodeHandler(mm *consensus.MigrationMonitor, migrationStateFn func() MonitorState) *ConsensusNodeHandler {
	return &ConsensusNodeHandler{mm: mm, migrationStateFn: migrationStateFn}
}

// RegisterRoutes implements ComponentHandler.
// All routes are prefixed with /consensus_node/.
//
// Current routes:
//
//	GET  /consensus_node/migration/status       — combined: monitor health + soak state
//	GET  /consensus_node/migration/soak/status  — soak-run state only
//	POST /consensus_node/migration/soak/start   — enqueue a new soak run
func (h *ConsensusNodeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /consensus_node/migration/status", h.handleConsensusMigrationStatus)
	mux.HandleFunc("GET /consensus_node/migration/soak/status", h.handleConsensusSoakStatus)
	mux.HandleFunc("POST /consensus_node/migration/soak/start", h.handleConsensusSoakStart)
}

// handleConsensusMigrationStatus returns the combined view of the migration
// monitor: supervisor health (running/backoff/stopped) + current soak state.
// Primary endpoint for operators that want a single call to assess the
// consensus-node migration subsystem.
//
// For finer-grained access:
//   - GET /consensus_node/migration/soak/status   — soak state only
//   - GET /consensus_node/migration/monitor/status — monitor health only (planned)
func (h *ConsensusNodeHandler) handleConsensusMigrationStatus(w http.ResponseWriter, _ *http.Request) {
	if h.mm == nil {
		writeError(w, http.StatusServiceUnavailable, "migration monitor not enabled")
		return
	}

	var state MonitorState
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

	// Cap body size to prevent oversized payloads from exhausting memory.
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10) // 16 KiB

	var req consensus.SoakStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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

	writeJSON(w, http.StatusAccepted, consensus.SoakStartResponse{Accepted: true})
}
