// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/deps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
)

// storagePathMode constants define the two mutually exclusive storage configuration modes.
const (
	storagePathModeBasePath   = "Single base path"
	storagePathModeIndividual = "Individual paths"
)

// validateRetentionThreshold validates that a retention threshold is a non-negative integer.
func validateRetentionThreshold(s string) error {
	if s == "" {
		return errorx.IllegalArgument.New("retention threshold cannot be empty")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return errorx.IllegalArgument.New("retention threshold must be a non-negative integer")
	}
	if n < 0 {
		return errorx.IllegalArgument.New("retention threshold must be a non-negative integer")
	}
	return nil
}

// validateOptionalPath validates a filesystem path when non-empty.
// An empty value is allowed because storage paths are optional when --base-path covers them.
func validateOptionalPath(s string) error {
	if s == "" {
		return nil
	}
	_, err := sanity.SanitizePath(s)
	return err
}

// validateRequiredPath validates that a path is non-empty and syntactically valid.
// Used for individual storage path prompts when the operator has explicitly chosen
// individual-paths mode, where every path must be provided.
func validateRequiredPath(s string) error {
	if s == "" {
		return errorx.IllegalArgument.New("path cannot be empty in individual paths mode")
	}
	_, err := sanity.SanitizePath(s)
	return err
}

// ── private per-field prompt builders ────────────────────────────────────────
// Each function returns a single ready-to-use InputPrompt. The public functions
// below compose subsets of these, keeping each definition in exactly one place.

func namespaceInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "namespace",
		Title:          "Kubernetes Namespace",
		Description:    "Namespace for the block node Helm release",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate: func(s string) error {
			if s == "" {
				return errorx.IllegalArgument.New("namespace cannot be empty")
			}
			return sanity.ValidateIdentifier(s)
		},
	}
}

func releaseNameInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "release-name",
		Title:          "Helm Release Name",
		Description:    "Name for the Helm release",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate: func(s string) error {
			if s == "" {
				return errorx.IllegalArgument.New("release name cannot be empty")
			}
			return sanity.ValidateIdentifier(s)
		},
	}
}

func chartVersionInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "chart-version",
		Title:          "Chart Version",
		Description:    "Helm chart version to deploy",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate: func(s string) error {
			if s == "" {
				return errorx.IllegalArgument.New("chart version cannot be empty")
			}
			return sanity.ValidateVersion(s)
		},
	}
}

func historicRetentionInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "historic-retention",
		Title:          "Historic Block Retention Threshold",
		Description:    "Number of blocks to preserve in historic storage (0 = unlimited)",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateRetentionThreshold,
	}
}

func recentRetentionInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "recent-retention",
		Title:          "Recent Block Retention Threshold",
		Description:    "Number of blocks to preserve in recent storage before deleting older files",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateRetentionThreshold,
	}
}

func basePathInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "base-path",
		Title:          "Storage Base Path",
		Description:    "Root directory for all storage volumes. Subdirectories for archive, live, log, verification, and plugins are created automatically.",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateRequiredPath,
	}
}

func archivePathInputPrompt(eff string, target *string, required bool) InputPrompt {
	validate := validateOptionalPath
	desc := "Path for archive storage. Optional when --base-path is set; required if base-path is empty."
	if required {
		validate = validateRequiredPath
		desc = "Path for archive storage."
	}
	return InputPrompt{
		FlagName:       "archive-path",
		Title:          "Archive Storage Path",
		Description:    desc,
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validate,
	}
}

func livePathInputPrompt(eff string, target *string, required bool) InputPrompt {
	validate := validateOptionalPath
	desc := "Path for live storage. Optional when --base-path is set; required if base-path is empty."
	if required {
		validate = validateRequiredPath
		desc = "Path for live storage."
	}
	return InputPrompt{
		FlagName:       "live-path",
		Title:          "Live Storage Path",
		Description:    desc,
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validate,
	}
}

func logPathInputPrompt(eff string, target *string, required bool) InputPrompt {
	validate := validateOptionalPath
	desc := "Path for log storage. Optional when --base-path is set; required if base-path is empty."
	if required {
		validate = validateRequiredPath
		desc = "Path for log storage."
	}
	return InputPrompt{
		FlagName:       "log-path",
		Title:          "Log Storage Path",
		Description:    desc,
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validate,
	}
}

