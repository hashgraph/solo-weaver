// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package shaper_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/blocknode/shaper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inboundClientsFixture / outboundClientsFixture are the JSON wire shape of the
// BN statusz endpoints (mirroring block-node/api/network-data.proto: a
// NetworkData with activeEndpoints, camelCase as the BN emits it). They are
// inlined here rather than served by a shared mock so this test owns the exact
// bytes it decodes.
const (
	inboundClientsFixture = `{"activeEndpoints":[
		{"remote":{"address":"10.10.1.0/24","port":"*"},"category":"publisher","tlsRequired":true},
		{"remote":{"address":"10.20.1.0/24","port":"*"},"category":"partner","tlsRequired":true}
	]}`
	outboundClientsFixture = `{"activeEndpoints":[
		{"remote":{"address":"10.30.5.7","port":"43473"},"category":"partner"}
	]}`
)

// TestStatuszClient_DecodesWireShape proves the production StatuszClient decodes
// the BN statusz wire shape exactly — the contract the read half of the MVP loop
// (fetch + decode) depends on. Applying the reconciled membership to nft is
// Linux-only and covered by the VM integration test.
func TestStatuszClient_DecodesWireShape(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/statusz/inbound", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(inboundClientsFixture))
	})
	mux.HandleFunc("/statusz/outbound", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(outboundClientsFixture))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := shaper.NewStatuszClient(srv.URL)

	inbound, err := client.InboundClients(context.Background())
	require.NoError(t, err)
	require.Len(t, inbound.ActiveEndpoints, 2)
	assert.Equal(t, "publisher", inbound.ActiveEndpoints[0].Category)
	assert.Equal(t, "10.10.1.0/24", inbound.ActiveEndpoints[0].Remote.Address)
	assert.True(t, inbound.ActiveEndpoints[0].TLSRequired)
	assert.Equal(t, "partner", inbound.ActiveEndpoints[1].Category)

	outbound, err := client.OutboundClients(context.Background())
	require.NoError(t, err)
	require.Len(t, outbound.ActiveEndpoints, 1)
	assert.Equal(t, "partner", outbound.ActiveEndpoints[0].Category)
	assert.Equal(t, "10.30.5.7", outbound.ActiveEndpoints[0].Remote.Address)
	assert.Equal(t, "43473", outbound.ActiveEndpoints[0].Remote.Port)
}
