// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/daemon/core"
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

func TestWriteError_WrapsMessageInEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusBadRequest, "bad input")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var body ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "bad input", body.Error)
}

func TestHandleHealth_AlwaysOK(t *testing.T) {
	s := NewServer("/tmp/unused.sock", ServerOptions{}, ServerConfig{})
	rec := httptest.NewRecorder()

	s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var body HealthResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body.Status)
}

func TestHandleStatus_NilStatusFnReturnsEmptyComponents(t *testing.T) {
	s := NewServer("/tmp/unused.sock", ServerOptions{}, ServerConfig{}) // no StatusFn
	rec := httptest.NewRecorder()

	s.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/status", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var body StatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotNil(t, body.Components)
	assert.Empty(t, body.Components)
}

func TestHandleStatus_DelegatesToStatusFn(t *testing.T) {
	want := StatusResponse{Components: map[string]ComponentStatus{
		"consensus-node": {Monitors: map[string]core.MonitorState{
			"upgrade-monitor": {State: "running"},
		}},
	}}
	s := NewServer("/tmp/unused.sock", ServerOptions{
		StatusFn: func() StatusResponse { return want },
	}, ServerConfig{})
	rec := httptest.NewRecorder()

	s.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/status", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	var body StatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "running", body.Components["consensus-node"].Monitors["upgrade-monitor"].State)
}