func verificationPathInputPrompt(eff string, target *string, required bool) InputPrompt {
	validate := validateOptionalPath
	desc := "Path for verification storage. Optional when --base-path is set; required if base-path is empty."
	if required {
		validate = validateRequiredPath
		desc = "Path for verification storage."
	}
	return InputPrompt{
		FlagName:       "verification-path",
		Title:          "Verification Storage Path",
		Description:    desc,
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validate,
	}
}

func pluginsPathInputPrompt(eff string, target *string, required bool) InputPrompt {
	validate := validateOptionalPath
	desc := "Path for plugins storage. Optional when --base-path is set; required if base-path is empty."
	if required {
		validate = validateRequiredPath
		desc = "Path for plugins storage."
	}
	return InputPrompt{
		FlagName:       "plugins-path",
		Title:          "Plugins Storage Path",
		Description:    desc,
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validate,
	}
}

func applicationStatePathInputPrompt(eff string, target *string, required bool) InputPrompt {
	validate := validateOptionalPath
	desc := "Path for application-state storage. Optional when --base-path is set; required if base-path is empty."
	if required {
		validate = validateRequiredPath
		desc = "Path for application-state storage."
	}
	return InputPrompt{
		FlagName:       "application-state-path",
		Title:          "Application-State Storage Path",
		Description:    desc,
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validate,
	}
}

// ── Public prompt builders ────────────────────────────────────────────────────

// StoragePathTargets holds pointers to the Cobra flag variables for all storage
// path flags. It is used by RunStoragePathPrompts to keep the parameter count
// within linter limits.
type StoragePathTargets struct {
	BasePath             *string
	ArchivePath          *string
	LivePath             *string
	LogPath              *string
	VerificationPath     *string
	PluginsPath          *string
	ApplicationStatePath *string
}

