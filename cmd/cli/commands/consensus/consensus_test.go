// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/stretchr/testify/require"
)

// TestConsensusGroupIsHiddenAndGated pins the production-safety contract: the
// consensus stack is not production-ready, so the whole group is hidden from
// --help and its opt-in flag is hidden too. solo-provisioner ships to production
// block-node operators, so a regression here would expose an unfinished feature.
func TestConsensusGroupIsHiddenAndGated(t *testing.T) {
	cmd := GetCmd()
	require.True(t, cmd.Hidden, "consensus group must be hidden — it is not production-ready")

	f := cmd.PersistentFlags().Lookup("experimental")
	require.NotNil(t, f, "--experimental gate flag must be registered")
	require.True(t, f.Hidden, "--experimental must be hidden from --help")
}

// TestRequireConsensusEnabled pins the run-gate: without an explicit opt-in the
// commands refuse to run, with a reason code and operator-actionable hints; the
// flag or a truthy env var unlocks them.
func TestRequireConsensusEnabled(t *testing.T) {
	t.Cleanup(func() { flagExperimental = false })

	// Default (no flag, no env): refused with reason + hints.
	flagExperimental = false
	err := requireConsensusEnabled()
	require.Error(t, err, "consensus must be gated by default")
	reason, ok := errx.ReasonOf(err)
	require.True(t, ok, "gate error must carry a reason code")
	require.Equal(t, reasons.PreconditionNotMet, reason)
	hints, ok := errx.Hints(err)
	require.True(t, ok, "gate error must carry hints")
	require.NotEmpty(t, hints)

	// --experimental flag opt-in.
	flagExperimental = true
	require.NoError(t, requireConsensusEnabled())
	flagExperimental = false

	// Truthy env opt-in.
	t.Setenv(consensusEnabledEnv, "1")
	require.NoError(t, requireConsensusEnabled())

	// Falsy env is still refused.
	t.Setenv(consensusEnabledEnv, "0")
	require.Error(t, requireConsensusEnabled())
}
