// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startTestDaemon runs a Daemon with a temp-dir socket and returns an HTTP
// client pre-configured to dial that socket plus a cancel func.
func startTestDaemon(t *testing.T) (*http.Client, context.CancelFunc) {
	t.Helper()
	client, _, cancel := startTestDaemonWithConfig(t, daemon.ServerConfig{})
	return client, cancel
}

// startTestDaemonWithConfig is like startTestDaemon but accepts a ServerConfig
// and also returns the socket path so tests can dial raw connections.
func startTestDaemonWithConfig(t *testing.T, cfg daemon.ServerConfig) (*http.Client, string, context.CancelFunc) {
	t.Helper()

	sockPath := filepath.Join(t.TempDir(), "daemon.sock")
	paths := models.WeaverPaths{DaemonSockPath: sockPath}

	ctx, cancel := context.WithCancel(context.Background())
	sw := daemon.NewSoakWatcher()
	srv := daemon.NewServer(sockPath, sw, cfg)
	go func() { _ = daemon.NewWithComponents(paths, srv, sw).Run(ctx) }()

	// Wait until the socket is reachable.
	require.Eventually(t, func() bool {
		c, err := net.Dial("unix", sockPath)
		if err == nil {
			_ = c.Close()
			return true
		}
		return false
	}, 3*time.Second, 5*time.Millisecond, "daemon socket did not become ready")

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
			},
		},
	}
	return client, sockPath, cancel
}

func getJSON(t *testing.T, client *http.Client, url string, dst any) *http.Response {
	t.Helper()
	resp, err := client.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.NoError(t, json.NewDecoder(resp.Body).Decode(dst))
	return resp
}

func postJSON(t *testing.T, client *http.Client, url string, body any, dst any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := client.Post(url, "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	defer resp.Body.Close()
	if dst != nil {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(dst))
	}
	return resp
}

func Test_Health(t *testing.T) {
	client, cancel := startTestDaemon(t)
	defer cancel()

	var body daemon.HealthResponse
	resp := getJSON(t, client, "http://daemon/health", &body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", body.Status)
}

func Test_SoakStatus_Idle(t *testing.T) {
	client, cancel := startTestDaemon(t)
	defer cancel()

	var body daemon.SoakStatusResponse
	resp := getJSON(t, client, "http://daemon/migration/consensus/soak/status", &body)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.False(t, body.Active)
	assert.Nil(t, body.Request)
}

func Test_SoakStart_Then_Status(t *testing.T) {
	client, cancel := startTestDaemon(t)
	defer cancel()

	cutover := time.Now().UTC().Truncate(time.Second)
	payload := daemon.SoakStartRequest{
		NodeID:            "0.0.3",
		CutoverTimestamp:  cutover,
		MigrationPlanPath: "/opt/solo/weaver/migration/consensus/0.0.3-20250521T143022Z-migration-plan.yaml",
	}

	var startResp daemon.SoakStartResponse
	resp := postJSON(t, client, "http://daemon/migration/consensus/soak/start", payload, &startResp)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.True(t, startResp.Accepted)

	// The soak-watcher goroutine updates soakStatus asynchronously after the
	// HTTP response returns — poll until it becomes visible.
	require.Eventually(t, func() bool {
		var status daemon.SoakStatusResponse
		getJSON(t, client, "http://daemon/migration/consensus/soak/status", &status)
		return status.Active
	}, 2*time.Second, 10*time.Millisecond, "soak status did not become active")

	var status daemon.SoakStatusResponse
	getJSON(t, client, "http://daemon/migration/consensus/soak/status", &status)
	require.NotNil(t, status.Request)
	assert.Equal(t, payload.NodeID, status.Request.NodeID)
	assert.Equal(t, payload.MigrationPlanPath, status.Request.MigrationPlanPath)
	assert.True(t, status.Request.CutoverTimestamp.Equal(cutover))
}

func Test_SoakStart_Conflict_When_Active(t *testing.T) {
	client, cancel := startTestDaemon(t)
	defer cancel()

	payload := daemon.SoakStartRequest{
		NodeID:            "0.0.3",
		CutoverTimestamp:  time.Now(),
		MigrationPlanPath: "/opt/solo/weaver/migration/consensus/0.0.3-20250521T143022Z-migration-plan.yaml",
	}

	var first daemon.SoakStartResponse
	resp := postJSON(t, client, "http://daemon/migration/consensus/soak/start", payload, &first)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	// Wait for the first request to be consumed from the channel.
	require.Eventually(t, func() bool {
		var status daemon.SoakStatusResponse
		getJSON(t, client, "http://daemon/migration/consensus/soak/status", &status)
		return status.Active
	}, 2*time.Second, 10*time.Millisecond)

	// Second POST while watcher is running should be rejected.
	resp2, err := client.Post("http://daemon/migration/consensus/soak/start",
		"application/json", bytes.NewReader(mustMarshal(t, payload)))
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusConflict, resp2.StatusCode)
	assert.Equal(t, "application/json", resp2.Header.Get("Content-Type"))
	var errBody daemon.ErrorResponse
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&errBody))
	assert.NotEmpty(t, errBody.Error)
}

