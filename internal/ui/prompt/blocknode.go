// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"fmt"
	"strconv"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
)

// validateRetentionThreshold validates that a retention threshold is a non-negative integer.
func validateRetentionThreshold(s string) error {
	if s == "" {
		return fmt.Errorf("retention threshold cannot be empty")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("retention threshold must be a non-negative integer")
	}
	if n < 0 {
		return fmt.Errorf("retention threshold must be a non-negative integer")
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
				return fmt.Errorf("namespace cannot be empty")
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
				return fmt.Errorf("release name cannot be empty")
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
				return fmt.Errorf("chart version cannot be empty")
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
		Description:    "Root directory for all storage volumes. Leave empty to set each path individually (archive, live, log, verification, plugins must all be provided when base-path is empty).",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateOptionalPath,
	}
}

func archivePathInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "archive-path",
		Title:          "Archive Storage Path",
		Description:    "Path for archive storage. Optional when --base-path is set; required if base-path is empty.",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateOptionalPath,
	}
}

func livePathInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "live-path",
		Title:          "Live Storage Path",
		Description:    "Path for live storage. Optional when --base-path is set; required if base-path is empty.",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateOptionalPath,
	}
}

func logPathInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "log-path",
		Title:          "Log Storage Path",
		Description:    "Path for log storage. Optional when --base-path is set; required if base-path is empty.",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateOptionalPath,
	}
}

func verificationPathInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "verification-path",
		Title:          "Verification Storage Path",
		Description:    "Path for verification storage. Optional when --base-path is set; required if base-path is empty.",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateOptionalPath,
	}
}

func pluginsPathInputPrompt(eff string, target *string) InputPrompt {
	return InputPrompt{
		FlagName:       "plugins-path",
		Title:          "Plugins Storage Path",
		Description:    "Path for plugins storage. Optional when --base-path is set; required if base-path is empty.",
		Placeholder:    eff,
		EffectiveValue: eff,
		Target:         target,
		Validate:       validateOptionalPath,
	}
}

// ── Public prompt builders ────────────────────────────────────────────────────

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
// Storage path prompts inform the operator that either --base-path alone or all
// individual paths (archive, live, log, verification, plugins) must be provided.
// Cross-field completeness is enforced at workflow time by ValidateStorageCompleteness.
//
// Parameters:
//   - defaults:                  prompt defaults read from the on-disk state file
//   - flagBasePath:              pointer to the flag variable for --base-path
//   - flagArchivePath:           pointer to the flag variable for --archive-path
//   - flagLivePath:              pointer to the flag variable for --live-path
//   - flagLogPath:               pointer to the flag variable for --log-path
//   - flagVerificationPath:      pointer to the flag variable for --verification-path
//   - flagPluginsPath:           pointer to the flag variable for --plugins-path
//   - flagHistoricRetention:     pointer to the flag variable for --historic-retention
//   - flagRecentRetention:       pointer to the flag variable for --recent-retention
func BlockNodeReconfigureInputPrompts(
	defaults state.PromptDefaults,
	flagBasePath, flagArchivePath, flagLivePath, flagLogPath, flagVerificationPath, flagPluginsPath *string,
	flagHistoricRetention, flagRecentRetention *string,
) []InputPrompt {
	cfg := config.Get()
	stor := defaults.BlockNode.Storage
	cfgStor := cfg.BlockNode.Storage

	return []InputPrompt{
		basePathInputPrompt(resolveEffective(stor.BasePath, cfgStor.BasePath, cfgStor.BasePath), flagBasePath),
		archivePathInputPrompt(resolveEffective(stor.ArchivePath, cfgStor.ArchivePath, cfgStor.ArchivePath), flagArchivePath),
		livePathInputPrompt(resolveEffective(stor.LivePath, cfgStor.LivePath, cfgStor.LivePath), flagLivePath),
		logPathInputPrompt(resolveEffective(stor.LogPath, cfgStor.LogPath, cfgStor.LogPath), flagLogPath),
		verificationPathInputPrompt(resolveEffective(stor.VerificationPath, cfgStor.VerificationPath, cfgStor.VerificationPath), flagVerificationPath),
		pluginsPathInputPrompt(resolveEffective(stor.PluginsPath, cfgStor.PluginsPath, cfgStor.PluginsPath), flagPluginsPath),
		historicRetentionInputPrompt(resolveEffective(defaults.BlockNode.HistoricRetention, cfg.BlockNode.HistoricRetention, models.DefaultHistoricRetention), flagHistoricRetention),
		recentRetentionInputPrompt(resolveEffective(defaults.BlockNode.RecentRetention, cfg.BlockNode.RecentRetention, models.DefaultRecentRetention), flagRecentRetention),
	}
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
