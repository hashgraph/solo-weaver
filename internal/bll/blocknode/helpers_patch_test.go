// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"testing"

	bnpkg "github.com/hashgraph/solo-weaver/internal/blocknode"
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

// TestPatch_ClearsStaleCustomPluginListOnSwitch covers the custom→preset switch: when
// the operator moves from a custom plugin list to a chart-backed preset (or "none"), the
// stale custom list must be cleared from state so it never sits beside the new preset.
func TestPatch_ClearsStaleCustomPluginListOnSwitch(t *testing.T) {
	patch := patchBlockNodeState()

	for _, preset := range []string{bnpkg.PresetTier1LFH, bnpkg.PresetTier1RFH, bnpkg.PresetNone} {
		t.Run(preset, func(t *testing.T) {
			st := deployedState()
			st.BlockNodeState.PluginPreset = bnpkg.PresetCustom
			st.BlockNodeState.PluginList = "facility-messaging,health"

			inputs := models.UserInputs[models.BlockNodeInputs]{
				Custom: models.BlockNodeInputs{PluginPreset: preset},
			}
			require.NoError(t, patch(st, inputs))

			assert.Equal(t, preset, st.BlockNodeState.PluginPreset)
			assert.Empty(t, st.BlockNodeState.PluginList, "stale custom list must be cleared on switch")
		})
	}
}

// TestPatch_PreservesStoredPluginsWhenPresetUnset ensures a reconfigure that does not touch
// plugins (empty preset and list) leaves the stored custom selection intact.
func TestPatch_PreservesStoredPluginsWhenPresetUnset(t *testing.T) {
	st := deployedState()
	st.BlockNodeState.PluginPreset = bnpkg.PresetCustom
	st.BlockNodeState.PluginList = "facility-messaging,health"

	require.NoError(t, patchBlockNodeState()(st, models.UserInputs[models.BlockNodeInputs]{}))

	assert.Equal(t, bnpkg.PresetCustom, st.BlockNodeState.PluginPreset)
	assert.Equal(t, "facility-messaging,health", st.BlockNodeState.PluginList)
}

// TestPatch_PersistsCustomPluginList ensures an explicit custom list is stored.
func TestPatch_PersistsCustomPluginList(t *testing.T) {
	st := deployedState()
	inputs := models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{PluginPreset: bnpkg.PresetCustom, PluginList: "a,b,c"},
	}
	require.NoError(t, patchBlockNodeState()(st, inputs))

	assert.Equal(t, bnpkg.PresetCustom, st.BlockNodeState.PluginPreset)
	assert.Equal(t, "a,b,c", st.BlockNodeState.PluginList)
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
	require.NoError(t, patch(st, inputs, nil))

	fw := st.MachineState.Firewall
	require.NotNil(t, fw, "firewall must be persisted when enabled with an allowlist")
	assert.False(t, fw.Disabled)
	assert.Equal(t, []string{"10.0.0.0/8"}, fw.ManagementCIDRs)
	assert.Equal(t, []int{2222}, fw.MgmtPorts)

	require.NotNil(t, st.BlockNodeState.Shaping, "shaping content must be persisted when tc enabled")
	assert.Equal(t, "eth0", st.BlockNodeState.Shaping.EgressInterface)
	assert.Equal(t, "1gbit", st.BlockNodeState.Shaping.LinkRate)

	assert.True(t, st.BlockNodeState.TrafficShapingEnabled)
}

// TestPatch_TrafficShapingDisableRecordsOff checks that turning shaping off
// overwrites a recorded true.
func TestPatch_TrafficShapingDisableRecordsOff(t *testing.T) {
	withHostConfig(t, models.HostConfig{Disabled: true})

	st := deployedState()
	st.BlockNodeState.TrafficShapingEnabled = true

	patch := patchBlockNodeStateWithTrafficShaping()
	require.NoError(t, patch(st, models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{TrafficShapingEnabled: false},
	}, nil))

	assert.False(t, st.BlockNodeState.TrafficShapingEnabled)
}

// TestPatch_ExplicitDecisionClearsCleanupMark: install and reconfigure record a
// decision, so the upgrade-time cleanup no longer applies.
func TestPatch_ExplicitDecisionClearsCleanupMark(t *testing.T) {
	withHostConfig(t, models.HostConfig{Disabled: true})

	for _, enabled := range []bool{true, false} {
		st := deployedState()
		st.BlockNodeState.TrafficShapingCleanupPending = true

		patch := patchBlockNodeStateWithTrafficShaping()
		require.NoError(t, patch(st, models.UserInputs[models.BlockNodeInputs]{
			Custom: models.BlockNodeInputs{TrafficShapingEnabled: enabled},
		}, nil))

		assert.False(t, st.BlockNodeState.TrafficShapingCleanupPending, "enabled=%v", enabled)
	}
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
	}, nil))

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
	}, nil))

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
	require.NoError(t, patch(st, inputs, nil))

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
	require.NoError(t, patch(st, inputs, nil))

	require.NotNil(t, st.BlockNodeState.Shaping)
	assert.NotContains(t, st.BlockNodeState.Shaping.ShapeOverrides, "partner",
		"an explicit --shape run records only what it asked for")
	assert.Equal(t, "200mbit", st.BlockNodeState.Shaping.ShapeOverrides["public"].Rate)
}
