// SPDX-License-Identifier: Apache-2.0

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
