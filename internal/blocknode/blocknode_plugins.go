// SPDX-License-Identifier: Apache-2.0

package blocknode

import "github.com/hashgraph/solo-weaver/pkg/semver"

// Plugin preset IDs used by the --plugin-preset flag and TUI prompt.
//
// Each preset maps to a fixed, ordered plugin list maintained here in lockstep
// with block-node chart releases. When the block-node team changes a preset's
// contents or adds/removes presets, this file must be updated in the same
// release cycle.
//
// Adding a new BN version that changes plugin lists:
//  1. Add a new blockNodePluginConfig entry to blockNodePluginHistory (in
//     ascending MinVersion order).
//  2. Populate Presets with ALL preset IDs (copy unchanged ones from the
//     previous entry; update only the ones that changed).
//  3. Populate AllPlugins with the full list for that version.
//  4. Add tests in blocknode_plugins_test.go for the new version boundary.
//  5. No changes to PluginListForPreset, PluginsForVersion, or any caller.
const (
	// PresetTier1LFH selects Local Full History storage (blocks stored on disk).
	PresetTier1LFH = "tier1-lfh"

	// PresetTier1RFH selects Remote Full History storage (blocks stored in cloud storage).
	PresetTier1RFH = "tier1-rfh"

	// PresetCustom indicates the operator selected a custom set of plugins.
	PresetCustom = "custom"

	// PresetNone means "do not override": the plugin list is left to the operator's
	// --values file (if it sets plugins.names) or the chart's built-in default.
	// It resolves to an empty plugin list, so injectPluginsConfig is a no-op.
	// It is deliberately absent from every Presets map, so IsKnownPreset reports
	// false and PluginListForPreset returns "".
	PresetNone = "none"
)

// blockNodePluginConfig holds the canonical plugin configuration for a range of
// chart versions starting at MinVersion. Entries in blockNodePluginHistory must
// be in ascending MinVersion order; the first entry uses an empty MinVersion
// (baseline, applies to all versions below the next entry's MinVersion).
type blockNodePluginConfig struct {
	// MinVersion is the minimum chart version (inclusive) for this config.
	// Empty string means "from the very beginning" (baseline).
	MinVersion string
	// Presets maps each preset ID to its canonical comma-separated plugin list.
	// Source of truth: hiero-block-node chart values-overrides/plugin-profile-*.yaml.
	Presets map[string]string
	// AllPlugins is the ordered list of all available plugins for this version,
	// shown in the TUI custom multi-select.
	// Source of truth: hiero-block-node block-node/app/build.gradle.kts.
	AllPlugins []string
}

// blockNodePluginHistory is the version-ordered registry of plugin configurations.
// Add a new entry here (in ascending MinVersion order) whenever a BN release
// changes the plugin list. See the package comment above for the procedure.
var blockNodePluginHistory = []blockNodePluginConfig{
	{
		// Baseline: BN < 0.35.0. s3-archive was the cloud-storage plugin.
		MinVersion: "",
		Presets: map[string]string{
			PresetTier1LFH: "facility-messaging,block-access-service,health,server-status,stream-publisher,stream-subscriber,verification,blocks-file-historic,blocks-file-recent,backfill",
			PresetTier1RFH: "facility-messaging,block-access-service,health,server-status,stream-publisher,stream-subscriber,verification,blocks-file-recent,backfill,s3-archive",
		},
		AllPlugins: []string{
			"facility-messaging",
			"block-access-service",
			"health",
			"server-status",
			"stream-publisher",
			"stream-subscriber",
			"verification",
			"blocks-file-historic",
			"blocks-file-recent",
			"backfill",
			"s3-archive",
		},
	},
	{
		// BN 0.35.0: s3-archive replaced by cloud-storage-archive + cloud-storage-expanded.
		MinVersion: "0.35.0",
		Presets: map[string]string{
			PresetTier1LFH: "facility-messaging,block-access-service,health,server-status,stream-publisher,stream-subscriber,verification,blocks-file-historic,blocks-file-recent,backfill",
			PresetTier1RFH: "facility-messaging,block-access-service,health,server-status,stream-publisher,stream-subscriber,verification,blocks-file-recent,backfill,cloud-storage-archive,cloud-storage-expanded",
		},
		AllPlugins: []string{
			"facility-messaging",
			"block-access-service",
			"health",
			"server-status",
			"stream-publisher",
			"stream-subscriber",
			"verification",
			"blocks-file-historic",
			"blocks-file-recent",
			"backfill",
			"cloud-storage-archive",
			"cloud-storage-expanded",
		},
	},
}

// AllBlockNodePlugins is the ordered list of available plugins for the current
// (latest) chart release. Use PluginsForVersion when the target chart version
// may be older.
var AllBlockNodePlugins = blockNodePluginHistory[len(blockNodePluginHistory)-1].AllPlugins

// presetLabels maps preset IDs to human-readable labels for TUI display.
var presetLabels = map[string]string{
	PresetTier1LFH: "Tier 1 — Local Full History  (blocks stored on local disk)",
	PresetTier1RFH: "Tier 1 — Remote Full History  (blocks stored in cloud storage)",
	PresetCustom:   "Custom  (select individual plugins)",
	PresetNone:     "Use Values File / Chart Default  (no override)",
}

// orderedPresets is the display order for the TUI select prompt.
var orderedPresets = []string{PresetTier1LFH, PresetTier1RFH, PresetCustom, PresetNone}

// configForVersion returns the plugin configuration applicable to the given
// chart version. Empty or unparseable versions return the latest config.
func configForVersion(chartVersion string) blockNodePluginConfig {
	latest := blockNodePluginHistory[len(blockNodePluginHistory)-1]
	if chartVersion == "" {
		return latest
	}
	target, err := semver.NewSemver(chartVersion)
	if err != nil {
		return latest
	}
	for i := len(blockNodePluginHistory) - 1; i >= 0; i-- {
		cfg := blockNodePluginHistory[i]
		if cfg.MinVersion == "" {
			return cfg
		}
		minVer, err := semver.NewSemver(cfg.MinVersion)
		if err != nil {
			continue
		}
		if !target.LessThan(minVer) {
			return cfg
		}
	}
	return blockNodePluginHistory[0]
}

// AvailablePresets returns the ordered preset IDs for TUI display.
func AvailablePresets() []string {
	result := make([]string, len(orderedPresets))
	copy(result, orderedPresets)
	return result
}

// PluginListForPreset returns the canonical comma-separated plugin list for a
// named preset at the given chart version, or an empty string when the preset
// ID is not recognised. When chartVersion is empty, the current-default
// (latest) list is returned.
func PluginListForPreset(presetID, chartVersion string) string {
	return configForVersion(chartVersion).Presets[presetID]
}

// PluginsForVersion returns the ordered list of available plugins for the given
// chart version, for use in the TUI custom multi-select. When chartVersion is
// empty, the current-default (latest) list is returned. Always returns an
// independent copy.
func PluginsForVersion(chartVersion string) []string {
	src := configForVersion(chartVersion).AllPlugins
	result := make([]string, len(src))
	copy(result, src)
	return result
}

// PresetLabel returns the human-readable TUI label for a preset ID,
// or the preset ID itself when no label is registered.
func PresetLabel(presetID string) string {
	if label, ok := presetLabels[presetID]; ok {
		return label
	}
	return presetID
}

// IsKnownPreset returns true when presetID is a recognised non-custom preset.
func IsKnownPreset(presetID string) bool {
	// A preset is known if it appears in at least the latest config's Presets map.
	_, ok := blockNodePluginHistory[len(blockNodePluginHistory)-1].Presets[presetID]
	return ok
}
