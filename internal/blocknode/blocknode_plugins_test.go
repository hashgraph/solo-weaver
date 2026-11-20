// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
)

const (
	lfhProfileBody = "plugins:\n  names: \"health,facility-app-state,stream-publisher\"\n"
	rfhProfileBody = "plugins:\n  names: \"health,facility-app-state,cloud-storage-archive\"\n"
)

// writeFixtureChart packages a minimal block-node chart .tgz carrying the given
// files (keyed by their in-chart path, e.g. "values-overrides/plugin-profile-lfh.yaml")
// and returns the archive path, mirroring what PullChart would hand back.
func writeFixtureChart(t *testing.T, version string, files map[string]string) string {
	t.Helper()
	ch := &chart.Chart{
		Metadata: &chart.Metadata{APIVersion: "v2", Name: "block-node-server", Version: version},
	}
	for name, body := range files {
		ch.Files = append(ch.Files, &chart.File{Name: name, Data: []byte(body)})
	}
	p, err := chartutil.Save(ch, t.TempDir())
	require.NoError(t, err)
	return p
}

// ── Preset metadata ────────────────────────────────────────────────────────────

func TestAvailablePresets(t *testing.T) {
	got := AvailablePresets()
	assert.Equal(t, []string{PresetLFH, PresetRFH, PresetCustom, PresetNone}, got)
}

func TestCanonicalPreset(t *testing.T) {
	assert.Equal(t, PresetLFH, CanonicalPreset(PresetTier1LFH), "tier1-lfh alias -> lfh")
	assert.Equal(t, PresetRFH, CanonicalPreset(PresetTier1RFH), "tier1-rfh alias -> rfh")
	// Canonical and non-alias values pass through unchanged.
	for _, p := range []string{PresetLFH, PresetRFH, PresetCustom, PresetNone, "", "bogus"} {
		assert.Equal(t, p, CanonicalPreset(p))
	}
}

func TestNormalizePluginList(t *testing.T) {
	cases := map[string]string{
		"a,b,c":         "a,b,c",
		"a b c":         "a,b,c",
		"a, b ,c":       "a,b,c",
		" a  b , c ":    "a,b,c",
		"a,,b":          "a,b",
		"":              "",
		"   ":           "",
		"solo-a solo-b": "solo-a,solo-b",
	}
	for in, want := range cases {
		assert.Equalf(t, want, NormalizePluginList(in), "NormalizePluginList(%q)", in)
	}
}

func TestIsChartBackedPreset(t *testing.T) {
	assert.True(t, IsChartBackedPreset(PresetLFH))
	assert.True(t, IsChartBackedPreset(PresetRFH))
	assert.True(t, IsChartBackedPreset(PresetTier1LFH), "legacy alias is chart-backed")
	assert.True(t, IsChartBackedPreset(PresetTier1RFH), "legacy alias is chart-backed")
	// Any profile name is chart-backed; the chart validates existence at resolve time.
	assert.True(t, IsChartBackedPreset("archive"), "unknown-to-this-binary profiles are still chart-backed")
	assert.False(t, IsChartBackedPreset(PresetCustom), "custom is not chart-resolved")
	assert.False(t, IsChartBackedPreset(PresetNone), "none is not chart-resolved")
	assert.False(t, IsChartBackedPreset(""), "empty is not chart-resolved")
}

func TestChartProfileFileName(t *testing.T) {
	assert.Equal(t, "plugin-profile-lfh.yaml", chartProfileFileName(PresetLFH))
	assert.Equal(t, "plugin-profile-rfh.yaml", chartProfileFileName(PresetRFH))
	assert.Equal(t, "plugin-profile-archive.yaml", chartProfileFileName("archive"))
}

func TestPresetLabel(t *testing.T) {
	assert.Contains(t, PresetLabel(PresetLFH), "Local Full History")
	assert.Contains(t, PresetLabel(PresetTier1LFH), "Local Full History", "alias resolves to the canonical label")
	assert.Equal(t, "unknown-preset", PresetLabel("unknown-preset"), "unknown IDs echo back")
}

