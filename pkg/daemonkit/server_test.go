// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package daemonkit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteJSON_SetsContentTypeAndStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusTeapot, map[string]string{"hello": "world"})

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "world", body["hello"])
}

func TestHandleHealth_AlwaysOK(t *testing.T) {
	s := NewServer("/tmp/unused.sock", ServerOptions{}, ServerConfig{})
	rec := httptest.NewRecorder()

	s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
}

func TestHandleStatus_NilStatusFnReturnsEmptyObject(t *testing.T) {
	s := NewServer("/tmp/unused.sock", ServerOptions{}, ServerConfig{}) // no StatusFn
	rec := httptest.NewRecorder()

	s.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/status", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Empty(t, body)
}

func TestHandleStatus_DelegatesToStatusFn(t *testing.T) {
	type componentStatus struct {
		Monitors map[string]MonitorState `json:"monitors"`
	}
	type statusResponse struct {
		Components map[string]componentStatus `json:"components"`
	}
	want := statusResponse{Components: map[string]componentStatus{
		"consensus-node": {Monitors: map[string]MonitorState{
			"upgrade-monitor": {State: "running"},
		}},
	}}
	s := NewServer("/tmp/unused.sock", ServerOptions{
		StatusFn: func() any { return want },
	}, ServerConfig{})
	rec := httptest.NewRecorder()

	s.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/status", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var body statusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "running", body.Components["consensus-node"].Monitors["upgrade-monitor"].State)
}
