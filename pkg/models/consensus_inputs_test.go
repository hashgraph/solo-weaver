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

func TestValidateConsensusAccountId(t *testing.T) {
	for _, ok := range []string{"0.0.3", "0.0.4", "1.2.1001"} {
		assert.NoError(t, ValidateConsensusAccountId(ok), ok)
	}
	// A bare node number is the mistake this guards against.
	for _, bad := range []string{"", "0", "1", "0.3", "0.0.", "0.0.3.4", "0.0.x", " 0.0.3", "0.0.3;ls"} {
		assert.Error(t, ValidateConsensusAccountId(bad), bad)
	}
}

func TestConsensusNodeInputsValidate_AccountId(t *testing.T) {
	in := ConsensusNodeInputs{Namespace: "ns", AccountId: "0.0.3", Weight: 500}
	assert.NoError(t, in.Validate())

	in.AccountId = "0"
	assert.Error(t, in.Validate())
}
