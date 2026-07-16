// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/daemon/blocknode"
	"github.com/hashgraph/solo-weaver/internal/daemon/blocknode/statuszmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStatuszClient_DecodesMockServer proves the mock statusz server's wire
// shape is exactly what the production StatuszClient decodes — the contract both
// sides depend on. This is the read half of the MVP loop (fetch + decode);
// applying the reconciled membership to nft is Linux-only and covered by the
// VM integration test.
func TestStatuszClient_DecodesMockServer(t *testing.T) {
	roster := statuszmock.Roster{
		Inbound: []statuszmock.Connection{
			{Remote: statuszmock.Endpoint{Address: "10.10.1.0/24", Port: "*"}, Category: "publisher", TLSRequired: true},
			{Remote: statuszmock.Endpoint{Address: "10.20.1.0/24", Port: "*"}, Category: "partner", TLSRequired: true},
		},
		Outbound: []statuszmock.Connection{
			{Remote: statuszmock.Endpoint{Address: "10.30.5.7", Port: "43473"}, Category: "peer_bn"},
		},
	}
	srv := httptest.NewServer(statuszmock.Handler(statuszmock.StaticRoster(roster)))
	defer srv.Close()

	client := blocknode.NewStatuszClient(srv.URL)

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
	assert.Equal(t, "peer_bn", outbound.ActiveEndpoints[0].Category)
	assert.Equal(t, "10.30.5.7", outbound.ActiveEndpoints[0].Remote.Address)
	assert.Equal(t, "43473", outbound.ActiveEndpoints[0].Remote.Port)
}
