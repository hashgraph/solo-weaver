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

// storagePathField builds a huh input field from an InputPrompt whose validator
// is suppressed while the field's group is hidden (active() == false). Because
// huh only supports hiding at the group level, both storage-mode variants are
// pre-built as separate groups; gating the validator on the active mode ensures a
// pre-filled but not-currently-selected path never blocks form submission,
// independent of huh's hidden-group validation semantics.
func storagePathField(p InputPrompt, active func() bool) huh.Field {
	validate := p.Validate
	return huh.NewInput().
		Key(p.FlagName).
		Title(p.Title).
		Description(p.Description).
		Placeholder(p.Placeholder).
		Value(p.Target).
		Validate(func(s string) error {
			if !active() || validate == nil {
				return nil
			}
			return validate(s)
		})
}

// AddStoragePathPrompts appends the storage-path wizard pages: a mode-select page
// (single base path vs individual paths) followed by two mutually-exclusive path
// pages, each shown via huh.Group.WithHideFunc based on the selected mode. Because
// the mode select and the path groups live in the same wizard form, the operator
// can navigate back to the mode page and switch modes — the path page re-renders
// to match, including when navigating backward.
//
// The function is a no-op when any storage-related flag was already provided on
// the command line — the caller's flag values are respected as-is.
//
// Parameters:
//   - w:            the wizard accumulating pages
//   - cmd:          the Cobra command (used for flag-changed detection)
//   - defaults:     prompt defaults read from the on-disk state file
//   - chartVersion: the target chart version, used to filter optional-storage prompts
//   - targets:      pointers to the seven storage path flag variables
//   - cv:           chosen-values collector for summary printing
//
// The optional-storage prompts (verification, plugins, application-state) are
// gated on the registry's RequiredByVersion for the *live* chart version. Each
// optional path lives in its own huh group whose WithHideFunc re-reads
// *chartVersion (huh can hide only whole groups, not individual fields), so
// editing the chart-version input earlier in the same wizard re-shapes which
// optional paths are shown — and the individual-paths label — when the operator
// navigates forward. chartVersion is taken by pointer so huh's dynamic-func
// bindings observe those later edits.
func AddStoragePathPrompts(
	w *Wizard,
	cmd *cobra.Command,
	defaults state.PromptDefaults,
	chartVersion *string,
	targets StoragePathTargets,
	cv *ChosenValues,
) {
	// If the user already supplied any storage flag on the CLI, respect it and skip prompts.
	for _, f := range []string{"base-path", "archive-path", "live-path", "log-path", "verification-path", "plugins-path", "application-state-path"} {
		if flagWasSet(cmd, f) {
			return
		}
	}

	cfg := config.Get()
	stor := defaults.BlockNode.Storage
	cfgStor := cfg.BlockNode.Storage

	// applicableOptional reports whether the named optional storage is required by
	// the *current* chart-version value. It re-reads *chartVersion on every call so
	// the per-optional group hide-funcs and the mode-select OptionsFunc react as the
	// operator edits the chart-version input earlier in the wizard.
	optByName := make(map[string]blocknode.OptionalStorage)
	for _, o := range blocknode.GetOptionalStorages() {
		optByName[o.Name] = o
	}
	applicableOptional := func(name string) bool {
		o, ok := optByName[name]
		return ok && o.RequiredByVersion(*chartVersion)
	}

	// Determine the default mode from persisted state.
	stateInBasepathMode := strings.TrimSpace(stor.BasePath) != ""
	defaultMode := storagePathModeIndividual
	if stateInBasepathMode {
		defaultMode = storagePathModeBasePath
	}

	// selectedMode is shared by the mode-select field, the path groups' hide funcs,
	// and the afterRun callback below.
	selectedMode := defaultMode
	inBaseMode := func() bool { return selectedMode == storagePathModeBasePath }
	inIndividualMode := func() bool { return selectedMode == storagePathModeIndividual }

	// ── Page: mode select ─────────────────────────────────────────────────────
	// The individual-paths option label lists the optional storages that apply to
	// the live chart version, so it is rebuilt via OptionsFunc bound to
	// *chartVersion. The option *values* are stable (only the label changes), so
	// huh preserves the current selection across rebuilds.
	modeOptions := func() []huh.Option[string] {
		var label strings.Builder
		label.WriteString("Individual paths  (archive, live, log")
		for _, o := range blocknode.GetApplicableOptionalStorages(*chartVersion) {
			label.WriteString(", ")
			label.WriteString(o.Name)
		}
		label.WriteString(" must all be provided)")
		return []huh.Option[string]{
			huh.NewOption("Single base path  (subdirectories are created automatically)", storagePathModeBasePath),
			huh.NewOption(label.String(), storagePathModeIndividual),
		}
	}
	modeGroup := huh.NewGroup(
		huh.NewSelect[string]().
			Key("storage-mode").
			Title("Storage Path Mode").
			Description("Choose how to configure storage paths for this block node").
			OptionsFunc(modeOptions, chartVersion).
			Value(&selectedMode),
	)

	// ── Page: base-path input (shown only in single-base-path mode) ───────────
	baseEff := resolveEffective(stor.BasePath, cfgStor.BasePath, deps.BLOCK_NODE_STORAGE_BASE_PATH)
	basePrompt := basePathInputPrompt(baseEff, targets.BasePath)
	*targets.BasePath = baseEff
	baseGroup := huh.NewGroup(storagePathField(basePrompt, inBaseMode)).
		WithHideFunc(func() bool { return !inBaseMode() })

	// ── Pages: individual path inputs (shown only in individual mode) ─────────
	// Core paths (archive, live, log) apply to every chart version and share one
	// group. Each optional path lives in its own group whose visibility tracks the
	// live chart version, because huh can hide only whole groups, not fields.
	// Per-path defaults derive from the effective base path so inputs are pre-filled
	// even without explicit per-path config; subdirectory names mirror those used by
	// GetStoragePaths at workflow time.
	indivDefault := func(subdir string) string { return path.Join(baseEff, subdir) }

	// indivEntry couples an individual-path prompt with the optional-storage name it
	// represents ("" for a core path that always applies). optName drives both the
	// per-group hide func and the afterRun applicability filter.
	type indivEntry struct {
		prompt  InputPrompt
		optName string
	}
	entries := []indivEntry{
		{archivePathInputPrompt(resolveEffective(stor.ArchivePath, cfgStor.ArchivePath, indivDefault("archive")), targets.ArchivePath, true), ""},
		{livePathInputPrompt(resolveEffective(stor.LivePath, cfgStor.LivePath, indivDefault("live")), targets.LivePath, true), ""},
		{logPathInputPrompt(resolveEffective(stor.LogPath, cfgStor.LogPath, indivDefault("logs")), targets.LogPath, true), ""},
		{verificationPathInputPrompt(resolveEffective(stor.VerificationPath, cfgStor.VerificationPath, indivDefault("verification")), targets.VerificationPath, true), "verification"},
		{pluginsPathInputPrompt(resolveEffective(stor.PluginsPath, cfgStor.PluginsPath, indivDefault("plugins")), targets.PluginsPath, true), "plugins"},
		{applicationStatePathInputPrompt(resolveEffective(stor.ApplicationStatePath, cfgStor.ApplicationStatePath, indivDefault("application-state")), targets.ApplicationStatePath, true), "application-state"},
	}

	// visible reports whether an entry's field should be shown and validated:
	// always in individual mode for core paths, and additionally gated on the live
	// chart version for optional paths.
	visible := func(e indivEntry) func() bool {
		return func() bool {
			if !inIndividualMode() {
				return false
			}
			return e.optName == "" || applicableOptional(e.optName)
		}
	}

	var coreFields []huh.Field
	var optGroups []*huh.Group
	for i := range entries {
		e := entries[i]
		*e.prompt.Target = e.prompt.EffectiveValue // pre-fill so the input shows the default
		active := visible(e)
		field := storagePathField(e.prompt, active)
		if e.optName == "" {
			coreFields = append(coreFields, field)
			continue
		}
		// One group per optional path so its visibility can track the live version.
		optGroups = append(optGroups, huh.NewGroup(field).WithHideFunc(func() bool { return !active() }))
	}
	coreGroup := huh.NewGroup(coreFields...).
		WithHideFunc(func() bool { return !inIndividualMode() })

	// afterRun records the mode, normalises targets so downstream storage-mode
	// inference is unambiguous (both variants are pre-filled, so the unused one must
	// be cleared), and records the chosen paths into the summary. In individual mode
	// an optional path that does not apply to the *final* chart version is cleared so
	// a pre-filled but hidden path can never leak into the resolved inputs.
	after := func() {
		cv.add("Storage Path Mode", selectedMode)
		if selectedMode == storagePathModeBasePath {
			for i := range entries {
				*entries[i].prompt.Target = ""
			}
			cv.add(basePrompt.Title, *targets.BasePath)
			return
		}
		*targets.BasePath = ""
		for i := range entries {
			e := entries[i]
			if e.optName != "" && !applicableOptional(e.optName) {
				*e.prompt.Target = ""
				continue
			}
			cv.add(e.prompt.Title, *e.prompt.Target)
		}
	}

	groups := append([]*huh.Group{modeGroup, baseGroup, coreGroup}, optGroups...)
	w.addGroups(after, groups...)
}

