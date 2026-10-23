// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// refreshNodeAgent runs the teleport checker against persisted state, with the
// node agent installer reporting the 18.11.3 catalog default the way the real
// one does.
func refreshNodeAgent(t *testing.T, persisted state.State, installed bool) state.TeleportNodeAgentState {
	t.Helper()
	inst := installerReporting(t, "teleport", "18.11.3", installed)
	orig := newTeleportNodeAgentInstaller
	newTeleportNodeAgentInstaller = func(...software.InstallerOption) (software.Software, error) { return inst, nil }
	t.Cleanup(func() { newTeleportNodeAgentInstaller = orig })

	checker, err := NewTeleportChecker(fakeMachineStateManager{st: persisted}, nil, func() ClusterReachability { return NotConfigured })
	require.NoError(t, err)
	ts, err := checker.RefreshState(context.Background())
	require.NoError(t, err)
	return ts.NodeAgent
}

func withNodeAgentVersion(s state.State, version string) state.State {
	s.TeleportState.NodeAgent = state.TeleportNodeAgentState{Installed: true, Configured: true, Version: version}
	return s
}

func TestTeleportNodeAgentVersion_ExistingAgentKeepsItsRecordedVersion(t *testing.T) {
	calls := stubCatalog(t, []string{"18.6.4", "18.11.3"}, nil, nil)
	persisted := withNodeAgentVersion(persistedSoftware(map[string]state.SoftwareState{
		"teleport": {Name: "teleport", Version: "18.6.4", Installed: true},
	}), "18.6.4")

	agent := refreshNodeAgent(t, persisted, true)

	assert.True(t, agent.Installed)
	assert.Equal(t, "18.6.4", agent.Version)
	assert.Equal(t, 0, *calls, "a valid record is kept without hashing")
}

// The agent's own record is not a source: earlier releases wrote the catalog
// default there. Without a machine record the binaries decide.
func TestTeleportNodeAgentVersion_AgentRecordIsNotTrusted(t *testing.T) {
	calls := stubCatalog(t, []string{"18.6.4", "18.11.3"}, map[string][]string{"teleport": {"18.6.4"}}, nil)
	persisted := withNodeAgentVersion(persistedSoftware(nil), "18.11.3")

	agent := refreshNodeAgent(t, persisted, true)

	assert.Equal(t, "18.6.4", agent.Version)
	assert.Equal(t, 1, *calls)
}

func TestTeleportNodeAgentVersion_FreshInstallIsIdentified(t *testing.T) {
	stubCatalog(t, []string{"18.6.4", "18.11.3"}, map[string][]string{"teleport": {"18.11.3"}}, nil)
	persisted := persistedSoftware(map[string]state.SoftwareState{
		"teleport": {Name: "teleport", Version: "18.11.3", Installed: false},
	})

	agent := refreshNodeAgent(t, persisted, true)

	assert.Equal(t, "18.11.3", agent.Version)
}

func TestTeleportNodeAgentVersion_UnknownBinariesAreNotTheDefault(t *testing.T) {
	stubCatalog(t, []string{"18.6.4", "18.11.3"}, map[string][]string{"teleport": nil}, nil)

	agent := refreshNodeAgent(t, persistedSoftware(nil), true)

	assert.True(t, agent.Installed)
	assert.Empty(t, agent.Version)
}

func TestTeleportNodeAgentVersion_AbsentAgentHasNoVersion(t *testing.T) {
	calls := stubCatalog(t, []string{"18.6.4", "18.11.3"}, nil, nil)

	agent := refreshNodeAgent(t, withNodeAgentVersion(persistedSoftware(nil), "18.6.4"), false)

	assert.False(t, agent.Installed)
	assert.Empty(t, agent.Version)
	assert.Equal(t, 0, *calls)
}
