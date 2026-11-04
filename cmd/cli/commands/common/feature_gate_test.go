// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/featuregate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// GateFeature hides an alpha command, labels it, and installs the hidden
// --experimental opt-in.
func TestGateFeature_HidesAlphaAndInstallsFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "demo", Short: "Demo"}
	GateFeature(cmd, featuregate.ConsensusNodeOperatorManaged) // alpha

	require.True(t, cmd.Hidden)
	require.Contains(t, cmd.Short, "[alpha]")

	f := cmd.PersistentFlags().Lookup(experimentalFlagName)
	require.NotNil(t, f)
	require.True(t, f.Hidden)
}

// An alpha command is refused until opted in; --experimental enables it even
// though the feature's activation version is not the current one.
func TestRequireFeatureEnabled_OptIn(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}
	cmd.Flags().Bool(experimentalFlagName, false, "")

	require.Error(t, requireFeatureEnabled(cmd, featuregate.ConsensusNodeOperatorManaged))

	require.NoError(t, cmd.Flags().Set(experimentalFlagName, "true"))
	require.NoError(t, requireFeatureEnabled(cmd, featuregate.ConsensusNodeOperatorManaged))
}

// The per-feature override env var enables an alpha command with no flag.
func TestRequireFeatureEnabled_EnvOverride(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}
	cmd.Flags().Bool(experimentalFlagName, false, "")

	t.Setenv(featuregate.OverrideEnvVar(featuregate.ConsensusNodeOperatorManaged), "1")
	require.NoError(t, requireFeatureEnabled(cmd, featuregate.ConsensusNodeOperatorManaged))
}

// GateFeatureFlag hides and labels an alpha flag, installs the opt-in, and
// refuses the flag when it is set without opting in.
func TestGateFeatureFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}
	cmd.Flags().String("thing", "", "do a thing")
	GateFeatureFlag(cmd, "thing", featuregate.ConsensusNodeOperatorManaged) // alpha

	fl := cmd.Flags().Lookup("thing")
	require.True(t, fl.Hidden)
	require.Contains(t, fl.Usage, "[alpha]")
	require.NotNil(t, cmd.PersistentFlags().Lookup(experimentalFlagName))

	// Flag not set -> allowed.
	require.NoError(t, cmd.PreRunE(cmd, nil))

	// Flag set without opt-in -> refused.
	require.NoError(t, cmd.Flags().Set("thing", "x"))
	require.Error(t, cmd.PreRunE(cmd, nil))
}
