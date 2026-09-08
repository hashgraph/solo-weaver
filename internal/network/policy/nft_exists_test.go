// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/network/nftexec/nfttest"
)

// The present/absent/unknown contract is covered once in nftexec; this only
// pins that Exists probes THIS package's TableName, not a copy-pasted one.
func TestExistsProbesOwnTable(t *testing.T) {
	r := &execRunner{bin: nfttest.FakeNft(t, "table inet weaver-host-firewall\n", 0)}
	present, err := r.Exists(context.Background())
	require.NoError(t, err)
	assert.False(t, present, "only the other manager's table is live")

	r = &execRunner{bin: nfttest.FakeNft(t, "table "+TableName+"\n", 0)}
	present, err = r.Exists(context.Background())
	require.NoError(t, err)
	assert.True(t, present)
}
