// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// withHostConfig sets the global host config for a test and restores an empty one
// afterwards, mirroring the pattern in reconfigure_handler_test.go.
func withHostConfig(t *testing.T, hc models.HostConfig) {
	t.Helper()
	config.OverrideHostConfig(hc)
	t.Cleanup(func() { config.OverrideHostConfig(models.HostConfig{}) })
}

func deployedState() *state.State {
	st := &state.State{}
	st.BlockNodeState.ReleaseInfo.Status = release.StatusDeployed
	return st
}

// TestPatch_PersistsFirewallAndShapingWhenEnabled covers AC1: an enabled firewall
// with a real allowlist, and enabled traffic shaping, are recorded into state.
func TestPatch_PersistsFirewallAndShapingWhenEnabled(t *testing.T) {
	withHostConfig(t, models.HostConfig{
		ManagementCIDRs: []string{"10.0.0.0/8"},
		BlockedCIDRs:    []string{"192.0.2.0/24"},
		MgmtPorts:       []int{2222},
		PodCIDR:         "10.4.0.0/14",
		InClusterPorts:  []int{8080},
		Disabled:        false,
	})

	st := deployedState()
	patch := patchBlockNodeStateWithTrafficShaping()
	inputs := models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			TrafficShapingEnabled: true,
			EgressInterface:       "eth0",
			LinkRate:              "1gbit",
		},
	}
	require.NoError(t, patch(st, inputs))

	fw := st.MachineState.Firewall
	require.NotNil(t, fw, "firewall must be persisted when enabled with an allowlist")
	assert.False(t, fw.Disabled)
	assert.Equal(t, []string{"10.0.0.0/8"}, fw.ManagementCIDRs)
	assert.Equal(t, []int{2222}, fw.MgmtPorts)

	require.NotNil(t, st.BlockNodeState.Shaping, "shaping content must be persisted when tc enabled")
	assert.Equal(t, "eth0", st.BlockNodeState.Shaping.EgressInterface)
	assert.Equal(t, "1gbit", st.BlockNodeState.Shaping.LinkRate)

	assert.False(t, st.BlockNodeState.TrafficShapingDisabled)
}

// TestPatch_DisablePreservesAllowlist covers AC2: disabling the firewall flips the
// decision but must NOT wipe the stored allowlist, so a later bare re-enable can
// restore it.
func TestPatch_DisablePreservesAllowlist(t *testing.T) {
	withHostConfig(t, models.HostConfig{Disabled: true})

	st := deployedState()
	st.MachineState.Firewall = &state.HostFirewallState{
		ManagementCIDRs: []string{"10.0.0.0/8"},
		MgmtPorts:       []int{2222},
	}

	patch := patchBlockNodeStateWithTrafficShaping()
	require.NoError(t, patch(st, models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{TrafficShapingEnabled: false},
	}))

	fw := st.MachineState.Firewall
	require.NotNil(t, fw)
	assert.True(t, fw.Disabled, "decision must record disabled")
	assert.Equal(t, []string{"10.0.0.0/8"}, fw.ManagementCIDRs, "allowlist must be preserved on disable")
	assert.Equal(t, []int{2222}, fw.MgmtPorts, "port must be preserved on disable")
}

// TestPatch_EnableWithEmptyAllowlistPreservesContent covers the skip-on-empty
// rule: enabling with no allowlist (the step then skips) must not clobber a
// previously recorded allowlist.
func TestPatch_EnableWithEmptyAllowlistPreservesContent(t *testing.T) {
	withHostConfig(t, models.HostConfig{Disabled: false}) // enabled, but no CIDRs

	st := deployedState()
	st.MachineState.Firewall = &state.HostFirewallState{ManagementCIDRs: []string{"10.0.0.0/8"}}

	patch := patchBlockNodeStateWithTrafficShaping()
	require.NoError(t, patch(st, models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{TrafficShapingEnabled: false},
	}))

	fw := st.MachineState.Firewall
	require.NotNil(t, fw)
	assert.False(t, fw.Disabled)
	assert.Equal(t, []string{"10.0.0.0/8"}, fw.ManagementCIDRs, "allowlist must survive an empty-allowlist enable")
}

// TestPatch_BareReconfigureKeepsRecordedShapeOverrides covers the state-record
// half of #1037: ShapeOverrides is no longer re-asserted as an effective input, so
// a bare reconfigure patches with an empty map. The previously recorded request
// must survive that — nothing else can recover it (the reality refresh preserves
// this record for the same reason).
func TestPatch_BareReconfigureKeepsRecordedShapeOverrides(t *testing.T) {
	withHostConfig(t, models.HostConfig{Disabled: true})

	prio := 0
	st := deployedState()
	st.BlockNodeState.Shaping = &state.ShapingState{
		EgressInterface: "eth0",
		LinkRate:        "1gbit",
		ShapeOverrides: map[string]models.ShapeOverride{
			"partner": {Rate: "400mbit", Ceil: "700mbit", Prio: &prio},
		},
	}

	patch := patchBlockNodeStateWithTrafficShaping()
	inputs := models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			TrafficShapingEnabled: true,
			EgressInterface:       "eth0",
			LinkRate:              "1gbit",
			// No --shape on this run.
		},
	}
	require.NoError(t, patch(st, inputs))

	require.NotNil(t, st.BlockNodeState.Shaping)
	require.Contains(t, st.BlockNodeState.Shaping.ShapeOverrides, "partner",
		"a bare reconfigure must not erase the recorded --shape request")
	assert.Equal(t, "400mbit", st.BlockNodeState.Shaping.ShapeOverrides["partner"].Rate)
}

// TestPatch_ExplicitShapeOverridesReplaceRecord is the counterpart: a run that did
// supply --shape replaces the record rather than merging into it, so the state
// always reflects the most recent request.
func TestPatch_ExplicitShapeOverridesReplaceRecord(t *testing.T) {
	withHostConfig(t, models.HostConfig{Disabled: true})

	st := deployedState()
	st.BlockNodeState.Shaping = &state.ShapingState{
		EgressInterface: "eth0",
		LinkRate:        "1gbit",
		ShapeOverrides: map[string]models.ShapeOverride{
			"partner": {Rate: "400mbit"},
		},
	}

	patch := patchBlockNodeStateWithTrafficShaping()
	inputs := models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			TrafficShapingEnabled: true,
			EgressInterface:       "eth0",
			LinkRate:              "1gbit",
			ShapeOverrides: map[string]models.ShapeOverride{
				"public": {Rate: "200mbit"},
			},
		},
	}
	require.NoError(t, patch(st, inputs))

	require.NotNil(t, st.BlockNodeState.Shaping)
	assert.NotContains(t, st.BlockNodeState.Shaping.ShapeOverrides, "partner",
		"an explicit --shape run records only what it asked for")
	assert.Equal(t, "200mbit", st.BlockNodeState.Shaping.ShapeOverrides["public"].Rate)
}
