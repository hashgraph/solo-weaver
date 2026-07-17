// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package shaper

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// Sample statusz payloads taken verbatim from the traffic-shaper design doc's
// worked example (network-data.proto JSON encoding). Address may be a CIDR and
// port may be "*" — both are exercised here so the decode boundary is pinned to
// the real BN schema rather than a simplified stand-in.
const sampleInboundClientsJSON = `{
  "active_endpoints": [
    { "local": {"address": "0.0.0.0", "port": "40840"}, "remote": {"address": "10.10.1.0/24", "port": "*"}, "category": "publisher", "tls_required": true },
    { "local": {"address": "0.0.0.0", "port": "40980"}, "remote": {"address": "10.20.1.0/24", "port": "*"}, "category": "partner",   "tls_required": true },
    { "local": {"address": "0.0.0.0", "port": "40980"}, "remote": {"address": "0.0.0.0/0",    "port": "*"}, "category": "public",    "tls_required": false }
  ]
}`

const sampleOutboundClientsJSON = `{
  "active_endpoints": [
    { "local": {"address": "0.0.0.0", "port": "*"}, "remote": {"address": "10.30.5.7", "port": "43473"}, "category": "peer_bn", "tls_required": true }
  ]
}`

// newMockStatuszServer starts an httptest server that serves the given handlers
// for the two statusz endpoints. A nil handler leaves that route unregistered.
func newMockStatuszServer(t *testing.T, inbound, outbound http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	if inbound != nil {
		mux.HandleFunc("/statusz/inbound-clients", inbound)
	}
	if outbound != nil {
		mux.HandleFunc("/statusz/outbound-clients", outbound)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// jsonResponder replies 200 with the given JSON body.
func jsonResponder(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}

func TestStatuszClient_InboundClients_DecodesSample(t *testing.T) {
	srv := newMockStatuszServer(t, jsonResponder(sampleInboundClientsJSON), nil)
	c := NewStatuszClient(srv.URL)

	data, err := c.InboundClients(context.Background())
	require.NoError(t, err)
	require.Len(t, data.ActiveEndpoints, 3)

	pub := data.ActiveEndpoints[0]
	require.Equal(t, "publisher", pub.Category)
	require.Equal(t, "10.10.1.0/24", pub.Remote.Address)
	require.Equal(t, "*", pub.Remote.Port)
	require.True(t, pub.TLSRequired)
	// local is decoded too — #755's install path reads local.port.
	require.Equal(t, "0.0.0.0", pub.Local.Address)
	require.Equal(t, "40840", pub.Local.Port)

	require.Equal(t, "partner", data.ActiveEndpoints[1].Category)

	pubClear := data.ActiveEndpoints[2]
	require.Equal(t, "public", pubClear.Category)
	require.False(t, pubClear.TLSRequired)
}

func TestStatuszClient_OutboundClients_DecodesSample(t *testing.T) {
	srv := newMockStatuszServer(t, nil, jsonResponder(sampleOutboundClientsJSON))
	c := NewStatuszClient(srv.URL)

	data, err := c.OutboundClients(context.Background())
	require.NoError(t, err)
	require.Len(t, data.ActiveEndpoints, 1)

	peer := data.ActiveEndpoints[0]
	require.Equal(t, "peer_bn", peer.Category)
	require.Equal(t, "10.30.5.7", peer.Remote.Address)
	require.Equal(t, "43473", peer.Remote.Port)
	require.True(t, peer.TLSRequired)
}

func TestStatuszClient_EmptyEndpoints_DecodesToEmptySlice(t *testing.T) {
	srv := newMockStatuszServer(t, jsonResponder(`{"active_endpoints": []}`), nil)
	c := NewStatuszClient(srv.URL)

	data, err := c.InboundClients(context.Background())
	require.NoError(t, err)
	require.Empty(t, data.ActiveEndpoints)
}

func TestStatuszClient_NonOKStatus_ReturnsExternalError(t *testing.T) {
	srv := newMockStatuszServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}, nil)
	c := NewStatuszClient(srv.URL)

	_, err := c.InboundClients(context.Background())
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, errorx.ExternalError))
	require.ErrorContains(t, err, "503")
}

func TestStatuszClient_MalformedBody_ReturnsIllegalFormat(t *testing.T) {
	srv := newMockStatuszServer(t, jsonResponder(`this is not json`), nil)
	c := NewStatuszClient(srv.URL)

	_, err := c.InboundClients(context.Background())
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, errorx.IllegalFormat))
}

func TestStatuszClient_TransportFailure_ReturnsExternalError(t *testing.T) {
	// Start then immediately stop a server so the URL is well-formed but the
	// connection is refused — a transport-level failure, not a status code.
	srv := httptest.NewServer(http.NewServeMux())
	baseURL := srv.URL
	srv.Close()
	c := NewStatuszClient(baseURL)

	_, err := c.InboundClients(context.Background())
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, errorx.ExternalError))
}

func TestStatuszClient_CancelledContext_ReturnsErrorWithoutPanic(t *testing.T) {
	srv := newMockStatuszServer(t, jsonResponder(sampleInboundClientsJSON), nil)
	c := NewStatuszClient(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.InboundClients(ctx)
	require.Error(t, err)
}
