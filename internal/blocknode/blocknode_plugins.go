// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"strings"
	"unicode"
)

// Plugin preset IDs used by the --plugin-preset flag and TUI prompt.
//
// Presets no longer carry hardcoded plugin lists. The canonical per-version
// plugin set lives in the block-node chart itself, under
// values-overrides/plugin-profile-{lfh,rfh}.yaml. At deploy time weaver pulls the
// target chart and reads the profile that matches the selected preset, so the
// plugin list is always correct for the chart version being installed and a BN
// release that changes a profile needs no solo-provisioner release.
//
// Resolution precedence (first match wins) is implemented in resolvePluginList:
//  1. --plugins (explicit list) — use it, no chart pull.
//  2. --values file that defines plugins.names — use it, no chart pull.
//  3. --plugin-preset=tier1-lfh|tier1-rfh (incl. the default) — pull the chart and
//     read plugin-profile-<profile>.yaml.
//  4. --plugin-preset=none with no --values names — omit plugins.names; the chart's
//     own default applies.
const (
	// PresetTier1LFH selects Local Full History storage (blocks stored on disk).
	// Resolves against the chart's values-overrides/plugin-profile-lfh.yaml.
	PresetTier1LFH = "tier1-lfh"

	// PresetTier1RFH selects Remote Full History storage (blocks stored in cloud storage).
	// Resolves against the chart's values-overrides/plugin-profile-rfh.yaml.
	PresetTier1RFH = "tier1-rfh"

	// PresetCustom indicates the operator supplied an explicit plugin list, either via
	// --plugins or by typing a comma-/space-separated list at the interactive prompt.
	// The list is used verbatim; weaver does not read a chart profile for it.
	PresetCustom = "custom"

	// PresetNone means "do not override": the plugin list is left to the operator's
	// --values file (if it sets plugins.names) or the chart's built-in default.
	// It resolves to an empty plugin list and triggers no chart pull.
	PresetNone = "none"
)

// chartProfileForPreset maps a chart-backed preset to the override filename (under
// the chart's values-overrides/ directory) that holds its canonical plugins.names.
// Presets absent from this map (custom, none) are not resolved from the chart.
var chartProfileForPreset = map[string]string{
	PresetTier1LFH: "plugin-profile-lfh.yaml",
	PresetTier1RFH: "plugin-profile-rfh.yaml",
}

// presetLabels maps preset IDs to human-readable labels for TUI display.
var presetLabels = map[string]string{
	PresetTier1LFH: "Tier 1 — Local Full History  (blocks stored on local disk)",
	PresetTier1RFH: "Tier 1 — Remote Full History  (blocks stored in cloud storage)",
	PresetCustom:   "Custom  (type a comma- or space-separated plugin list)",
	PresetNone:     "Use Values File / Chart Default  (no override)",
}

// orderedPresets is the display order for the TUI select prompt. Selecting Custom
// opens a free-text prompt for the plugin names (there is no enumerated multi-select,
// since community plugins are not known to weaver).
var orderedPresets = []string{PresetTier1LFH, PresetTier1RFH, PresetCustom, PresetNone}

// NormalizePluginList accepts a plugin list that is comma- or space-separated (or a
// mix) and returns a clean, comma-separated list: entries are trimmed and empty
// entries dropped. It is the single normaliser for both the --plugins flag and the
// interactive custom prompt, so an operator may type "a,b c" or "a  b,c".
func NormalizePluginList(s string) string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	return strings.Join(fields, ",")
}

// AvailablePresets returns the ordered preset IDs for TUI display.
func AvailablePresets() []string {
	result := make([]string, len(orderedPresets))
	copy(result, orderedPresets)
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

// IsKnownPreset returns true when presetID is a recognised chart-backed preset
// (one weaver resolves from the chart's values-overrides). custom and none are not.
func IsKnownPreset(presetID string) bool {
	_, ok := chartProfileForPreset[presetID]
	return ok
}