// ── Profile YAML parsing ─────────────────────────────────────────────────────────

func TestPluginNamesFromProfileYAML(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		names, err := pluginNamesFromProfileYAML([]byte(lfhProfileBody))
		require.NoError(t, err)
		assert.Equal(t, "health,facility-app-state,stream-publisher", names)
	})
	t.Run("trims surrounding whitespace", func(t *testing.T) {
		names, err := pluginNamesFromProfileYAML([]byte("plugins:\n  names: \"  a,b  \"\n"))
		require.NoError(t, err)
		assert.Equal(t, "a,b", names)
	})
	t.Run("missing names returns empty", func(t *testing.T) {
		names, err := pluginNamesFromProfileYAML([]byte("plugins:\n  mavenImage: x\n"))
		require.NoError(t, err)
		assert.Equal(t, "", names)
	})
	t.Run("malformed yaml errors", func(t *testing.T) {
		_, err := pluginNamesFromProfileYAML([]byte("plugins: : :\n"))
		require.Error(t, err)
	})
}

// ── resolvePluginList precedence ─────────────────────────────────────────────────

func TestResolvePluginList_ExplicitListKept(t *testing.T) {
	// A custom list triggers a best-effort pull to warn about unknown names, but the
	// explicit list itself must be left intact.
	tgz := writeFixtureChart(t, "0.46.0", map[string]string{
		"values-overrides/plugin-profile-all.yaml": "plugins:\n  names: \"x,y\"\n",
	})
	m, hm := managerWithHelmMock(t, models.BlockNodeInputs{
		Chart:        "oci://example/block-node-server",
		ChartVersion: "0.46.0",
		PluginPreset: PresetCustom,
		PluginList:   "x,y,z",
	})
	hm.EXPECT().PullChart(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(tgz, nil)
	require.NoError(t, m.resolvePluginList(context.Background(), ""))
	assert.Equal(t, "x,y,z", m.blockNodeInputs.PluginList, "explicit --plugins must be left intact")
}

func TestResolvePluginList_ValuesFileOverrideSkipsPull(t *testing.T) {
	valuesFile := writeValuesFile(t, "plugins:\n  names: \"from-values\"\n")
	m, _ := managerWithHelmMock(t, models.BlockNodeInputs{PluginPreset: PresetTier1LFH})
	require.NoError(t, m.resolvePluginList(context.Background(), valuesFile))
	assert.Equal(t, "", m.blockNodeInputs.PluginList,
		"a --values plugins.names override wins over a preset; weaver injects nothing")
}

func TestResolvePluginList_NoneReadsChartDefault(t *testing.T) {
	// none / unset injects the chart's own values.yaml default so an upgrade stays
	// version-correct instead of Helm --reuse-values keeping a stale deployed list.
	ch := &chart.Chart{
		Metadata: &chart.Metadata{APIVersion: "v2", Name: "block-node-server", Version: "0.46.0"},
		// Save writes values.yaml from Raw (not from Values), so seed the default there.
		Raw: []*chart.File{{Name: "values.yaml", Data: []byte("plugins:\n  names: \"health,facility-app-state\"\n")}},
	}
	tgz, err := chartutil.Save(ch, t.TempDir())
	require.NoError(t, err)

	for _, preset := range []string{PresetNone, ""} {
		name := preset
		if name == "" {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			m, hm := managerWithHelmMock(t, models.BlockNodeInputs{
				Chart:        "oci://example/block-node-server",
				ChartVersion: "0.46.0",
				PluginPreset: preset,
			})
			hm.EXPECT().PullChart(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(tgz, nil)
			require.NoError(t, m.resolvePluginList(context.Background(), ""))
			assert.Equal(t, "health,facility-app-state", m.blockNodeInputs.PluginList)
		})
	}
}

// TestResolvePluginList_NoneFallsBackWhenDefaultUnreadable verifies none degrades to
// leaving the list unset (Helm/chart default) when the chart pull fails.
func TestResolvePluginList_NoneFallsBackWhenDefaultUnreadable(t *testing.T) {
	m, hm := managerWithHelmMock(t, models.BlockNodeInputs{
		Chart:        "oci://example/block-node-server",
		ChartVersion: "0.46.0",
		PluginPreset: PresetNone,
	})
	hm.EXPECT().PullChart(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return("", errorx.ExternalError.New("registry down"))
	require.NoError(t, m.resolvePluginList(context.Background(), ""), "none must not hard-fail on a pull error")
	assert.Equal(t, "", m.blockNodeInputs.PluginList)
}

func TestResolvePluginList_PresetReadsChartProfile(t *testing.T) {
	cases := []struct {
		preset   string
		profile  string
		body     string
		wantList string
	}{
		{PresetLFH, "values-overrides/plugin-profile-lfh.yaml", lfhProfileBody, "health,facility-app-state,stream-publisher"},
		{PresetTier1LFH, "values-overrides/plugin-profile-lfh.yaml", lfhProfileBody, "health,facility-app-state,stream-publisher"},
		{PresetRFH, "values-overrides/plugin-profile-rfh.yaml", rfhProfileBody, "health,facility-app-state,cloud-storage-archive"},
		{PresetTier1RFH, "values-overrides/plugin-profile-rfh.yaml", rfhProfileBody, "health,facility-app-state,cloud-storage-archive"},
		// Zero-touch: a profile name this binary has no const for still resolves,
		// because the filename is derived and validated against the chart.
		{"archive", "values-overrides/plugin-profile-archive.yaml", "plugins:\n  names: \"backfill,cloud-storage-archive\"\n", "backfill,cloud-storage-archive"},
	}
	for _, tc := range cases {
		t.Run(tc.preset, func(t *testing.T) {
			tgz := writeFixtureChart(t, "0.46.0", map[string]string{tc.profile: tc.body})
			m, hm := managerWithHelmMock(t, models.BlockNodeInputs{
				Chart:        "oci://example/block-node-server",
				ChartVersion: "0.46.0",
				PluginPreset: tc.preset,
			})
			hm.EXPECT().
				PullChart(gomock.Any(), gomock.Any(), "oci://example/block-node-server", "0.46.0").
				Return(tgz, nil)

			require.NoError(t, m.resolvePluginList(context.Background(), ""))
			assert.Equal(t, tc.wantList, m.blockNodeInputs.PluginList)
		})
	}
}

func TestResolvePluginList_PullError(t *testing.T) {
	m, hm := managerWithHelmMock(t, models.BlockNodeInputs{
		Chart:        "oci://example/block-node-server",
		ChartVersion: "0.46.0",
		PluginPreset: PresetTier1LFH,
	})
	hm.EXPECT().PullChart(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return("", errorx.ExternalError.New("registry down"))

	err := m.resolvePluginList(context.Background(), "")
	require.Error(t, err)
	assert.Empty(t, m.blockNodeInputs.PluginList)
}

func TestResolvePluginList_UnknownPresetListsAvailable(t *testing.T) {
	// A chart that ships rfh + minimal but not the requested profile.
	tgz := writeFixtureChart(t, "0.46.0", map[string]string{
		"values-overrides/plugin-profile-rfh.yaml":     rfhProfileBody,
		"values-overrides/plugin-profile-minimal.yaml": "plugins:\n  names: \"health\"\n",
	})
	m, hm := managerWithHelmMock(t, models.BlockNodeInputs{
		Chart:        "oci://example/block-node-server",
		ChartVersion: "0.46.0",
		PluginPreset: "bogus",
	})
	hm.EXPECT().PullChart(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(tgz, nil)

	err := m.resolvePluginList(context.Background(), "")
	require.Error(t, err)
	// The error names the bad preset and lists the profiles the chart does ship.
	assert.Contains(t, err.Error(), "bogus")
	assert.Contains(t, err.Error(), "minimal")
	assert.Contains(t, err.Error(), "rfh")
	assert.Empty(t, m.blockNodeInputs.PluginList)
}
