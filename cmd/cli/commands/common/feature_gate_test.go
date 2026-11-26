// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	gatekit "github.com/automa-saga/gatekit"
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

// A command gated on a feature that is not in the catalog (a wiring bug) fails closed:
// it is hidden and refuses to run, rather than silently running for everyone.
func TestGateFeature_UncataloguedFeatureFailsClosed(t *testing.T) {
	// A no-op parent pre-run so the gate chains onto it, not the real root pre-run.
	cmd := &cobra.Command{Use: "demo", Short: "Demo",
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	GateFeature(cmd, gatekit.Feature("NotInCatalog"))

	require.True(t, cmd.Hidden, "an uncatalogued gated command must be hidden")
	require.Error(t, cmd.PersistentPreRunE(cmd, nil), "it must refuse to run")
}

// Gating a flag that is not registered (a wiring bug) fails closed: the command
// refuses to run, rather than silently leaving the flag ungated.
func TestGateFeatureFlag_UnregisteredFlagFailsClosed(t *testing.T) {
	cmd := &cobra.Command{Use: "demo",
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	GateFeatureFlag(cmd, "nope", featuregate.ConsensusNodeOperatorManaged) // "nope" not registered

	require.Error(t, cmd.PersistentPreRunE(cmd, nil))
}

// GateBehavior installs the sudo-safe --experimental on a GA command that reads a
// code-path gate: the command always runs, and --experimental turns the declared
// feature on for business code via the shared context gate.
func TestGateBehavior_ExperimentalEnablesDeclaredFeature(t *testing.T) {
	cmd := &cobra.Command{Use: "demo",
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	GateBehavior(cmd, featuregate.ConsensusNodeOperatorManaged)

	// Not opted in: the command runs, the feature is off for business code.
	require.NoError(t, cmd.ParseFlags(nil))
	require.NoError(t, cmd.PersistentPreRunE(cmd, nil))
	off, err := featuregate.Gate(cmd.Context())
	require.NoError(t, err)
	require.False(t, off.Enabled(featuregate.ConsensusNodeOperatorManaged))

	// --experimental turns it on (a flag, so it survives sudo).
	require.NoError(t, cmd.ParseFlags([]string{"--" + experimentalFlagName}))
	require.NoError(t, cmd.PersistentPreRunE(cmd, nil))
	on, err := featuregate.Gate(cmd.Context())
	require.NoError(t, err)
	require.True(t, on.Enabled(featuregate.ConsensusNodeOperatorManaged))
}

// --experimental on a GateBehavior command is scoped to the declared features only.
func TestGateBehavior_ScopedToDeclaredFeatures(t *testing.T) {
	cmd := &cobra.Command{Use: "demo",
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	GateBehavior(cmd, featuregate.ConsensusNodeOperatorManaged) // declare only this one

	require.NoError(t, cmd.ParseFlags([]string{"--" + experimentalFlagName}))
	require.NoError(t, cmd.PersistentPreRunE(cmd, nil))
	g, err := featuregate.Gate(cmd.Context())
	require.NoError(t, err)
	require.True(t, g.Enabled(featuregate.ConsensusNodeOperatorManaged))
	require.False(t, g.Enabled(featuregate.KubeClusterUpgrade)) // not declared → stays off
}

// A declared feature that is not in the catalog (a wiring bug) fails closed on every run.
func TestGateBehavior_UncataloguedFailsClosed(t *testing.T) {
	cmd := &cobra.Command{Use: "demo",
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	GateBehavior(cmd, gatekit.Feature("NotInCatalog"))

	require.Error(t, cmd.PersistentPreRunE(cmd, nil))
}

// A child that installs its own hook (here GateBehavior) must not shadow an ancestor
// group's gate: running the child is still refused until the group is opted in.
func TestNestedGate_ChildDoesNotShadowParent(t *testing.T) {
	parent := &cobra.Command{Use: "parent",
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	GateFeature(parent, featuregate.ConsensusNodeOperatorManaged) // alpha group
	child := &cobra.Command{Use: "child", RunE: func(*cobra.Command, []string) error { return nil }}
	parent.AddCommand(child)
	GateBehavior(child, featuregate.KubeClusterUpgrade) // child installs its own hook
	parent.SilenceErrors, parent.SilenceUsage = true, true

	// Without opting in, the parent's gate still refuses the child.
	parent.SetArgs([]string{"child"})
	require.Error(t, parent.Execute())

	// With --experimental, the child runs.
	parent.SetArgs([]string{"child", "--experimental"})
	require.NoError(t, parent.Execute())
}

// Stacking GateFeature + GateBehavior on one command must accumulate, not replace: both
// features resolve on for business code.
func TestStackedGates_ShareContext(t *testing.T) {
	cmd := &cobra.Command{Use: "demo",
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	GateFeature(cmd, featuregate.ConsensusNodeOperatorManaged)
	GateBehavior(cmd, featuregate.KubeClusterUpgrade)

	require.NoError(t, cmd.ParseFlags([]string{"--" + experimentalFlagName}))
	require.NoError(t, cmd.PersistentPreRunE(cmd, nil))

	g, err := featuregate.Gate(cmd.Context())
	require.NoError(t, err)
	require.True(t, g.Enabled(featuregate.ConsensusNodeOperatorManaged)) // from GateFeature
	require.True(t, g.Enabled(featuregate.KubeClusterUpgrade))           // from GateBehavior — not lost
}

// When an env =0 kill switch blocks a run, the refusal names the variable, not "set X=1".
func TestRequireFeatureEnabled_EnvKillSwitchHint(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}
	cmd.Flags().Bool(experimentalFlagName, false, "")
	require.NoError(t, cmd.Flags().Set(experimentalFlagName, "true"))
	t.Setenv(featuregate.OverrideEnvVar(featuregate.ConsensusNodeOperatorManaged), "0")

	err := requireFeatureEnabled(cmd, featuregate.ConsensusNodeOperatorManaged)
	require.Error(t, err)
	require.Contains(t, err.Error(), featuregate.OverrideEnvVar(featuregate.ConsensusNodeOperatorManaged)+"=0")
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

// Opting a command in shares one gate with its business code: --experimental turns the
// feature on for both, so featuregate.Gate(ctx) sees it enabled.
func TestRequireFeatureEnabled_SharesGateWithBusinessCode(t *testing.T) {
	cmd := &cobra.Command{Use: "demo"}
	cmd.Flags().Bool(experimentalFlagName, false, "")

	// Without the opt-in the feature is off for business code too.
	off, err := featuregate.Gate(cmd.Context())
	require.NoError(t, err)
	require.False(t, off.Enabled(featuregate.ConsensusNodeOperatorManaged))

	require.NoError(t, cmd.Flags().Set(experimentalFlagName, "true"))
	require.NoError(t, requireFeatureEnabled(cmd, featuregate.ConsensusNodeOperatorManaged))

	// The command installed the gate on its context; business code reads the same on-state.
	on, err := featuregate.Gate(cmd.Context())
	require.NoError(t, err)
	require.True(t, on.Enabled(featuregate.ConsensusNodeOperatorManaged))
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

// A gated PERSISTENT flag declared on a parent is enforced even when it is used on
// a subcommand: the check lives on PersistentPreRunE (cobra inherits it), not
// PreRunE (which it does not), so the subcommand cannot use the inherited flag
// unchecked.
func TestGateFeatureFlag_PersistentFlagEnforcedOnSubcommand(t *testing.T) {
	// A no-op parent pre-run so GateFeatureFlag chains onto it instead of the real
	// root pre-run (global checks + startup migrations) during Execute.
	parent := &cobra.Command{Use: "parent", PersistentPreRunE: func(*cobra.Command, []string) error { return nil }}
	parent.PersistentFlags().String("thing", "", "do a thing")
	ran := false
	child := &cobra.Command{Use: "child", RunE: func(*cobra.Command, []string) error { ran = true; return nil }}
	parent.AddCommand(child)

	GateFeatureFlag(parent, "thing", featuregate.ConsensusNodeOperatorManaged) // alpha
	parent.SilenceErrors, parent.SilenceUsage = true, true

	// Using the inherited persistent flag on the child without opting in is refused,
	// and the child's RunE never runs.
	parent.SetArgs([]string{"child", "--thing", "x"})
	require.Error(t, parent.Execute())
	require.False(t, ran)

	// With --experimental, the same invocation is allowed and the child runs.
	parent.SetArgs([]string{"child", "--thing", "x", "--experimental"})
	require.NoError(t, parent.Execute())
	require.True(t, ran)
}
