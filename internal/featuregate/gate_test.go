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

	t.Run("Alpha feature is off before its activation version", func(t *testing.T) {
		g, err := featuregate.New(featuregate.Options{Current: 1})
		require.NoError(t, err)
		// ActiveFrom 2 has not been reached: no active spec, so off.
		require.False(t, g.Enabled(featuregate.BlockNodeOperatorManaged))
	})

	t.Run("Alpha at its version is still off without --experimental", func(t *testing.T) {
		g, err := featuregate.New(featuregate.Options{Current: 2})
		require.NoError(t, err)
		// Spec is active, but Alpha maturity withholds it until opt-in.
		require.False(t, g.Enabled(featuregate.BlockNodeOperatorManaged))
	})

	t.Run("Alpha at its version turns on with --experimental", func(t *testing.T) {
		g, err := featuregate.New(featuregate.Options{Current: 2, Experimental: true})
		require.NoError(t, err)
		require.True(t, g.Enabled(featuregate.BlockNodeOperatorManaged))
		// A GA feature is unaffected by the opt-in.
		require.True(t, g.Enabled(featuregate.NetworkPolicy))
	})

	t.Run("per-feature override forces on regardless of version/maturity", func(t *testing.T) {
		g, err := featuregate.New(featuregate.Options{
			Current:   1, // before BlockNodeOperatorManaged's ActiveFrom
			Overrides: map[gatekit.Feature]bool{featuregate.BlockNodeOperatorManaged: true},
		})
		require.NoError(t, err)
		require.True(t, g.Enabled(featuregate.BlockNodeOperatorManaged))
	})
}