// RunStoragePathPrompts presents a two-pass interactive storage path prompt.
//
// Pass 1 — mode select: asks whether the operator wants to configure a single
// base directory or each path individually, pre-selecting the mode that matches
// the current on-disk state.
//
// Pass 2 — path inputs: shows only the fields relevant to the chosen mode.
// When base-path mode is selected, only the --base-path input is shown.
// When individual mode is selected, the five individual path inputs are shown
// (archive, live, log, verification, plugins).
//
// The function is a no-op when any storage-related flag was already provided on
// the command line — the caller's flag values are respected as-is.
//
// Parameters:
//   - cmd:          the Cobra command (used for flag-changed detection)
//   - defaults:     prompt defaults read from the on-disk state file
//   - chartVersion: the target chart version, used to filter optional-storage prompts
//   - targets:      pointers to the seven storage path flag variables
//   - cv:           chosen-values collector for summary printing
//
// The optional-storage prompts (verification, plugins, application-state) are
// gated on the registry's GetApplicableOptionalStorages(chartVersion) so the
// operator only sees the storages the target chart actually needs.
func RunStoragePathPrompts(
	cmd *cobra.Command,
	defaults state.PromptDefaults,
	chartVersion string,
	targets StoragePathTargets,
	cv *ChosenValues,
) error {
	// If the user already supplied any storage flag on the CLI, respect it and skip prompts.
	for _, f := range []string{"base-path", "archive-path", "live-path", "log-path", "verification-path", "plugins-path", "application-state-path"} {
		if flagWasSet(cmd, f) {
			return nil
		}
	}

	cfg := config.Get()
	stor := defaults.BlockNode.Storage
	cfgStor := cfg.BlockNode.Storage

	// Determine which optional storages apply to the target chart version.
	applicable := blocknode.GetApplicableOptionalStorages(chartVersion)
	includeVerification := false
	includePlugins := false
	includeApplicationState := false
	optionalLabels := make([]string, 0, len(applicable))
	for _, optStor := range applicable {
		switch optStor.Name {
		case "verification":
			includeVerification = true
		case "plugins":
			includePlugins = true
		case "application-state":
			includeApplicationState = true
		}
		optionalLabels = append(optionalLabels, optStor.Name)
	}

	// Determine the default mode from persisted state.
	stateInBasepathMode := strings.TrimSpace(stor.BasePath) != ""
	defaultMode := storagePathModeIndividual
	if stateInBasepathMode {
		defaultMode = storagePathModeBasePath
	}

	// ── Pass 1: mode select ───────────────────────────────────────────────────
	selectedMode := defaultMode
	individualLabel := "Individual paths  (archive, live, log"
	for _, n := range optionalLabels {
		individualLabel += ", " + n
	}
	individualLabel += " must all be provided)"
	modeField := huh.NewSelect[string]().
		Key("storage-mode").
		Title("Storage Path Mode").
		Description("Choose how to configure storage paths for this block node").
		Options(
			huh.NewOption("Single base path  (subdirectories are created automatically)", storagePathModeBasePath),
			huh.NewOption(individualLabel, storagePathModeIndividual),
		).
		Value(&selectedMode)

	modeForm := huh.NewForm(huh.NewGroup(modeField)).
		WithTheme(SoloTheme()).
		WithShowHelp(true)

	if err := modeForm.Run(); err != nil {
		return wrapFormError(err)
	}

	cv.add("Storage Path Mode", selectedMode)

	// ── Pass 2: path inputs based on chosen mode ──────────────────────────────
	var inputPrompts []InputPrompt
	if selectedMode == storagePathModeBasePath {
		eff := resolveEffective(stor.BasePath, cfgStor.BasePath, deps.BLOCK_NODE_STORAGE_BASE_PATH)
		inputPrompts = []InputPrompt{
			basePathInputPrompt(eff, targets.BasePath),
		}
	} else {
		// Derive per-path defaults from the effective base path so that individual
		// path inputs are pre-filled even when no explicit per-path config exists
		// (e.g. fresh install or switching from base-path mode).
		// The subdirectory names mirror those used by GetStoragePaths at workflow time.
		effectiveBase := resolveEffective(stor.BasePath, cfgStor.BasePath, deps.BLOCK_NODE_STORAGE_BASE_PATH)
		indivDefault := func(subdir string) string { return path.Join(effectiveBase, subdir) }

		inputPrompts = []InputPrompt{
			archivePathInputPrompt(resolveEffective(stor.ArchivePath, cfgStor.ArchivePath, indivDefault("archive")), targets.ArchivePath, true),
			livePathInputPrompt(resolveEffective(stor.LivePath, cfgStor.LivePath, indivDefault("live")), targets.LivePath, true),
			logPathInputPrompt(resolveEffective(stor.LogPath, cfgStor.LogPath, indivDefault("logs")), targets.LogPath, true),
		}
		if includeVerification {
			inputPrompts = append(inputPrompts,
				verificationPathInputPrompt(resolveEffective(stor.VerificationPath, cfgStor.VerificationPath, indivDefault("verification")), targets.VerificationPath, true))
		}
		if includePlugins {
			inputPrompts = append(inputPrompts,
				pluginsPathInputPrompt(resolveEffective(stor.PluginsPath, cfgStor.PluginsPath, indivDefault("plugins")), targets.PluginsPath, true))
		}
		if includeApplicationState {
			inputPrompts = append(inputPrompts,
				applicationStatePathInputPrompt(resolveEffective(stor.ApplicationStatePath, cfgStor.ApplicationStatePath, indivDefault("application-state")), targets.ApplicationStatePath, true))
		}
	}

	return RunInputPrompts(cmd, inputPrompts, cv)
}

// BlockNodeSelectPrompts returns the select-type prompts for block node commands.
// The defaults parameter provides persisted state values read once by the caller;
// the effective profile is resolved using the priority: persisted state > config > default.
//
// Parameters:
//   - defaults:     prompt defaults read from the on-disk state file
//   - flagProfile:  pointer to the flag variable for --profile
func BlockNodeSelectPrompts(defaults state.PromptDefaults, flagProfile *string) []SelectPrompt {
	effectiveProfile := resolveEffective(defaults.Profile, config.Get().Profile, "")

	return []SelectPrompt{
		{
			FlagName:       "profile",
			Title:          "Deployment Profile",
			Description:    "Select the target deployment profile for this block node",
			Options:        models.SupportedProfiles(),
			EffectiveValue: effectiveProfile,
			Target:         flagProfile,
		},
	}
}

