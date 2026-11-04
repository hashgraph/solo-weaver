// SPDX-License-Identifier: Apache-2.0

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConsensusNodeName(t *testing.T) {
	assert.Equal(t, "node1", ConsensusNodeName(0, false))
	assert.Equal(t, "node3", ConsensusNodeName(2, false))
	assert.Equal(t, "node0", ConsensusNodeName(0, true))
	assert.Equal(t, "node2", ConsensusNodeName(2, true))
	// Secret and resource names stay 0-based regardless of the CN convention.
	assert.Equal(t, "node2", ConsensusNodeScope(2))
}

func TestConsensusDefaultAccountId(t *testing.T) {
	assert.Equal(t, "0.0.3", ConsensusDefaultAccountId(0))
	assert.Equal(t, "0.0.7", ConsensusDefaultAccountId(4))
}
