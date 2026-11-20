// SPDX-License-Identifier: Apache-2.0

package featuregate_test

import (
	"testing"

	gatekit "github.com/automa-saga/gatekit"
	"github.com/hashgraph/solo-weaver/internal/featuregate"
	"github.com/stretchr/testify/require"
)

// Proves the two axes the spike exists to demonstrate: the activation-version
// axis (ActiveFrom vs the in-force version) and the maturity axis (Alpha needs
// opt-in), plus per-feature overrides.
func TestGate_VersionAndMaturityAxes(t *testing.T) {
	t.Run("GA feature is on at the GA floor", func(t *testing.T) {
		g, err := featuregate.New(featuregate.Options{Current: 1})
		require.NoError(t, err)
		require.True(t, g.Enabled(featuregate.NetworkPolicy))
		require.True(t, g.Enabled(featuregate.NetworkFirewall))
		require.True(t, g.Enabled(featuregate.NetworkTrafficShaping))
		require.True(t, g.Enabled(featuregate.EsoSecretManagement))
	})

	t.Run("Alpha feature is off without an opt-in", func(t *testing.T) {
		g, err := featuregate.New(featuregate.Options{Current: 1})
		require.NoError(t, err)
		// The spec is active at version 1, but Alpha maturity withholds it until opt-in.
		require.False(t, g.Enabled(featuregate.ConsensusNodeOperatorManaged))
	})

	// Options.Overrides is the raw seam (tests/programmatic): it forces a feature on
	// regardless of version, maturity, or readiness.
	t.Run("a raw override forces on regardless of maturity", func(t *testing.T) {
		g, err := featuregate.New(featuregate.Options{
			Current:   1,
			Overrides: map[gatekit.Feature]bool{featuregate.ConsensusNodeOperatorManaged: true},
		})
		require.NoError(t, err)
		require.True(t, g.Enabled(featuregate.ConsensusNodeOperatorManaged))
		// A GA feature is unaffected by the opt-in.
		require.True(t, g.Enabled(featuregate.NetworkPolicy))
	})
}

// Model C: the operator opt-ins (--experimental / env) respect readiness. Both current
// alphas are Alpha,Default:true, so they are ready to dogfood via an opt-in. (An
// unfinished Alpha,Default:false feature would report false — see TestExperimentalReadyIn.)
func TestExperimentalReady_CurrentAlphas(t *testing.T) {
	for _, f := range []gatekit.Feature{
		featuregate.ConsensusNodeOperatorManaged,
		featuregate.KubeClusterUpgrade,
	} {
		ready, err := featuregate.ExperimentalReady(f)
		require.NoError(t, err)
		require.True(t, ready, "%s should be ready to dogfood", f)
	}
}
