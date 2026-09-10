// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestExperimentalEnvVar(t *testing.T) {
	cases := map[string]string{
		"consensus":    "SOLO_PROVISIONER_ENABLE_CONSENSUS",
		"block-node":   "SOLO_PROVISIONER_ENABLE_BLOCK_NODE",
		"some feature": "SOLO_PROVISIONER_ENABLE_SOME_FEATURE",
		"a.b":          "SOLO_PROVISIONER_ENABLE_A_B",
	}
	for in, want := range cases {
		require.Equal(t, want, ExperimentalEnvVar(in), "for %q", in)
	}
}

// TestGateExperimental_HidesAndRegistersFlag pins the wiring: a gated command is
// hidden, carries the hidden --experimental opt-in, and gets a run-gate hook.
func TestGateExperimental_HidesAndRegistersFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "widget"}
	GateExperimental(cmd, "widget")

	require.True(t, cmd.Hidden, "gated command must be hidden")
	f := cmd.PersistentFlags().Lookup(experimentalFlagName)
	require.NotNil(t, f, "--experimental must be registered")
	require.True(t, f.Hidden, "--experimental must be hidden")
	require.NotNil(t, cmd.PersistentPreRunE, "gate must install a PersistentPreRunE")

	// Idempotent flag registration: gating a second time (or a subcommand under an
	// already-gated group) must not panic on a duplicate flag.
	require.NotPanics(t, func() { GateExperimental(cmd, "widget") })
}

// TestRequireExperimentalEnabled pins the opt-in logic: refused by default with a
// reason + hints; unlocked by the flag or a truthy env var.
func TestRequireExperimentalEnabled(t *testing.T) {
	newCmd := func() *cobra.Command {
		c := &cobra.Command{Use: "widget"}
		c.Flags().Bool(experimentalFlagName, false, "")
		return c
	}
	envVar := ExperimentalEnvVar("widget")

	// Default: no flag, no env -> refused with reason + hints.
	err := requireExperimentalEnabled(newCmd(), "widget", envVar)
	require.Error(t, err)
	reason, ok := errx.ReasonOf(err)
	require.True(t, ok, "gate error must carry a reason")
	require.Equal(t, reasons.PreconditionNotMet, reason)
	hints, ok := errx.Hints(err)
	require.True(t, ok)
	require.NotEmpty(t, hints)

	// --experimental flag opt-in.
	c := newCmd()
	require.NoError(t, c.Flags().Set(experimentalFlagName, "true"))
	require.NoError(t, requireExperimentalEnabled(c, "widget", envVar))

	// Truthy env opt-in.
	t.Setenv(envVar, "1")
	require.NoError(t, requireExperimentalEnabled(newCmd(), "widget", envVar))

	// Falsy env is still refused.
	t.Setenv(envVar, "0")
	require.Error(t, requireExperimentalEnabled(newCmd(), "widget", envVar))
}
