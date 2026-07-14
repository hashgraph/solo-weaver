// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/semver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ── blockNodePluginHistory registry invariants ────────────────────────────────

func TestPluginHistory_AscendingVersionOrder(t *testing.T) {
	for i := 1; i < len(blockNodePluginHistory); i++ {
		prev := blockNodePluginHistory[i-1]
		curr := blockNodePluginHistory[i]
		require.NotEmpty(t, curr.MinVersion, "entry %d must have a MinVersion (only the first entry may be empty)", i)
		// Always validate that curr.MinVersion is parseable, even when prev is the
		// baseline (empty MinVersion). A typo here would cause configForVersion to
		// silently skip the entry rather than returning an error.
		currVer, err := semver.NewSemver(curr.MinVersion)
		require.NoError(t, err, "entry %d MinVersion %q must be valid semver", i, curr.MinVersion)
		if prev.MinVersion == "" {
			continue
		}
		prevVer, err := semver.NewSemver(prev.MinVersion)
		require.NoError(t, err, "entry %d MinVersion %q must be valid semver", i-1, prev.MinVersion)
		assert.True(t, prevVer.LessThan(currVer), "entry %d (%s) must be > entry %d (%s)", i, curr.MinVersion, i-1, prev.MinVersion)
	}
}

func TestPluginHistory_AllKnownPresetsInEveryEntry(t *testing.T) {
	knownPresets := []string{PresetTier1LFH, PresetTier1RFH}
	for i, cfg := range blockNodePluginHistory {
		for _, preset := range knownPresets {
			assert.NotEmpty(t, cfg.Presets[preset], "entry %d (MinVersion=%q) missing preset %q", i, cfg.MinVersion, preset)
		}
	}
}

func TestPluginHistory_AllPluginsNonEmpty(t *testing.T) {
	for i, cfg := range blockNodePluginHistory {
		assert.NotEmpty(t, cfg.AllPlugins, "entry %d (MinVersion=%q) must have a non-empty AllPlugins list", i, cfg.MinVersion)
	}
}

// ── AvailablePresets ─────────────────────────────────────────────────────────

func TestAvailablePresets_ContainsExpectedPresets(t *testing.T) {
	presets := AvailablePresets()
	assert.Contains(t, presets, PresetTier1LFH)
	assert.Contains(t, presets, PresetTier1RFH)
	assert.Contains(t, presets, PresetCustom)
	assert.Contains(t, presets, PresetNone)
}

// TestPresetNone_ResolvesToEmptyList locks in the contract that the "no override"
// preset is not a known preset and yields an empty plugin list, so injectPluginsConfig
// leaves the merged --values/chart-default plugins.names untouched.
func TestPresetNone_ResolvesToEmptyList(t *testing.T) {
	assert.False(t, IsKnownPreset(PresetNone), "PresetNone must not be a known preset")
	assert.Empty(t, PluginListForPreset(PresetNone, ""), "PresetNone must resolve to an empty plugin list")
	assert.Empty(t, PluginListForPreset(PresetNone, "0.35.0"), "PresetNone must resolve empty at any version")
	assert.NotEmpty(t, PresetLabel(PresetNone), "PresetNone must have a display label")
	// It must be absent from every version entry's Presets map.
	for i, cfg := range blockNodePluginHistory {
		_, ok := cfg.Presets[PresetNone]
		assert.False(t, ok, "entry %d (MinVersion=%q) must not define PresetNone", i, cfg.MinVersion)
	}
}

func TestAvailablePresets_ReturnsACopy(t *testing.T) {
	a := AvailablePresets()
	b := AvailablePresets()
	a[0] = "mutated"
	assert.NotEqual(t, "mutated", b[0], "AvailablePresets must return an independent copy")
}

// ── PluginListForPreset ──────────────────────────────────────────────────────

func TestPluginListForPreset_Tier1LFH(t *testing.T) {
	list := PluginListForPreset(PresetTier1LFH, "")
	assert.NotEmpty(t, list)
	assert.Contains(t, list, "facility-messaging")
	assert.Contains(t, list, "blocks-file-historic")
	assert.Contains(t, list, "blocks-file-recent")
	assert.NotContains(t, list, "s3-archive")
	assert.NotContains(t, list, "cloud-storage-archive")
	assert.NotContains(t, list, "cloud-storage-expanded")
}

