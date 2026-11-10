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
	assert.Equal(t, []string{PresetTier1LFH, PresetTier1RFH, PresetCustom, PresetNone}, got)
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

func TestIsKnownPreset(t *testing.T) {
	assert.True(t, IsKnownPreset(PresetTier1LFH))
	assert.True(t, IsKnownPreset(PresetTier1RFH))
	assert.False(t, IsKnownPreset(PresetCustom), "custom is not chart-resolved")
	assert.False(t, IsKnownPreset(PresetNone), "none is not chart-resolved")
	assert.False(t, IsKnownPreset("bogus"))
}

func TestChartProfileForPreset(t *testing.T) {
	assert.Equal(t, "plugin-profile-lfh.yaml", chartProfileForPreset[PresetTier1LFH])
	assert.Equal(t, "plugin-profile-rfh.yaml", chartProfileForPreset[PresetTier1RFH])
	_, ok := chartProfileForPreset[PresetNone]
	assert.False(t, ok)
	_, ok = chartProfileForPreset[PresetCustom]
	assert.False(t, ok)
}

func TestPresetLabel(t *testing.T) {
	assert.Contains(t, PresetLabel(PresetTier1LFH), "Local Full History")
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

func TestResolvePluginList_ExplicitListSkipsPull(t *testing.T) {
	m, _ := managerWithHelmMock(t, models.BlockNodeInputs{
		PluginPreset: PresetTier1LFH,
		PluginList:   "x,y,z",
	})
	// No PullChart expectation: calling it would fail the mock controller.
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

func TestResolvePluginList_NoneSkipsPull(t *testing.T) {
	m, _ := managerWithHelmMock(t, models.BlockNodeInputs{PluginPreset: PresetNone})
	require.NoError(t, m.resolvePluginList(context.Background(), ""))
	assert.Equal(t, "", m.blockNodeInputs.PluginList)
}

func TestResolvePluginList_PresetReadsChartProfile(t *testing.T) {
	cases := []struct {
		preset   string
		profile  string
		body     string
		wantList string
	}{
		{PresetTier1LFH, "values-overrides/plugin-profile-lfh.yaml", lfhProfileBody, "health,facility-app-state,stream-publisher"},
		{PresetTier1RFH, "values-overrides/plugin-profile-rfh.yaml", rfhProfileBody, "health,facility-app-state,cloud-storage-archive"},
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

func TestResolvePluginList_MissingProfileInChart(t *testing.T) {
	// A chart that ships no lfh profile file.
	tgz := writeFixtureChart(t, "0.46.0", map[string]string{
		"values-overrides/plugin-profile-rfh.yaml": rfhProfileBody,
	})
	m, hm := managerWithHelmMock(t, models.BlockNodeInputs{
		Chart:        "oci://example/block-node-server",
		ChartVersion: "0.46.0",
		PluginPreset: PresetTier1LFH,
	})
	hm.EXPECT().PullChart(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(tgz, nil)

	err := m.resolvePluginList(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin-profile-lfh.yaml")
}
