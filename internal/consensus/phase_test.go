// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus_test

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/consensus"
	"github.com/stretchr/testify/assert"
)

func TestPhaseConstants_MatchCRDStrings(t *testing.T) {
	// Pin the on-the-wire string values: these must match the CRD and the
	// reconciler exactly, so a rename here is a deliberate contract change.
	assert.Equal(t, "Pending", string(consensus.PhasePending))
	assert.Equal(t, "ReadyForProvisionerDaemon", string(consensus.PhaseReadyForProvisionerDaemon))
	assert.Equal(t, "PendingInfraUpgrade", string(consensus.PhasePendingInfraUpgrade))
	assert.Equal(t, "PendingNodeUpgrade", string(consensus.PhasePendingNodeUpgrade))
	assert.Equal(t, "Succeeded", string(consensus.PhaseSucceeded))
	assert.Equal(t, "Failed", string(consensus.PhaseFailed))
	assert.Equal(t, "DaemonResult", string(consensus.DaemonResultCondition))
	assert.Equal(t, "True", string(consensus.ConditionTrue))
	assert.Equal(t, "False", string(consensus.ConditionFalse))
}

func TestIsTerminal(t *testing.T) {
	// Only Succeeded/Failed are terminal — the reconciler is their sole writer.
	assert.True(t, consensus.PhaseSucceeded.IsTerminal())
	assert.True(t, consensus.PhaseFailed.IsTerminal())

	for _, p := range []consensus.Phase{
		consensus.PhasePending,
		consensus.PhaseReadyForProvisionerDaemon,
		consensus.PhasePendingInfraUpgrade,
		consensus.PhasePendingNodeUpgrade,
	} {
		assert.False(t, p.IsTerminal(), "phase %q must not be terminal", p)
	}
}

func TestIsDaemonWritable(t *testing.T) {
	// The daemon writes exactly the durable resume anchor and the handshake phase.
	assert.True(t, consensus.PhasePendingInfraUpgrade.IsDaemonWritable())
	assert.True(t, consensus.PhasePendingNodeUpgrade.IsDaemonWritable())

	// The daemon must never write reconciler-owned phases.
	for _, p := range []consensus.Phase{
		consensus.PhasePending,
		consensus.PhaseReadyForProvisionerDaemon,
		consensus.PhaseSucceeded,
		consensus.PhaseFailed,
	} {
		assert.False(t, p.IsDaemonWritable(), "daemon must not write phase %q", p)
	}
}

func TestDaemonNeverWritesTerminal(t *testing.T) {
	// The core #706 invariant: no phase is both daemon-writable and terminal.
	for _, p := range []consensus.Phase{
		consensus.PhasePending,
		consensus.PhaseReadyForProvisionerDaemon,
		consensus.PhasePendingInfraUpgrade,
		consensus.PhasePendingNodeUpgrade,
		consensus.PhaseSucceeded,
		consensus.PhaseFailed,
	} {
		assert.False(t, p.IsDaemonWritable() && p.IsTerminal(),
			"phase %q violates the sole-terminal-writer invariant", p)
	}
}

func TestNoInProgressPhase(t *testing.T) {
	// There is deliberately no InProgress phase. Guard against one being
	// reintroduced by asserting none of the defined phases use that string.
	for _, p := range []consensus.Phase{
		consensus.PhasePending,
		consensus.PhaseReadyForProvisionerDaemon,
		consensus.PhasePendingInfraUpgrade,
		consensus.PhasePendingNodeUpgrade,
		consensus.PhaseSucceeded,
		consensus.PhaseFailed,
	} {
		assert.NotEqual(t, "InProgress", string(p))
	}
}