func TestPluginListForPreset_Tier1RFH_BN035Plus(t *testing.T) {
	for _, ver := range []string{"", "0.35.0", "0.36.0", "1.0.0"} {
		list := PluginListForPreset(PresetTier1RFH, ver)
		assert.NotEmpty(t, list, "version=%q", ver)
		assert.Contains(t, list, "cloud-storage-archive", "version=%q", ver)
		assert.Contains(t, list, "cloud-storage-expanded", "version=%q", ver)
		assert.NotContains(t, list, "s3-archive", "version=%q", ver)
		assert.NotContains(t, list, "blocks-file-historic", "version=%q", ver)
	}
}

func TestPluginListForPreset_Tier1RFH_LegacyBN(t *testing.T) {
	for _, ver := range []string{"0.34.9", "0.30.0", "0.26.2"} {
		list := PluginListForPreset(PresetTier1RFH, ver)
		assert.NotEmpty(t, list, "version=%q", ver)
		assert.Contains(t, list, "s3-archive", "version=%q", ver)
		assert.NotContains(t, list, "cloud-storage-archive", "version=%q", ver)
		assert.NotContains(t, list, "cloud-storage-expanded", "version=%q", ver)
	}
}

// ── BN 0.37.1 roster-bootstrap boundary ──────────────────────────────────────

// TestPluginListForPreset_BN0371_ExactLists pins the 0.37.1 preset strings to the
// verbatim upstream values-overrides/plugin-profile-{lfh,rfh}.yaml so the injected
// plugins.names is byte-identical to the chart default. "" resolves to the latest
// (0.37.1) entry.
func TestPluginListForPreset_BN0371_ExactLists(t *testing.T) {
	const wantLFH = "backfill,block-access-service,blocks-file-historic,blocks-file-recent,facility-messaging,health,roster-bootstrap-rsa,roster-bootstrap-tss,server-status,stream-publisher,stream-subscriber,verification"
	const wantRFH = "backfill,cloud-storage-archive,cloud-storage-expanded,facility-messaging,health,roster-bootstrap-rsa,roster-bootstrap-tss,server-status,verification"
	for _, ver := range []string{"", "0.37.1", "0.38.0", "1.0.0"} {
		assert.Equal(t, wantLFH, PluginListForPreset(PresetTier1LFH, ver), "LFH version=%q", ver)
		assert.Equal(t, wantRFH, PluginListForPreset(PresetTier1RFH, ver), "RFH version=%q", ver)
	}
}

// TestPluginListForPreset_Pre0371_NoRosterBootstrap verifies that chart versions
// below 0.37.1 keep resolving to the 0.35.0 bracket: no roster-bootstrap plugins,
// and the RFH preset still carries the plugins that 0.37.1 trims.
func TestPluginListForPreset_Pre0371_NoRosterBootstrap(t *testing.T) {
	for _, ver := range []string{"0.35.0", "0.36.0", "0.37.0"} {
		lfh := PluginListForPreset(PresetTier1LFH, ver)
		rfh := PluginListForPreset(PresetTier1RFH, ver)
		assert.NotContains(t, lfh, "roster-bootstrap", "LFH version=%q", ver)
		assert.NotContains(t, rfh, "roster-bootstrap", "RFH version=%q", ver)
		for _, kept := range []string{"block-access-service", "stream-publisher", "stream-subscriber", "blocks-file-recent"} {
			assert.Contains(t, rfh, kept, "RFH version=%q must still carry %q below 0.37.1", ver, kept)
		}
	}
}

