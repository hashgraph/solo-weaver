// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/automa-saga/daemonkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBlockNodeHandler_TrafficShaperStatus_Enabled verifies the status route
// returns 200 with the supervisor-reported monitor state when the monitor is
// enabled.
func TestBlockNodeHandler_TrafficShaperStatus_Enabled(t *testing.T) {
	mon := NewTrafficShaperMonitor(nil)
	stateFn := func() daemonkit.MonitorState { return daemonkit.MonitorState{State: "running"} }
	h := NewBlockNodeHandler(mon, stateFn)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/block_node/traffic_shaper/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp TrafficShaperStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "running", resp.Monitor.State)
}

// TestBlockNodeHandler_TrafficShaperStatus_Disabled verifies the status route
// returns 503 when the monitor is not enabled (nil monitor).
func TestBlockNodeHandler_TrafficShaperStatus_Disabled(t *testing.T) {
	h := NewBlockNodeHandler(nil, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/block_node/traffic_shaper/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Contains(t, body.Error, "not enabled")
}
