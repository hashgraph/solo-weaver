// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package statuszmock_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/daemon/blocknode/statuszmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decode(t *testing.T, srv *httptest.Server, path string) []map[string]any {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var body struct {
		ActiveEndpoints []map[string]any `json:"active_endpoints"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body.ActiveEndpoints
}

func TestHandler_StaticRoster(t *testing.T) {
	roster := statuszmock.Roster{
		Inbound: []statuszmock.Connection{
			{Remote: statuszmock.Endpoint{Address: "10.10.1.0/24", Port: "*"}, Category: "publisher"},
		},
		Outbound: []statuszmock.Connection{
			{Remote: statuszmock.Endpoint{Address: "10.30.5.7", Port: "43473"}, Category: "peer_bn"},
		},
	}
	srv := httptest.NewServer(statuszmock.Handler(statuszmock.StaticRoster(roster)))
	defer srv.Close()

	in := decode(t, srv, "/statusz/inbound-clients")
	require.Len(t, in, 1)
	assert.Equal(t, "publisher", in[0]["category"])

	out := decode(t, srv, "/statusz/outbound-clients")
	require.Len(t, out, 1)
	assert.Equal(t, "peer_bn", out[0]["category"])
}

func TestHandler_EmptyRosterReturnsEmptyArray(t *testing.T) {
	srv := httptest.NewServer(statuszmock.Handler(statuszmock.StaticRoster(statuszmock.Roster{})))
	defer srv.Close()

	// active_endpoints must be [] not null so the client always decodes a slice.
	resp, err := srv.Client().Get(srv.URL + "/statusz/inbound-clients")
	require.NoError(t, err)
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw))
	assert.Equal(t, "[]", string(raw["active_endpoints"]))
}

func TestFileRoster_ReReadsBetweenRequests(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roster.json")

	srv := httptest.NewServer(statuszmock.Handler(statuszmock.FileRoster(path)))
	defer srv.Close()

	// Missing file → empty roster, not an error.
	assert.Empty(t, decode(t, srv, "/statusz/inbound-clients"))

	write := func(category string) {
		r := statuszmock.Roster{Inbound: []statuszmock.Connection{
			{Remote: statuszmock.Endpoint{Address: "10.1.0.1/32"}, Category: category},
		}}
		data, err := json.Marshal(r)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0o600))
	}

	write("publisher")
	in := decode(t, srv, "/statusz/inbound-clients")
	require.Len(t, in, 1)
	assert.Equal(t, "publisher", in[0]["category"])

	// Edit the file; the next request must reflect it (live re-read).
	write("partner")
	in = decode(t, srv, "/statusz/inbound-clients")
	require.Len(t, in, 1)
	assert.Equal(t, "partner", in[0]["category"])
}

func TestFileRoster_MalformedJSONErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roster.json")
	require.NoError(t, os.WriteFile(path, []byte("{ not json"), 0o600))

	srv := httptest.NewServer(statuszmock.Handler(statuszmock.FileRoster(path)))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/statusz/inbound-clients")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
