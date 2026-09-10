// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConsensusGroupIsGated pins the production-safety contract: the consensus
// stack is not production-ready, so the whole group must stay hidden and behind
// the experimental gate. solo-provisioner ships to production block-node
// operators, so dropping the gate would expose an unfinished feature. The gate
// mechanism itself is covered by common.GateExperimental's tests.
func TestConsensusGroupIsGated(t *testing.T) {
	cmd := GetCmd()
	require.True(t, cmd.Hidden, "consensus group must be hidden — it is not production-ready")

	f := cmd.PersistentFlags().Lookup("experimental")
	require.NotNil(t, f, "consensus must be behind the experimental gate")
	require.True(t, f.Hidden, "--experimental must be hidden from --help")
}
