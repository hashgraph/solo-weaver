// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
)

// chartProfilesSubDir is the directory inside the packaged block-node chart that
// holds the plugin-profile override files weaver reads to resolve a preset.
const chartProfilesSubDir = "values-overrides"

// chartDownloadsSubdir mirrors the workflow steps' convention: pulled chart archives
// live under <WeaverPaths.DownloadsDir>/charts so operators find them beside the other
// host artifacts rather than in a temp directory.
const chartDownloadsSubdir = "charts"

// resolvePluginList fills m.blockNodeInputs.PluginList by resolving the operator's
// selection against the target chart. Precedence (first match wins):
//
//  1. --plugins (explicit list) — PluginList already set; kept as-is (a best-effort
//     warning flags names the chart does not know, to catch typos early).
//  2. --values file that defines plugins.names — the operator's file wins; no resolution.
//  3. --plugin-preset=tier1-lfh|tier1-rfh — pull the chart, read plugin-profile-<p>.yaml.
//  4. none / unset — inject the chart's own values.yaml default plugins.names, so an
//     upgrade applies the target version's default instead of Helm --reuse-values keeping
//     a stale deployed list (the #1402 class). If the default can't be read, fall back to
//     leaving it unset so Helm still applies the chart default.
//
// Callers must invoke this before ComputeValuesFile renders values, because the
// storage decision (managesPluginsStorage) keys off the resolved PluginList.
func (m *Manager) resolvePluginList(ctx context.Context, valuesFile string) error {
	// (1) An explicit --plugins list was already resolved upstream; keep it, but warn
	// about names the chart's known set does not contain (likely typos).
	if list := strings.TrimSpace(m.blockNodeInputs.PluginList); list != "" {
		m.warnUnknownCustomPlugins(ctx, list)
		return nil
	}

	// (2) An operator --values file that defines plugins.names wins; no resolution. This
	// covers both a concrete list and an explicit empty (plugins-baked image), each
	// handled downstream by injectPluginsConfig.
	if _, defined := pluginsNamesFromValuesFile(valuesFile); defined {
		return nil
	}

	// (3) A chart-backed preset resolves from the chart's profile override, whose filename
	// is derived from the (canonical) preset name — so any profile the chart ships works
	// by name, and a tier1-* alias resolves to its canonical lfh/rfh name.
	if preset := CanonicalPreset(m.blockNodeInputs.PluginPreset); IsChartBackedPreset(preset) {
		profileFile := chartProfileFileName(preset)
		names, err := m.pluginNamesFromChartProfile(ctx, profileFile)
		if err != nil {
			return err
		}
		m.blockNodeInputs.PluginList = names
		logx.As().Info().
			Str("preset", preset).
			Str("profile", profileFile).
			Str("chartVersion", m.blockNodeInputs.ChartVersion).
			Str("plugins_names", names).
			Msg("Resolved block node plugin list from chart profile")
		return nil
	}

	// (4) none / unset: inject the chart's own default so upgrades stay version-correct.
	names, err := m.pluginNamesFromChartDefault(ctx)
	if err != nil {
		logx.As().Warn().Err(err).
			Msg("Could not read the chart's default plugins.names; leaving it to the chart/Helm")
		return nil
	}
	if names == "" {
		// The chart genuinely defaults to no plugins; leave it unset so the baked-image
		// signal and chart handling are preserved.
		return nil
	}
	m.blockNodeInputs.PluginList = names
	logx.As().Info().
		Str("preset", m.blockNodeInputs.PluginPreset).
		Str("chartVersion", m.blockNodeInputs.ChartVersion).
		Str("plugins_names", names).
		Msg("Resolved block node plugin list from chart default")
	return nil
}

// loadTargetChart pulls the target block-node chart (reusing the local cache) and loads
// it, so callers can read packaged files (plugin-profile overrides) or its default values.
func (m *Manager) loadTargetChart(ctx context.Context) (*chart.Chart, error) {
	destDir := path.Join(models.Paths().DownloadsDir, chartDownloadsSubdir)

	tgz, err := m.helmManager.PullChart(ctx, destDir, m.blockNodeInputs.Chart, m.blockNodeInputs.ChartVersion)
	if err != nil {
		return nil, errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to pull block-node chart %q version %q",
				m.blockNodeInputs.Chart, m.blockNodeInputs.ChartVersion),
			reasons.DownloadFailed,
			"Check network access to the chart registry, or pass an explicit plugin list with --plugins.",
		)
	}

	ch, err := loader.Load(tgz)
	if err != nil {
		return nil, errx.Decorate(
			errorx.IllegalFormat.Wrap(err, "failed to load block-node chart archive %q", tgz),
			reasons.IntegrityCheckFailed,
			"Re-run the install; if it persists, pass an explicit plugin list with --plugins.",
		)
	}
	return ch, nil
}

// pluginNamesFromChartDefault returns the trimmed plugins.names from the target chart's
// own values.yaml default (empty when the chart sets none).
func (m *Manager) pluginNamesFromChartDefault(ctx context.Context) (string, error) {
	ch, err := m.loadTargetChart(ctx)
	if err != nil {
		return "", err
	}
	return pluginNamesFromValuesMap(ch.Values), nil
}