// BlockNodeInputPrompts returns the text-input-type prompts for install/upgrade commands.
// Includes namespace, release-name, chart-version and retention thresholds.
//
// Parameters:
//   - defaults:                prompt defaults read from the on-disk state file
//   - flagNamespace:           pointer to the flag variable for --namespace
//   - flagReleaseName:         pointer to the flag variable for --release-name
//   - flagChartVersion:        pointer to the flag variable for --chart-version
//   - flagHistoricRetention:   pointer to the flag variable for --historic-retention
//   - flagRecentRetention:     pointer to the flag variable for --recent-retention
func BlockNodeInputPrompts(defaults state.PromptDefaults, flagNamespace, flagReleaseName, flagChartVersion, flagHistoricRetention, flagRecentRetention *string) []InputPrompt {
	cfg := config.Get()
	summary := defaults.BlockNode

	return []InputPrompt{
		namespaceInputPrompt(resolveEffective(summary.Namespace, cfg.BlockNode.Namespace, cfg.BlockNode.Namespace), flagNamespace),
		releaseNameInputPrompt(resolveEffective(summary.ReleaseName, cfg.BlockNode.Release, cfg.BlockNode.Release), flagReleaseName),
		chartVersionInputPrompt(resolveEffective(summary.ChartVersion, cfg.BlockNode.ChartVersion, cfg.BlockNode.ChartVersion), flagChartVersion),
		historicRetentionInputPrompt(resolveEffective(summary.HistoricRetention, cfg.BlockNode.HistoricRetention, models.DefaultHistoricRetention), flagHistoricRetention),
		recentRetentionInputPrompt(resolveEffective(summary.RecentRetention, cfg.BlockNode.RecentRetention, models.DefaultRecentRetention), flagRecentRetention),
	}
}

// BlockNodeReconfigureInputPrompts returns the text-input-type prompts for the
// reconfigure command. It omits fields that are immutable once a block node is
// installed (namespace, release-name) and chart-version (reconfigure never
// changes the chart version — use upgrade for that).
//
// Storage path prompts are handled separately by RunStoragePathPrompts, which
// presents a mode select (single base path vs individual paths) before showing
// only the relevant path inputs.
// Cross-field completeness is enforced at workflow time by ValidateStorageCompleteness.
//
// Parameters:
//   - defaults:                  prompt defaults read from the on-disk state file
//   - flagHistoricRetention:     pointer to the flag variable for --historic-retention
//   - flagRecentRetention:       pointer to the flag variable for --recent-retention
func BlockNodeReconfigureInputPrompts(
	defaults state.PromptDefaults,
	flagHistoricRetention, flagRecentRetention *string,
) []InputPrompt {
	cfg := config.Get()

	return []InputPrompt{
		historicRetentionInputPrompt(resolveEffective(defaults.BlockNode.HistoricRetention, cfg.BlockNode.HistoricRetention, models.DefaultHistoricRetention), flagHistoricRetention),
		recentRetentionInputPrompt(resolveEffective(defaults.BlockNode.RecentRetention, cfg.BlockNode.RecentRetention, models.DefaultRecentRetention), flagRecentRetention),
	}
}