func Test_SoakStart_InvalidBody(t *testing.T) {
	client, cancel := startTestDaemon(t)
	defer cancel()

	resp, err := client.Post("http://daemon/migration/consensus/soak/start",
		"application/json", bytes.NewReader([]byte("not-json")))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	var errBody daemon.ErrorResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errBody))
	assert.NotEmpty(t, errBody.Error)
}

func Test_SoakStart_MissingNodeID(t *testing.T) {
	client, cancel := startTestDaemon(t)
	defer cancel()

	payload := daemon.SoakStartRequest{CutoverTimestamp: time.Now()}
	resp, err := client.Post("http://daemon/migration/consensus/soak/start",
		"application/json", bytes.NewReader(mustMarshal(t, payload)))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var errBody daemon.ErrorResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errBody))
	assert.Equal(t, "node_id is required", errBody.Error)
}

func Test_SoakStart_MissingMigrationPlanPath(t *testing.T) {
	client, cancel := startTestDaemon(t)
	defer cancel()

	payload := daemon.SoakStartRequest{NodeID: "0.0.3", CutoverTimestamp: time.Now()}
	resp, err := client.Post("http://daemon/migration/consensus/soak/start",
		"application/json", bytes.NewReader(mustMarshal(t, payload)))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var errBody daemon.ErrorResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errBody))
	assert.Equal(t, "migration_plan_path is required", errBody.Error)
}

func Test_SoakStart_MissingCutoverTimestamp(t *testing.T) {
	client, cancel := startTestDaemon(t)
	defer cancel()

	payload := daemon.SoakStartRequest{
		NodeID:            "0.0.3",
		MigrationPlanPath: "/opt/solo/weaver/migration/consensus/0.0.3-20250521T143022Z-migration-plan.yaml",
	}
	resp, err := client.Post("http://daemon/migration/consensus/soak/start",
		"application/json", bytes.NewReader(mustMarshal(t, payload)))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var errBody daemon.ErrorResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&errBody))
	assert.Equal(t, "cutover_timestamp is required", errBody.Error)
}

func Test_SoakStart_OversizedBody(t *testing.T) {
	client, cancel := startTestDaemon(t)
	defer cancel()

	// Send a body larger than the 16 KiB cap set by MaxBytesReader.
	oversized := make([]byte, 17*1024)
	resp, err := client.Post("http://daemon/migration/consensus/soak/start",
		"application/json", bytes.NewReader(oversized))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func Test_ReadHeaderTimeout(t *testing.T) {
	_, sockPath, cancel := startTestDaemonWithConfig(t, daemon.ServerConfig{
		ReadHeaderTimeout: 50 * time.Millisecond,
	})
	defer cancel()

	// Connect and deliberately send no headers — server must close the
	// connection after ReadHeaderTimeout elapses.
	conn, err := net.Dial("unix", sockPath)
	require.NoError(t, err)
	defer conn.Close()

	// Give the server enough time to fire the timeout, then attempt a read.
	// The server will have closed the connection, so Read returns an error.
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	assert.Error(t, err, "server should close the connection after ReadHeaderTimeout")
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