// pluginNamesFromValuesMap extracts the trimmed plugins.names string from a chart's
// parsed values map (helm normalises nested values to map[string]interface{}).
func pluginNamesFromValuesMap(values map[string]interface{}) string {
	plugins, ok := values["plugins"].(map[string]interface{})
	if !ok {
		return ""
	}
	names, _ := plugins["names"].(string)
	return strings.TrimSpace(names)
}

// warnUnknownCustomPlugins logs a best-effort warning for any explicit --plugins name
// that is not in the chart's known plugin set (plugin-profile-all.yaml), so a typo is
// flagged up front rather than surfacing later as a resolve-plugins maven timeout.
// Community plugins (legitimately absent from all.yaml) still proceed — this never fails.
func (m *Manager) warnUnknownCustomPlugins(ctx context.Context, list string) {
	ch, err := m.loadTargetChart(ctx)
	if err != nil {
		return // best-effort: never block a custom install on the warning check
	}

	known := map[string]struct{}{}
	allName := path.Join(chartProfilesSubDir, "plugin-profile-all.yaml")
	for _, f := range ch.Files {
		if f.Name != allName {
			continue
		}
		names, perr := pluginNamesFromProfileYAML(f.Data)
		if perr != nil {
			return
		}
		for _, n := range strings.Split(names, ",") {
			if n = strings.TrimSpace(n); n != "" {
				known[n] = struct{}{}
			}
		}
	}
	if len(known) == 0 {
		return // no all-profile to compare against
	}

	var unknown []string
	for _, n := range strings.Split(list, ",") {
		if n = strings.TrimSpace(n); n != "" {
			if _, ok := known[n]; !ok {
				unknown = append(unknown, n)
			}
		}
	}
	if len(unknown) > 0 {
		logx.As().Warn().
			Strs("plugins", unknown).
			Msg("Custom plugins are not in the chart's known set (plugin-profile-all.yaml); check for typos. If these are community plugins, you can ignore this warning")
	}
}

// pluginNamesFromChartProfile loads the target chart and returns the comma-separated
// plugins.names from its values-overrides/<profileFile>.
func (m *Manager) pluginNamesFromChartProfile(ctx context.Context, profileFile string) (string, error) {
	ch, err := m.loadTargetChart(ctx)
	if err != nil {
		return "", err
	}

	wantName := path.Join(chartProfilesSubDir, profileFile)
	var available []string
	for _, f := range ch.Files {
		// Collect the profiles the chart actually ships, for a helpful error on a miss.
		if name, ok := profileNameFromFile(f.Name); ok {
			available = append(available, name)
		}
		if f.Name != wantName {
			continue
		}
		names, perr := pluginNamesFromProfileYAML(f.Data)
		if perr != nil {
			return "", perr
		}
		if names == "" {
			return "", errx.Decorate(
				errorx.IllegalState.New("block-node chart %q version %q profile %q defines no plugins.names",
					m.blockNodeInputs.Chart, m.blockNodeInputs.ChartVersion, wantName),
				reasons.PreconditionNotMet,
				"The chart version may predate this plugin profile; choose a different --plugin-preset or pass --plugins.",
			)
		}
		return names, nil
	}

	// The preset's profile file is not in this chart version: treat it as an invalid
	// --plugin-preset and list the profiles the chart does ship.
	sort.Strings(available)
	wantPreset := strings.TrimSuffix(strings.TrimPrefix(profileFile, chartProfilePrefix), chartProfileSuffix)
	return "", errx.Decorate(
		errorx.IllegalArgument.New("block-node chart %q version %q has no plugin profile %q (available: %s)",
			m.blockNodeInputs.Chart, m.blockNodeInputs.ChartVersion, wantPreset, strings.Join(available, ", ")),
		reasons.InvalidArgument,
		"Pass a valid --plugin-preset (one of the available profiles), or an explicit --plugins list.",
	)
}

// profileNameFromFile returns the preset name for a values-overrides/plugin-profile-*.yaml
// file path, and false for any other file.
func profileNameFromFile(filePath string) (string, bool) {
	prefix := path.Join(chartProfilesSubDir, chartProfilePrefix)
	if !strings.HasPrefix(filePath, prefix) || !strings.HasSuffix(filePath, chartProfileSuffix) {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(filePath, prefix), chartProfileSuffix), true
}

// pluginNamesFromProfileYAML extracts the trimmed plugins.names string from a
// plugin-profile override file's YAML body.
func pluginNamesFromProfileYAML(data []byte) (string, error) {
	var doc struct {
		Plugins struct {
			Names string `yaml:"names"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", errx.Decorate(
			errorx.IllegalFormat.Wrap(err, "failed to parse block-node plugin profile YAML"),
			reasons.IntegrityCheckFailed,
			"The chart's plugin profile is malformed; pass an explicit --plugins list to bypass it.",
		)
	}
	return strings.TrimSpace(doc.Plugins.Names), nil
}
