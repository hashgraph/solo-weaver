// SPDX-License-Identifier: Apache-2.0

package blocknode

// Plugin preset IDs used by the --plugin-preset flag and TUI prompt.
//
// Each preset maps to a fixed, ordered plugin list maintained here in lockstep
// with block-node chart releases. When the block-node team changes a preset's
// contents or adds/removes presets, this file must be updated in the same
// release cycle.
const (
	// PresetTier1LFH selects Local Full History storage (blocks stored on disk).
	PresetTier1LFH = "tier1-lfh"

	// PresetTier1RFH selects Remote Full History storage (blocks stored in S3-compatible cloud storage).
	PresetTier1RFH = "tier1-rfh"

	// PresetCustom indicates the operator selected a custom set of plugins.
	PresetCustom = "custom"
)

// AllBlockNodePlugins is the ordered list of available plugins shown in the
// TUI multi-select when the operator chooses the Custom preset.
// Source of truth: hiero-block-node block-node/app/build.gradle.kts (blockNodePlugins configuration).
var AllBlockNodePlugins = []string{
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
}

// presetPlugins maps each preset ID to the canonical comma-separated plugin list.
// Source of truth: hiero-block-node chart values-overrides/plugin-profile-*.yaml.
var presetPlugins = map[string]string{
	PresetTier1LFH: "facility-messaging,block-access-service,health,server-status,stream-publisher,stream-subscriber,verification,blocks-file-historic,blocks-file-recent,backfill",
	PresetTier1RFH: "facility-messaging,block-access-service,health,server-status,stream-publisher,stream-subscriber,verification,blocks-file-recent,backfill,s3-archive",
}

// presetLabels maps preset IDs to human-readable labels for TUI display.
var presetLabels = map[string]string{
	PresetTier1LFH: "Tier 1 — Local Full History  (blocks stored on local disk)",
	PresetTier1RFH: "Tier 1 — Remote Full History  (blocks stored in S3-compatible cloud storage)",
	PresetCustom:   "Custom  (select individual plugins)",
}

// orderedPresets is the display order for the TUI select prompt.
var orderedPresets = []string{PresetTier1LFH, PresetTier1RFH, PresetCustom}

// AvailablePresets returns the ordered preset IDs for TUI display.
func AvailablePresets() []string {
	result := make([]string, len(orderedPresets))
	copy(result, orderedPresets)
	return result
}

// PluginListForPreset returns the canonical comma-separated plugin list for a
// named preset, or an empty string when the preset ID is not recognised.
func PluginListForPreset(presetID string) string {
	return presetPlugins[presetID]
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
	_, ok := presetPlugins[presetID]
	return ok
}
