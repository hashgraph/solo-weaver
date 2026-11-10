// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"path"
	"strings"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/chart/loader"
)

// chartProfilesSubDir is the directory inside the packaged block-node chart that
// holds the plugin-profile override files weaver reads to resolve a preset.
const chartProfilesSubDir = "values-overrides"

// resolvePluginList fills m.blockNodeInputs.PluginList for a chart-backed preset by
// pulling the target chart and reading the plugin-profile override that matches the
// selected preset. It is a no-op — leaving PluginList untouched and performing no
// chart pull — whenever the operator has already overridden plugins (precedence,
// first match wins):
//
//  1. --plugins (explicit list) — PluginList already set.
//  2. --values file that defines plugins.names — the operator's file wins.
//  3. --plugin-preset=tier1-lfh|tier1-rfh — pull the chart, read plugin-profile-<p>.yaml.
//  4. none / custom-without-list / unset — leave empty; the chart's own default applies.
//
// Callers must invoke this before ComputeValuesFile renders values, because the
// storage decision (managesPluginsStorage) keys off the resolved PluginList.
func (m *Manager) resolvePluginList(ctx context.Context, valuesFile string) error {
	// (1) An explicit --plugins list was already resolved upstream.
	if strings.TrimSpace(m.blockNodeInputs.PluginList) != "" {
		return nil
	}

	// (2) An operator --values file that defines plugins.names wins; no pull. This
	// covers both a concrete list and an explicit empty (plugins-baked image), each
	// handled downstream by injectPluginsConfig.
	if _, defined := pluginsNamesFromValuesFile(valuesFile); defined {
		return nil
	}

	// (3) A chart-backed preset resolves from the chart's profile override.
	profileFile, ok := chartProfileForPreset[m.blockNodeInputs.PluginPreset]
	if !ok {
		// (4) none / custom-without-list / unset: leave empty so the chart default applies.
		return nil
	}

	names, err := m.pluginNamesFromChartProfile(ctx, profileFile)
	if err != nil {
		return err
	}
	m.blockNodeInputs.PluginList = names

	logx.As().Info().
		Str("preset", m.blockNodeInputs.PluginPreset).
		Str("profile", profileFile).
		Str("chartVersion", m.blockNodeInputs.ChartVersion).
		Str("plugins_names", names).
		Msg("Resolved block node plugin list from chart profile")
	return nil
}

// pluginNamesFromChartProfile pulls the block-node chart and returns the
// comma-separated plugins.names from its values-overrides/<profileFile>.
func (m *Manager) pluginNamesFromChartProfile(ctx context.Context, profileFile string) (string, error) {
	destDir := path.Join(models.Paths().TempDir, "bn-chart-profiles")

	tgz, err := m.helmManager.PullChart(ctx, destDir, m.blockNodeInputs.Chart, m.blockNodeInputs.ChartVersion)
	if err != nil {
		return "", errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to pull block-node chart %q version %q to read plugin profile %q",
				m.blockNodeInputs.Chart, m.blockNodeInputs.ChartVersion, profileFile),
			reasons.DownloadFailed,
			"Check network access to the chart registry, or pass an explicit plugin list with --plugins.",
		)
	}

	ch, err := loader.Load(tgz)
	if err != nil {
		return "", errx.Decorate(
			errorx.IllegalFormat.Wrap(err, "failed to load block-node chart archive %q", tgz),
			reasons.IntegrityCheckFailed,
			"Re-run the install; if it persists, pass an explicit plugin list with --plugins.",
		)
	}

	wantName := path.Join(chartProfilesSubDir, profileFile)
	for _, f := range ch.Files {
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

	return "", errx.Decorate(
		errorx.IllegalState.New("block-node chart %q version %q does not contain %q",
			m.blockNodeInputs.Chart, m.blockNodeInputs.ChartVersion, wantName),
		reasons.FileMissing,
		"The chart version may not ship this plugin profile; choose a different --plugin-preset or pass an explicit --plugins list.",
	)
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
