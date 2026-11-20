// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package node

import (
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateNodeSelectorFlags_HintNamesTheVerb(t *testing.T) {
	err := validateNodeSelectorFlags(false, "hiero-network-1", 0, "stop")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--node-id is required")
}

func TestConsensusRunStateInputs_NoProfileAndValid(t *testing.T) {
	origNs, origId, origProfile := flagNamespace, flagNodeId, flagProfile
	t.Cleanup(func() { flagNamespace, flagNodeId, flagProfile = origNs, origId, origProfile })
	flagNamespace, flagNodeId, flagProfile = "hiero-network-1", 2, "mainnet"

	inputs := consensusRunStateInputs(90 * time.Second)

	assert.Empty(t, inputs.Custom.Profile)
	assert.Equal(t, "hiero-network-1", inputs.Custom.Namespace)
	assert.Equal(t, int64(2), inputs.Custom.NodeId)
	assert.Equal(t, 90*time.Second, inputs.Custom.ReadyTimeout)
	for _, a := range []models.ActionType{models.ActionStart, models.ActionStop} {
		require.NoError(t, inputs.ValidateFor(a), "the base handler validates these inputs")
	}
}

func TestRunStateCommandsAreRegistered(t *testing.T) {
	for _, name := range []string{"start", "stop"} {
		cmd, _, err := nodeCmd.Find([]string{name})
		require.NoError(t, err)
		assert.Equal(t, name, cmd.Name())
		f := cmd.Flags().Lookup("ready-timeout")
		require.NotNil(t, f, "%s needs --ready-timeout", name)
		assert.Equal(t, "5m0s", f.DefValue)
	}
}