// TestPluginsForVersion_BN0371Boundary verifies the TUI custom multi-select menu
// gains roster-bootstrap only at 0.37.1+.
func TestPluginsForVersion_BN0371Boundary(t *testing.T) {
	for _, ver := range []string{"", "0.37.1", "0.38.0"} {
		plugins := PluginsForVersion(ver)
		assert.Contains(t, plugins, "roster-bootstrap-rsa", "version=%q", ver)
		assert.Contains(t, plugins, "roster-bootstrap-tss", "version=%q", ver)
	}
	for _, ver := range []string{"0.35.0", "0.37.0"} {
		plugins := PluginsForVersion(ver)
		assert.NotContains(t, plugins, "roster-bootstrap-rsa", "version=%q", ver)
		assert.NotContains(t, plugins, "roster-bootstrap-tss", "version=%q", ver)
	}
}

// TestPluginListForPreset_UpgradeScenario documents the expected behaviour when an
// operator upgrades from a pre-0.35 chart to 0.35+. solo-weaver reads the stored
// PluginPreset ("tier1-rfh") and calls PluginListForPreset with the new target
// chart version; the returned list must contain the renamed cloud-storage plugins
// even though the previous install used s3-archive.
func TestPluginListForPreset_UpgradeScenario(t *testing.T) {
	cases := []struct {
		name            string
		installedPreset string
		targetVersion   string
		wantContains    []string
		wantAbsent      []string
	}{
		{
			name:            "RFH upgrade pre-0.35 to 0.35 switches plugin names",
			installedPreset: PresetTier1RFH,
			targetVersion:   "0.35.0",
			wantContains:    []string{"cloud-storage-archive", "cloud-storage-expanded"},
			wantAbsent:      []string{"s3-archive"},
		},
		{
			name:            "RFH upgrade stays on pre-0.35 keeps legacy name",
			installedPreset: PresetTier1RFH,
			targetVersion:   "0.34.9",
			wantContains:    []string{"s3-archive"},
			wantAbsent:      []string{"cloud-storage-archive", "cloud-storage-expanded"},
		},
		{
			name:            "LFH upgrade is unaffected by the 0.35 boundary",
			installedPreset: PresetTier1LFH,
			targetVersion:   "0.35.0",
			wantContains:    []string{"blocks-file-historic", "blocks-file-recent"},
			wantAbsent:      []string{"s3-archive", "cloud-storage-archive", "cloud-storage-expanded"},
		},
		{
			name:            "RFH upgrade to 0.37.1 adds roster-bootstrap and drops trimmed plugins",
			installedPreset: PresetTier1RFH,
			targetVersion:   "0.37.1",
			wantContains:    []string{"roster-bootstrap-rsa", "roster-bootstrap-tss", "cloud-storage-archive", "cloud-storage-expanded"},
			wantAbsent:      []string{"s3-archive", "block-access-service", "stream-publisher", "stream-subscriber", "blocks-file-recent"},
		},
		{
			name:            "RFH upgrade 0.37.0 to 0.37.1 crosses the roster-bootstrap boundary",
			installedPreset: PresetTier1RFH,
			targetVersion:   "0.37.0",
			wantContains:    []string{"cloud-storage-archive", "cloud-storage-expanded", "blocks-file-recent", "block-access-service"},
			wantAbsent:      []string{"roster-bootstrap-rsa", "roster-bootstrap-tss"},
		},
		{
			name:            "LFH upgrade to 0.37.1 adds roster-bootstrap and keeps file plugins",
			installedPreset: PresetTier1LFH,
			targetVersion:   "0.37.1",
			wantContains:    []string{"roster-bootstrap-rsa", "roster-bootstrap-tss", "blocks-file-historic", "blocks-file-recent"},
			wantAbsent:      []string{"s3-archive", "cloud-storage-archive", "cloud-storage-expanded"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list := PluginListForPreset(tc.installedPreset, tc.targetVersion)
			require.NotEmpty(t, list)
			for _, want := range tc.wantContains {
				assert.Contains(t, list, want)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, list, absent)
			}
		})
	}
}

func TestPluginListForPreset_InvalidVersionFallsToLatest(t *testing.T) {
	list := PluginListForPreset(PresetTier1RFH, "not-a-semver")
	assert.Contains(t, list, "cloud-storage-archive", "invalid semver must fall back to the latest config")
}

func TestPluginListForPreset_UnknownReturnsEmpty(t *testing.T) {
	assert.Empty(t, PluginListForPreset("does-not-exist", ""))
}

