// SPDX-License-Identifier: Apache-2.0

package featuregate_test

import (
	"testing"

	gatekit "github.com/automa-saga/gatekit"
	"github.com/hashgraph/solo-weaver/internal/featuregate"
	"github.com/stretchr/testify/require"
)

func TestOverrideEnvVar_CamelToScreamingSnake(t *testing.T) {
	require.Equal(t,
		"SOLO_PROVISIONER_ENABLE_CONSENSUS_NODE_OPERATOR_MANAGED",
		featuregate.OverrideEnvVar(featuregate.ConsensusNodeOperatorManaged))
	require.Equal(t,
		"SOLO_PROVISIONER_ENABLE_BLOCK_NODE_PROVISIONER_MANAGED",
		featuregate.OverrideEnvVar(featuregate.BlockNodeProvisionerManaged))
}

func TestEnvOverride_EnablesAlphaFeature(t *testing.T) {
	// An alpha feature is off by default at the current activation version.
	g, err := featuregate.NewDefault()
	require.NoError(t, err)
	require.False(t, g.Enabled(featuregate.ConsensusNodeOperatorManaged))

	// Setting its opt-in env var forces it on, with no --experimental.
	t.Setenv(featuregate.OverrideEnvVar(featuregate.ConsensusNodeOperatorManaged), "1")
	g2, err := featuregate.NewDefault()
	require.NoError(t, err)
	require.True(t, g2.Enabled(featuregate.ConsensusNodeOperatorManaged))

	// Explicit Options.Overrides still wins over the env var.
	g3, err := featuregate.New(featuregate.Options{
		Overrides: map[gatekit.Feature]bool{featuregate.ConsensusNodeOperatorManaged: false},
	})
	require.NoError(t, err)
	require.False(t, g3.Enabled(featuregate.ConsensusNodeOperatorManaged))
}