// RunPluginPresetPrompts presents a two-pass interactive plugin preset prompt.
//
// Pass 1 — preset select: asks which preset to deploy, pre-selecting the last
// used preset read from the on-disk state file.
//
// Pass 2 — custom plugin multi-select (conditional): when the operator selects
// the Custom preset, a multi-select is shown listing all known block-node
// plugins for the given chartVersion. The resulting selection is joined as a
// comma-separated string and written to *flagPlugins.
//
// The function is a no-op when either flag was already supplied on the command
// line — the caller's values are respected as-is.
//
// Parameters:
//   - cmd:             the Cobra command (used for flag-changed detection)
//   - defaults:        prompt defaults read from the on-disk state file
//   - flagPluginPreset: pointer to the --plugin-preset flag variable
//   - flagPlugins:     pointer to the --plugins flag variable
//   - chartVersion:   target chart version (used to filter available plugins)
//   - cv:              chosen-values collector for summary printing
func RunPluginPresetPrompts(
	cmd *cobra.Command,
	defaults state.PromptDefaults,
	flagPluginPreset *string,
	flagPlugins *string,
	chartVersion string,
	cv *ChosenValues,
) error {
	// If the operator already supplied --plugins, no prompting is needed.
	if flagWasSet(cmd, "plugins") {
		return nil
	}
	// If the operator already supplied --plugin-preset, no prompting is needed.
	if flagWasSet(cmd, "plugin-preset") {
		return nil
	}

	// ── Pass 1: preset selection ───────────────────────────────────────────────
	effectivePreset := resolveEffective(defaults.BlockNode.PluginPreset, "", blocknode.PresetTier1LFH)
	*flagPluginPreset = effectivePreset

	var options []huh.Option[string]
	for _, id := range blocknode.AvailablePresets() {
		options = append(options, huh.NewOption(blocknode.PresetLabel(id), id))
	}

	presetField := huh.NewSelect[string]().
		Key("plugin-preset").
		Title("Block Node Plugin Preset").
		Description("Select the plugin set to deploy with this block node").
		Options(options...).
		Value(flagPluginPreset)

	presetForm := huh.NewForm(huh.NewGroup(presetField)).
		WithTheme(SoloTheme()).
		WithShowHelp(true)

	if err := presetForm.Run(); err != nil {
		return wrapFormError(err)
	}

	cv.add("Plugin Preset", blocknode.PresetLabel(*flagPluginPreset))

	// ── Pass 2: custom multi-select (only when Custom was chosen) ─────────────
	if *flagPluginPreset != blocknode.PresetCustom {
		return nil
	}

	// Pre-select the plugins from the last-used custom list (if any),
	// but filter out any entries that no longer exist in the current
	// available plugin list so the UI does not silently drop them.
	versionedPlugins := blocknode.PluginsForVersion(chartVersion)
	validPlugins := make(map[string]struct{}, len(versionedPlugins))
	var pluginOptions []huh.Option[string]
	for _, p := range versionedPlugins {
		validPlugins[p] = struct{}{}
		pluginOptions = append(pluginOptions, huh.NewOption(p, p))
	}

	var preSelected []string
	var unknownPlugins []string
	if defaults.BlockNode.PluginList != "" {
		for _, plugin := range strings.Split(defaults.BlockNode.PluginList, ",") {
			plugin = strings.TrimSpace(plugin)
			if plugin == "" {
				continue
			}
			if _, ok := validPlugins[plugin]; ok {
				preSelected = append(preSelected, plugin)
				continue
			}
			unknownPlugins = append(unknownPlugins, plugin)
		}
	}

	selectedPlugins := preSelected

	description := "Choose the individual plugins to install"
	if len(unknownPlugins) > 0 {
		description = fmt.Sprintf(
			"%s\nWarning: previously saved plugins are no longer available and were not pre-selected: %s",
			description,
			strings.Join(unknownPlugins, ", "),
		)
	}

	customField := huh.NewMultiSelect[string]().
		Key("plugins").
		Title("Select Plugins").
		Description(description).
		Options(pluginOptions...).
		Value(&selectedPlugins).
		Validate(func(selected []string) error {
			if len(selected) == 0 {
				return errorx.IllegalArgument.New("at least one plugin must be selected for the Custom preset")
			}
			return nil
		})

	customForm := huh.NewForm(huh.NewGroup(customField)).
		WithTheme(SoloTheme()).
		WithShowHelp(true)

	if err := customForm.Run(); err != nil {
		return wrapFormError(err)
	}

	*flagPlugins = strings.Join(selectedPlugins, ",")
	cv.add("Custom Plugins", *flagPlugins)

	return nil
}

// resolveEffective returns the first non-empty value from the priority chain:
// persisted state > config file > compile-time default.
func resolveEffective(stateValue, configValue, defaultValue string) string {
	if stateValue != "" {
		return stateValue
	}
	if configValue != "" {
		return configValue
	}
	return defaultValue
}
