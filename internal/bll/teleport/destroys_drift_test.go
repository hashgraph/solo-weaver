// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package teleport

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/drift"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

func nodeAgent(installed, configured bool) state.TeleportState {
	return state.TeleportState{NodeAgent: state.TeleportNodeAgentState{Installed: installed, Configured: configured}}
}

func clusterAgent(installed bool, chartVersion string) state.TeleportState {
	t := state.TeleportState{}
	if installed {
		t.ClusterAgent = state.TeleportClusterAgentState{
			Installed: true, Release: "teleport-agent", Namespace: "teleport-agent", ChartVersion: chartVersion,
		}
	}
	return t
}

// unobservedNodeAgent is t as the checker returns it when it could not read the
// node agent: the recorded agent, marked unobserved.
func unobservedNodeAgent(t state.TeleportState) state.TeleportState {
	t.NodeAgent.Unobserved = true
	return t
}

// unobservedClusterAgent is t as the checker returns it when it could not read
// the cluster agent: the recorded agent, marked unobserved.
func unobservedClusterAgent(t state.TeleportState) state.TeleportState {
	t.ClusterAgent.Unobserved = true
	return t
}

// change is the one change the Teleport producer reports from persisted to live.
func change(t *testing.T, persisted, live state.TeleportState) drift.Change {
	t.Helper()
	var baseline, observed state.State
	baseline.TeleportState, observed.TeleportState = persisted, live
	changes := drift.Teleport(baseline, observed)
	require.Len(t, changes, 1)
	return changes[0]
}

// An uninstall destroys out-of-band changes to its own agent, and nothing on
// the other agent.
func TestUninstall_DestroysOnlyItsOwnAgentsDrift(t *testing.T) {
	cases := []struct {
		name             string
		persisted, live  state.TeleportState
		nodeUninstall    bool
		clusterUninstall bool
	}{
		{"node agent configured by hand", nodeAgent(true, false), nodeAgent(true, true), true, false},
		{"node agent installed by hand", nodeAgent(false, false), nodeAgent(true, true), true, false},
		{"cluster agent chart pinned by hand", clusterAgent(true, "18.1.0"), clusterAgent(true, "18.2.1"), false, true},
		{"cluster agent installed by hand", clusterAgent(false, ""), clusterAgent(true, "18.2.1"), false, true},
		{"node agent could not be read", nodeAgent(true, true), unobservedNodeAgent(nodeAgent(true, true)), true, false},
		{"cluster agent could not be read", clusterAgent(true, "18.1.0"), unobservedClusterAgent(clusterAgent(true, "18.1.0")), false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := change(t, tc.persisted, tc.live)
			require.Equal(t, tc.nodeUninstall,
				(&NodeUninstallHandler{}).DestroysDrift(models.UserInputs[models.TeleportNodeInputs]{}, c), "node uninstall")
			require.Equal(t, tc.clusterUninstall,
				(&ClusterUninstallHandler{}).DestroysDrift(models.UserInputs[models.TeleportClusterInputs]{}, c), "cluster uninstall")
		})
	}
}
