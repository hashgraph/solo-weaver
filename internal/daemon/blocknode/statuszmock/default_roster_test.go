// SPDX-License-Identifier: Apache-2.0

package statuszmock_test

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/daemon/blocknode/statuszmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRoster_CarriesPerFacilityListenerPorts(t *testing.T) {
	r := statuszmock.DefaultRoster()

	// Collect the local (BN-side) listener ports reported per category so the
	// traffic-shaper derivation has realistic per-facility ports to reconcile.
	ports := map[string]map[string]bool{}
	for _, c := range r.Inbound {
		if ports[c.Category] == nil {
			ports[c.Category] = map[string]bool{}
		}
		ports[c.Category][c.Local.Port] = true
	}

	assert.True(t, ports["publisher"]["40984"], "publisher listens on 40984")
	assert.True(t, ports["partner"]["40980"], "partners subscribe on 40980")
	assert.True(t, ports["public"]["40980"], "public subscriber on 40980")
	assert.True(t, ports["public"]["40981"], "public block-access on 40981")
	assert.True(t, ports["public"]["40982"], "public server-status on 40982")

	require.NotEmpty(t, r.Outbound, "the backfill peer appears as an outbound partner")
}
