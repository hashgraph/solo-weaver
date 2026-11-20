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
//  3. --plugin-preset=lfh|rfh (incl. the default) — pull the chart and read
//     plugin-profile-<profile>.yaml.
//  4. --plugin-preset=none with no --values names — read the chart's own values.yaml
//     default plugins.names.
//
// Preset values are the chart's own profile names (lfh, rfh), matching the block-node
// chart's plugin-profile-<name>.yaml and the forthcoming chart plugins.profile selector.
// The legacy tier1-lfh / tier1-rfh values are accepted as aliases for backward
// compatibility (see presetAliases / CanonicalPreset); callers canonicalize at the
// boundary so the rest of the code — and the hardware sizer — only sees canonical names.
const (
	// PresetLFH selects Local Full History storage (blocks stored on disk).
	// Resolves against the chart's values-overrides/plugin-profile-lfh.yaml.
	PresetLFH = "lfh"

	// PresetRFH selects Remote Full History storage (blocks stored in cloud storage).
	// Resolves against the chart's values-overrides/plugin-profile-rfh.yaml.
	PresetRFH = "rfh"

	// PresetTier1LFH is the deprecated alias for PresetLFH, accepted for backward
	// compatibility and canonicalized to "lfh".
	PresetTier1LFH = "tier1-lfh"

	// PresetTier1RFH is the deprecated alias for PresetRFH, accepted for backward
	// compatibility and canonicalized to "rfh".
	PresetTier1RFH = "tier1-rfh"

	// PresetCustom indicates the operator supplied an explicit plugin list, either via
	// --plugins or by typing a comma-/space-separated list at the interactive prompt.
	// The list is used verbatim; weaver does not read a chart profile for it.
	PresetCustom = "custom"

	// PresetNone means "do not override": the plugin list is left to the operator's
	// --values file (if it sets plugins.names) or the chart's built-in default.
	PresetNone = "none"
)

// presetAliases maps deprecated preset values to their canonical chart profile name.
var presetAliases = map[string]string{
	PresetTier1LFH: PresetLFH,
	PresetTier1RFH: PresetRFH,
}

// CanonicalPreset returns the canonical preset value for presetID, resolving a legacy
// tier1-* alias to its chart profile name (lfh/rfh) and leaving every other value
// (lfh, rfh, custom, none, "") unchanged. Apply it at input boundaries so the rest of
// the code and the hardware sizer only deal with canonical names.
func CanonicalPreset(presetID string) string {
	if canonical, ok := presetAliases[presetID]; ok {
		return canonical
	}
	return presetID
}

// chartProfilePrefix / chartProfileSuffix bracket the override filename for a preset:
// a chart-backed preset <name> resolves from values-overrides/plugin-profile-<name>.yaml.
// Deriving the filename (rather than keeping a lookup table) means any plugin profile the
// block-node chart ships is selectable by name with no solo-provisioner change — validity
// is checked against the pulled chart at resolve time.
const (
	chartProfilePrefix = "plugin-profile-"
	chartProfileSuffix = ".yaml"
)

// chartProfileFileName returns the override filename for a canonical chart-backed preset.
func chartProfileFileName(preset string) string {
	return chartProfilePrefix + preset + chartProfileSuffix
}

// presetLabels maps canonical preset IDs to human-readable labels for TUI display.
var presetLabels = map[string]string{
	PresetLFH:    "Tier 1 — Local Full History  (blocks stored on local disk)",
	PresetRFH:    "Tier 1 — Remote Full History  (blocks stored in cloud storage)",
	PresetCustom: "Custom  (type a comma- or space-separated plugin list)",
	PresetNone:   "Use Values File / Chart Default  (no override)",
}

// orderedPresets is the display order for the TUI select prompt. Selecting Custom
// opens a free-text prompt for the plugin names (there is no enumerated multi-select,
// since community plugins are not known to weaver).
var orderedPresets = []string{PresetLFH, PresetRFH, PresetCustom, PresetNone}

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

// PresetLabel returns the human-readable TUI label for a preset ID (resolving a
// legacy alias first), or the preset ID itself when no label is registered.
func PresetLabel(presetID string) string {
	if label, ok := presetLabels[CanonicalPreset(presetID)]; ok {
		return label
	}
	return presetID
}

// IsChartBackedPreset reports whether presetID selects a chart plugin profile — i.e. it
// is resolved at deploy time from values-overrides/plugin-profile-<name>.yaml — rather
// than custom, none, or an unset value. Any such name qualifies (including ones this
// binary has never heard of); whether the chart actually ships that profile is validated
// at resolve time, so a new BN profile needs no change here.
func IsChartBackedPreset(presetID string) bool {
	switch CanonicalPreset(presetID) {
	case "", PresetCustom, PresetNone:
		return false
	default:
		return true
	}
}
