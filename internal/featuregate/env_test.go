// SPDX-License-Identifier: Apache-2.0

package featuregate_test

import (
	"strings"
	"testing"

	"github.com/automa-saga/errx"
	gatekit "github.com/automa-saga/gatekit"
	"github.com/hashgraph/solo-weaver/internal/featuregate"
	"github.com/stretchr/testify/require"
)

func TestOverrideEnvVar_CamelToScreamingSnake(t *testing.T) {
	require.Equal(t,
		"SOLO_PROVISIONER_ENABLE_CONSENSUS_NODE_OPERATOR_MANAGED",
		featuregate.OverrideEnvVar(featuregate.ConsensusNodeOperatorManaged))
	require.Equal(t,
		"SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE",
		featuregate.OverrideEnvVar(featuregate.KubeClusterUpgrade))
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

// An env off-request (=0) is a kill switch: it wins over an explicit override such as
// the --experimental opt-in, so a feature an operator disabled cannot be resurrected.
func TestEnvOffRequest_WinsOverOverride(t *testing.T) {
	t.Setenv(featuregate.OverrideEnvVar(featuregate.KubeClusterUpgrade), "0")
	g, err := featuregate.New(featuregate.Options{
		Overrides: map[gatekit.Feature]bool{featuregate.KubeClusterUpgrade: true}, // like --experimental
	})
	require.NoError(t, err)
	require.False(t, g.Enabled(featuregate.KubeClusterUpgrade))
}

// A mistyped opt-in var that matches no feature fails fast (error-and-stop).
func TestEnvOverride_UnknownVarErrors(t *testing.T) {
	t.Setenv("SOLO_PROVISIONER_ENABLE_NOPE", "1")
	_, err := featuregate.NewDefault()
	require.Error(t, err)
	require.Contains(t, err.Error(), "SOLO_PROVISIONER_ENABLE_NOPE")
}

// A renamed opt-in var from older docs gives a hint naming the current variable.
func TestEnvOverride_RenamedVarHint(t *testing.T) {
	t.Setenv("SOLO_PROVISIONER_ENABLE_CLUSTER_UPGRADE", "1")
	_, err := featuregate.NewDefault()
	require.Error(t, err)
	hints, ok := errx.Hints(err)
	require.True(t, ok)
	require.Contains(t, strings.Join(hints, " "), "SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE")
}
