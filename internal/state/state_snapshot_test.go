// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// nestedState returns a State with a value behind every kind of nested
// reference the state model has: pointers, maps, slices and a pointer inside a map.
func nestedState(stateFile string) State {
	prio := 1
	s := NewState(stateFile)
	s.MachineState.Software["kubectl"] = SoftwareState{Name: "kubectl", Version: "1.33.0", Installed: true,
		Metadata: models.StringMap{"path": "/usr/local/bin/kubectl"}}
	s.MachineState.Firewall = &HostFirewallState{ManagementCIDRs: []string{"10.0.0.0/8"}, MgmtPorts: []int{22}}
	s.BlockNodeState.Shaping = &ShapingState{
		EgressInterface: "eth0",
		ShapeOverrides:  map[string]models.ShapeOverride{"publisher": {Rate: "800mbit", Prio: &prio}},
	}
	s.ConsensusNodes = map[string]ConsensusNodeState{"node1": {NodeId: 1,
		ConfigHashes: map[string]ConfigHashEntry{"app": {Hash: "abc"}}}}
	s.TeleportState.NodeAgent = TeleportNodeAgentState{Installed: true, Version: "18.1.0"}
	return s
}

func TestPersistedSnapshot_CarriesEveryPersistedRecord(t *testing.T) {
	s := nestedState("/tmp/state.yaml")

	snapshot, err := s.PersistedSnapshot()
	require.NoError(t, err)

	want, err := yaml.Marshal(s)
	require.NoError(t, err)
	got, err := yaml.Marshal(snapshot)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func TestPersistedSnapshot_IsolatedFromMutationOfTheSource(t *testing.T) {
	s := nestedState("/tmp/state.yaml")
	snapshot, err := s.PersistedSnapshot()
	require.NoError(t, err)

	s.MachineState.Software["kubectl"].Metadata["path"] = "/changed"
	s.MachineState.Software["helm"] = SoftwareState{Name: "helm"}
	s.MachineState.Firewall.ManagementCIDRs[0] = "192.0.2.0/24"
	s.BlockNodeState.Shaping.EgressInterface = "eth1"
	*s.BlockNodeState.Shaping.ShapeOverrides["publisher"].Prio = 7
	delete(s.BlockNodeState.Shaping.ShapeOverrides, "publisher")
	s.ConsensusNodes["node1"].ConfigHashes["app"] = ConfigHashEntry{Hash: "changed"}

	require.Equal(t, "/usr/local/bin/kubectl", snapshot.MachineState.Software["kubectl"].Metadata["path"])
	require.NotContains(t, snapshot.MachineState.Software, "helm")
	require.Equal(t, []string{"10.0.0.0/8"}, snapshot.MachineState.Firewall.ManagementCIDRs)
	require.Equal(t, "eth0", snapshot.BlockNodeState.Shaping.EgressInterface)
	require.Contains(t, snapshot.BlockNodeState.Shaping.ShapeOverrides, "publisher")
	require.Equal(t, 1, *snapshot.BlockNodeState.Shaping.ShapeOverrides["publisher"].Prio)
	require.Equal(t, "abc", snapshot.ConsensusNodes["node1"].ConfigHashes["app"].Hash)
}

// A Refresh decodes the file onto a shallow Clone, writing through pointers the
// previous State() copy still holds. The snapshot must not see that write.
func TestPersistedSnapshot_SurvivesRefreshOfAChangedFile(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	sm, err := NewStateManager(WithState(nestedState(stateFile)), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.FlushState())
	require.NoError(t, sm.Refresh())

	plainCopy := sm.State()
	snapshot, err := sm.State().PersistedSnapshot()
	require.NoError(t, err)

	changed := sm.State()
	changed.BlockNodeState.Shaping = &ShapingState{EgressInterface: "eth9"}
	b, err := yaml.Marshal(changed)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(stateFile, b, 0o600))
	require.NoError(t, sm.Refresh())

	require.Equal(t, "eth9", plainCopy.BlockNodeState.Shaping.EgressInterface,
		"precondition: a plain copy of State() aliases the refreshed state")
	require.Equal(t, "eth0", snapshot.BlockNodeState.Shaping.EgressInterface)
	require.Contains(t, snapshot.BlockNodeState.Shaping.ShapeOverrides, "publisher")
}