func TestPluginListForPreset_CustomReturnsEmpty(t *testing.T) {
	assert.Empty(t, PluginListForPreset(PresetCustom, ""), "custom preset has no predefined plugin list")
}

// ── PluginsForVersion ────────────────────────────────────────────────────────

func TestPluginsForVersion_BN035Plus(t *testing.T) {
	for _, ver := range []string{"", "0.35.0", "0.36.0"} {
		plugins := PluginsForVersion(ver)
		assert.Contains(t, plugins, "cloud-storage-archive", "version=%q", ver)
		assert.Contains(t, plugins, "cloud-storage-expanded", "version=%q", ver)
		assert.NotContains(t, plugins, "s3-archive", "version=%q", ver)
	}
}

func TestPluginsForVersion_LegacyBN(t *testing.T) {
	for _, ver := range []string{"0.34.9", "0.30.0"} {
		plugins := PluginsForVersion(ver)
		assert.Contains(t, plugins, "s3-archive", "version=%q", ver)
		assert.NotContains(t, plugins, "cloud-storage-archive", "version=%q", ver)
		assert.NotContains(t, plugins, "cloud-storage-expanded", "version=%q", ver)
	}
}

func TestPluginsForVersion_ReturnsACopy(t *testing.T) {
	a := PluginsForVersion("")
	b := PluginsForVersion("")
	a[0] = "mutated"
	assert.NotEqual(t, "mutated", b[0], "PluginsForVersion must return an independent copy")
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
	m := managerWithInputs(t, PresetTier1LFH, PluginListForPreset(PresetTier1LFH, ""))
	input := []byte("plugins:\n  mavenImage: some-image\n")

	result, err := m.injectPluginsConfig(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))

	plugins, ok := vals["plugins"].(map[string]interface{})
	require.True(t, ok, "plugins key must be a map")
	assert.Equal(t, PluginListForPreset(PresetTier1LFH, ""), plugins["names"])
}

func TestInjectPluginsConfig_CreatesPluginsMapWhenAbsent(t *testing.T) {
	m := managerWithInputs(t, PresetTier1RFH, PluginListForPreset(PresetTier1RFH, ""))
	input := []byte("blockNode:\n  config: {}\n")

	result, err := m.injectPluginsConfig(input)
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(result, &vals))

	plugins, ok := vals["plugins"].(map[string]interface{})
	require.True(t, ok, "plugins map must be created when absent")
	assert.Equal(t, PluginListForPreset(PresetTier1RFH, ""), plugins["names"])
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

// ── ValuesFileDefinesPlugins ───────────────────────────────────────────────────

func TestValuesFileDefinesPlugins(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"scalar string", "plugins:\n  names: \"health,verification\"\n", true},
		{"yaml sequence", "plugins:\n  names:\n    - health\n    - verification\n", true},
		// An explicit empty string, empty sequence, or null still counts as "defined":
		// the operator touched the key, and Helm's CoalesceTables treats an operator
		// null as an instruction to delete the chart's default plugins.names entirely
		// (see ValuesFileDefinesPlugins doc comment) — that's deliberate operator intent,
		// not "no opinion", so it must not be clobbered by a preset-derived list.
		{"empty string", "plugins:\n  names: \"\"\n", true},
		{"empty sequence", "plugins:\n  names: []\n", true},
		{"explicit null", "plugins:\n  names: null\n", true},
		{"names absent", "plugins:\n  mavenImage: img\n", false},
		{"plugins absent", "blockNode:\n  config: {}\n", false},
		{"unparseable yaml", "plugins: : :\n  - [\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "values.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o600))
			assert.Equal(t, tc.want, ValuesFileDefinesPlugins(path))
		})
	}
}

func TestValuesFileDefinesPlugins_EmptyPathIsFalse(t *testing.T) {
	assert.False(t, ValuesFileDefinesPlugins(""))
}

func TestValuesFileDefinesPlugins_MissingFileIsFalse(t *testing.T) {
	assert.False(t, ValuesFileDefinesPlugins(filepath.Join(t.TempDir(), "does-not-exist.yaml")))
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
