// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/joomcode/errorx"
)

// SoakStart sends POST /consensus_node/migration/soak/start to the daemon socket
// and returns the accepted response. Returns an error if the daemon rejects the
// request (e.g. 409 Conflict when a soak is already active).
func SoakStart(sockPath string, req consensus.SoakStartRequest) (*consensus.SoakStartResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "marshal soak start request")
	}

	resp, err := socketClient(sockPath).Post(
		"http://local/consensus_node/migration/soak/start",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "soak start")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return nil, decodeAPIError(resp)
	}

	var out consensus.SoakStartResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "decode soak start response")
	}
	return &out, nil
}

// SoakStop sends DELETE /consensus_node/migration/soak to the daemon socket.
// When keepState is true the query param delete_state=false is appended so
// cutover-state.jsonl is preserved and the daemon will resume on next restart.
// Returns an error if no soak is active (409) or the daemon is unreachable.
func SoakStop(sockPath string, keepState bool) error {
	url := "http://local/consensus_node/migration/soak"
	if keepState {
		url += "?delete_state=false"
	}

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return errorx.InternalError.Wrap(err, "build soak stop request")
	}

	resp, err := soakSocketClient(sockPath).Do(req)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "soak stop")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return decodeAPIError(resp)
	}
	return nil
}

// SoakStatus fetches GET /consensus_node/migration/soak/status from the daemon
// socket. Returns nil if the daemon is unreachable or returns a non-200 status.
func SoakStatus(sockPath string) *consensus.SoakStatusResponse {
	resp, err := socketClient(sockPath).Get("http://local/consensus_node/migration/soak/status")
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer resp.Body.Close()

	var out consensus.SoakStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil
	}
	return &out
}

// decodeAPIError reads the JSON {"error":"..."} body from a non-success response
// and wraps it in a descriptive error.
func decodeAPIError(resp *http.Response) error {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Error != "" {
		return errorx.ExternalError.New("daemon returned %d: %s", resp.StatusCode, body.Error)
	}
	return errorx.ExternalError.New("daemon returned unexpected status %d", resp.StatusCode)
}

// soakClientTimeout is used by the soak client calls. Longer than the default
// socketClient timeout because TryStop blocks until the watcher drains.
const soakClientTimeout = 30 * time.Second

// soakSocketClient returns an HTTP client with a longer timeout for soak stop.
func soakSocketClient(sockPath string) *http.Client {
	c := socketClient(sockPath)
	c.Timeout = soakClientTimeout
	return c
}
