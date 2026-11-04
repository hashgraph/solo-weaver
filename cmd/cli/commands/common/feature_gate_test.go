// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/featuregate"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// GateFeature labels an alpha command and installs the hidden --experimental
// opt-in, without hiding the command (mark + gate).
func TestGateFeature_LabelsAndFlagNoHide(t *testing.T) {
	cmd := &cobra.Command{Use: "demo", Short: "Demo"}
	GateFeature(cmd, featuregate.ConsensusNodeOperatorManaged) // alpha

	require.False(t, cmd.Hidden)
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
