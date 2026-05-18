// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ── AvailablePresets ─────────────────────────────────────────────────────────

func TestAvailablePresets_ContainsExpectedPresets(t *testing.T) {
	presets := AvailablePresets()
	assert.Contains(t, presets, PresetTier1LFH)
	assert.Contains(t, presets, PresetTier1RFH)
	assert.Contains(t, presets, PresetCustom)
}

func TestAvailablePresets_ReturnsACopy(t *testing.T) {
	a := AvailablePresets()
	b := AvailablePresets()
	a[0] = "mutated"
	assert.NotEqual(t, "mutated", b[0], "AvailablePresets must return an independent copy")
}

// ── PluginListForPreset ──────────────────────────────────────────────────────

func TestPluginListForPreset_Tier1LFH(t *testing.T) {
	list := PluginListForPreset(PresetTier1LFH)
	assert.NotEmpty(t, list)
	assert.Contains(t, list, "facility-messaging")
	assert.Contains(t, list, "blocks-file-historic")
	assert.Contains(t, list, "blocks-file-recent")
	assert.NotContains(t, list, "s3-archive")
}

func TestPluginListForPreset_Tier1RFH(t *testing.T) {
	list := PluginListForPreset(PresetTier1RFH)
	assert.NotEmpty(t, list)
	assert.Contains(t, list, "facility-messaging")
	assert.Contains(t, list, "s3-archive")
	assert.NotContains(t, list, "blocks-file-historic")
}

func TestPluginListForPreset_UnknownReturnsEmpty(t *testing.T) {
	assert.Empty(t, PluginListForPreset("does-not-exist"))
}

func TestPluginListForPreset_CustomReturnsEmpty(t *testing.T) {
	assert.Empty(t, PluginListForPreset(PresetCustom), "custom preset has no predefined plugin list")
}

// ── PresetLabel ──────────────────────────────────────────────────────────────

func TestPresetLabel_KnownPresets(t *testing.T) {
	assert.NotEmpty(t, PresetLabel(PresetTier1LFH))
	assert.NotEmpty(t, PresetLabel(PresetTier1RFH))
	assert.NotEmpty(t, PresetLabel(PresetCustom))
}

func TestPresetLabel_UnknownReturnsFallback(t *testing.T) {
	assert.Equal(t, "unknown-preset", PresetLabel("unknown-preset"))
}

// ── IsKnownPreset ────────────────────────────────────────────────────────────

func TestIsKnownPreset_RecognisedPresets(t *testing.T) {
	assert.True(t, IsKnownPreset(PresetTier1LFH))
	assert.True(t, IsKnownPreset(PresetTier1RFH))
}

func TestIsKnownPreset_CustomIsFalse(t *testing.T) {
	assert.False(t, IsKnownPreset(PresetCustom), "custom has no predefined list so IsKnownPreset must be false")
}

func TestIsKnownPreset_UnknownIsFalse(t *testing.T) {
	assert.False(t, IsKnownPreset("not-a-preset"))
}

// ── injectPluginsConfig ──────────────────────────────────────────────────────

// injectPluginsConfig_ReturnsUnchangedWhenPluginListEmpty verifies that an empty
// PluginList leaves the values content untouched.
func TestInjectPluginsConfig_NoOpWhenPluginListEmpty(t *testing.T) {
	m := managerWithInputs(t, "", "")
	input := []byte("plugins:\n  names: original-list\n")
	result, err := m.injectPluginsConfig(input)
	require.NoError(t, err)
	assert.Equal(t, string(input), string(result))
}

func TestInjectPluginsConfig_SetsPluginsNames(t *testing.T) {
	m := managerWithInputs(t, PresetTier1LFH, PluginListForPreset(PresetTier1LFH))
	input := []byte("plugins:\n  mavenImage: some-image\n")

	result, err := m.injectPluginsConfig(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))

	plugins, ok := vals["plugins"].(map[string]interface{})
	require.True(t, ok, "plugins key must be a map")
	assert.Equal(t, PluginListForPreset(PresetTier1LFH), plugins["names"])
}

func TestInjectPluginsConfig_CreatesPluginsMapWhenAbsent(t *testing.T) {
	m := managerWithInputs(t, PresetTier1RFH, PluginListForPreset(PresetTier1RFH))
	input := []byte("blockNode:\n  config: {}\n")

	result, err := m.injectPluginsConfig(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))

	plugins, ok := vals["plugins"].(map[string]interface{})
	require.True(t, ok, "plugins map must be created when absent")
	assert.Equal(t, PluginListForPreset(PresetTier1RFH), plugins["names"])
}

func TestInjectPluginsConfig_OverridesExistingPluginsNames(t *testing.T) {
	newList := "facility-messaging,health"
	m := managerWithInputs(t, PresetCustom, newList)
	input := []byte("plugins:\n  names: old-list\n  mavenImage: img\n")

	result, err := m.injectPluginsConfig(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))

	plugins := vals["plugins"].(map[string]interface{})
	assert.Equal(t, newList, plugins["names"])
	assert.Equal(t, "img", plugins["mavenImage"], "existing sibling keys must be preserved")
}

func TestInjectPluginsConfig_CustomListNoWhitespace(t *testing.T) {
	list := "facility-messaging,health,server-status"
	m := managerWithInputs(t, PresetCustom, list)
	input := []byte("{}\n")

	result, err := m.injectPluginsConfig(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))

	names := vals["plugins"].(map[string]interface{})["names"].(string)
	for _, entry := range strings.Split(names, ",") {
		assert.Equal(t, strings.TrimSpace(entry), entry, "no surrounding whitespace per plugin entry")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// managerWithInputs builds a minimal Manager with PluginPreset and PluginList set.
func managerWithInputs(t *testing.T, preset, list string) *Manager {
	t.Helper()
	m := &Manager{}
	m.blockNodeInputs.PluginPreset = preset
	m.blockNodeInputs.PluginList = list
	return m
}