// RunStoragePathPrompts presents the storage-path prompts as a standalone wizard.
// It is retained for callers that run storage prompts in isolation; the block-node
// install flow uses AddStoragePathPrompts to combine these pages with the rest of
// the installation wizard into one navigable form. It is a no-op when any
// storage-related flag was already provided on the command line.
func RunStoragePathPrompts(
	cmd *cobra.Command,
	defaults state.PromptDefaults,
	chartVersion string,
	targets StoragePathTargets,
	cv *ChosenValues,
) error {
	w := NewWizard()
	AddStoragePathPrompts(w, cmd, defaults, &chartVersion, targets, cv)
	return w.Run()
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

// AddPluginPresetPrompts appends the plugin wizard pages: a preset-select page
// and a conditional custom-plugin multi-select page shown via
// huh.Group.WithHideFunc only when the Custom preset is selected. Because both
// pages live in the same wizard form, the operator can navigate back to the preset
// page and switch away from Custom — the multi-select page hides again and
// --plugins is left empty (normalised in afterRun).
//
// The preset-select page pre-selects the last used preset read from the on-disk
// state file. When the operator's --values file defines plugins.names, the
// "no override" preset (PresetNone) is pre-selected instead so the values file
// wins unless the operator actively picks a preset.
//
// The custom multi-select page lists all known block-node plugins for the live
// chartVersion; the operator's selection is joined as a comma-separated string
// and written to *flagPlugins.
//
// The function is a no-op when either flag was already supplied on the command
// line — the caller's values are respected as-is.
//
// Parameters:
//   - w:               the wizard accumulating pages
//   - cmd:             the Cobra command (used for flag-changed detection)
//   - defaults:        prompt defaults read from the on-disk state file
//   - flagPluginPreset: pointer to the --plugin-preset flag variable
//   - flagPlugins:     pointer to the --plugins flag variable
//   - chartVersion:   pointer to the target chart version (used to filter available plugins)
//   - valuesFile:     path to the operator's --values file (empty when not supplied);
//     used to smart-default the preset to "no override" when it defines plugins.names
//   - cv:              chosen-values collector for summary printing
//
// The custom multi-select's option list and warning are rebuilt via OptionsFunc /
// DescriptionFunc bound to *chartVersion, so editing the chart-version input earlier
// in the same wizard re-shapes the available plugins; huh reconciles the current
// selection against the new option set (dropping plugins that no longer apply).
// chartVersion is taken by pointer so those bindings observe later edits.
func AddPluginPresetPrompts(
	w *Wizard,
	cmd *cobra.Command,
	defaults state.PromptDefaults,
	flagPluginPreset *string,
	flagPlugins *string,
	chartVersion *string,
	valuesFile string,
	cv *ChosenValues,
) {
	// If the operator already supplied --plugins, no prompting is needed.
	if flagWasSet(cmd, "plugins") {
		return
	}
	// If the operator already supplied --plugin-preset, no prompting is needed.
	if flagWasSet(cmd, "plugin-preset") {
		return
	}

	// ── Page: preset selection ─────────────────────────────────────────────────
	effectivePreset := resolveEffective(defaults.BlockNode.PluginPreset, "", blocknode.PresetTier1LFH)
	// Smart default: when the operator's --values file defines plugins.names, pre-select
	// the "no override" option so their file wins unless they actively pick a preset.
	// This overrides the tier1-lfh/state default (and is sticky when state already saved
	// "none", since resolveEffective returns it and this just re-selects it).
	if blocknode.ValuesFileDefinesPlugins(valuesFile) {
		effectivePreset = blocknode.PresetNone
	}
	*flagPluginPreset = effectivePreset

	var options []huh.Option[string]
	for _, id := range blocknode.AvailablePresets() {
		options = append(options, huh.NewOption(blocknode.PresetLabel(id), id))
	}

	presetGroup := huh.NewGroup(
		huh.NewSelect[string]().
			Key("plugin-preset").
			Title("Block Node Plugin Preset").
			Description("Select the plugin set to deploy with this block node").
			Options(options...).
			Value(flagPluginPreset),
	)

	// ── Page: custom multi-select (shown only when Custom is selected) ────────
	// The available plugins depend on the live chart version, so validForVersion
	// re-reads *chartVersion on every call. The last-used custom list (from state)
	// is pre-selected but filtered to the plugins that still apply.
	savedPlugins := make([]string, 0)
	for plugin := range strings.SplitSeq(defaults.BlockNode.PluginList, ",") {
		if plugin = strings.TrimSpace(plugin); plugin != "" {
			savedPlugins = append(savedPlugins, plugin)
		}
	}
	validForVersion := func() map[string]struct{} {
		vp := blocknode.PluginsForVersion(*chartVersion)
		set := make(map[string]struct{}, len(vp))
		for _, p := range vp {
			set[p] = struct{}{}
		}
		return set
	}
	pluginOptionsFn := func() []huh.Option[string] {
		var opts []huh.Option[string]
		for _, p := range blocknode.PluginsForVersion(*chartVersion) {
			opts = append(opts, huh.NewOption(p, p))
		}
		return opts
	}
	descriptionFn := func() string {
		valid := validForVersion()
		var unknown []string
		for _, p := range savedPlugins {
			if _, ok := valid[p]; !ok {
				unknown = append(unknown, p)
			}
		}
		desc := "Choose the individual plugins to install"
		if len(unknown) > 0 {
			desc = fmt.Sprintf(
				"%s\nWarning: previously saved plugins are not available for chart version %s and were not pre-selected: %s",
				desc, *chartVersion, strings.Join(unknown, ", "))
		}
		return desc
	}

	// Pre-select the saved plugins that apply to the version at construction time;
	// huh reconciles this selection against the option set whenever it is rebuilt.
	initialValid := validForVersion()
	var selectedPlugins []string
	for _, p := range savedPlugins {
		if _, ok := initialValid[p]; ok {
			selectedPlugins = append(selectedPlugins, p)
		}
	}

	isCustom := func() bool { return *flagPluginPreset == blocknode.PresetCustom }
	customGroup := huh.NewGroup(
		huh.NewMultiSelect[string]().
			Key("plugins").
			Title("Select Plugins").
			DescriptionFunc(descriptionFn, chartVersion).
			OptionsFunc(pluginOptionsFn, chartVersion).
			Value(&selectedPlugins).
			Validate(func(selected []string) error {
				// Only enforce a selection while the Custom preset is active; when
				// the group is hidden this validator must not block submission.
				if !isCustom() {
					return nil
				}
				if len(selected) == 0 {
					return errorx.IllegalArgument.New("at least one plugin must be selected for the Custom preset")
				}
				return nil
			}),
	).WithHideFunc(func() bool { return !isCustom() })

	after := func() {
		cv.add("Plugin Preset", blocknode.PresetLabel(*flagPluginPreset))
		if !isCustom() {
			// Ensure a pre-selected-but-hidden multi-select never contaminates a
			// non-custom preset.
			*flagPlugins = ""
			return
		}
		// Drop any selection that does not apply to the final chart version, in case
		// the version changed after the multi-select was last reconciled.
		valid := validForVersion()
		var final []string
		for _, p := range selectedPlugins {
			if _, ok := valid[p]; ok {
				final = append(final, p)
			}
		}
		*flagPlugins = strings.Join(final, ",")
		cv.add("Custom Plugins", *flagPlugins)
	}

	w.addGroups(after, presetGroup, customGroup)
}

// RunPluginPresetPrompts presents the plugin preset prompts as a standalone
// wizard. It is retained for callers that run plugin prompts in isolation; the
// block-node install flow uses AddPluginPresetPrompts to combine these pages with
// the rest of the installation wizard. It is a no-op when either --plugins or
// --plugin-preset was already provided on the command line.
func RunPluginPresetPrompts(
	cmd *cobra.Command,
	defaults state.PromptDefaults,
	flagPluginPreset *string,
	flagPlugins *string,
	chartVersion string,
	valuesFile string,
	cv *ChosenValues,
) error {
	w := NewWizard()
	AddPluginPresetPrompts(w, cmd, defaults, flagPluginPreset, flagPlugins, &chartVersion, valuesFile, cv)
	return w.Run()
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
