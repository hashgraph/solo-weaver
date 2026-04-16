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

// BlockNodeInputPrompts returns the text-input-type prompts for block node commands.
// The defaults parameter provides persisted state values read once by the caller;
// effective values are resolved using the priority: persisted state > config > default.
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

	effNamespace := resolveEffective(summary.Namespace, cfg.BlockNode.Namespace, cfg.BlockNode.Namespace)
	effReleaseName := resolveEffective(summary.ReleaseName, cfg.BlockNode.Release, cfg.BlockNode.Release)
	effChartVersion := resolveEffective(summary.ChartVersion, cfg.BlockNode.ChartVersion, cfg.BlockNode.ChartVersion)
	effHistoricRetention := resolveEffective(summary.HistoricRetention, cfg.BlockNode.HistoricRetention, models.DefaultHistoricRetention)
	effRecentRetention := resolveEffective(summary.RecentRetention, cfg.BlockNode.RecentRetention, models.DefaultRecentRetention)

	return []InputPrompt{
		{
			FlagName:       "namespace",
			Title:          "Kubernetes Namespace",
			Description:    "Namespace for the block node Helm release",
			Placeholder:    effNamespace,
			EffectiveValue: effNamespace,
			Target:         flagNamespace,
			Validate: func(s string) error {
				if s == "" {
					return fmt.Errorf("namespace cannot be empty")
				}
				return sanity.ValidateIdentifier(s)
			},
		},
		{
			FlagName:       "release-name",
			Title:          "Helm Release Name",
			Description:    "Name for the Helm release",
			Placeholder:    effReleaseName,
			EffectiveValue: effReleaseName,
			Target:         flagReleaseName,
			Validate: func(s string) error {
				if s == "" {
					return fmt.Errorf("release name cannot be empty")
				}
				return sanity.ValidateIdentifier(s)
			},
		},
		{
			FlagName:       "chart-version",
			Title:          "Chart Version",
			Description:    "Helm chart version to deploy",
			Placeholder:    effChartVersion,
			EffectiveValue: effChartVersion,
			Target:         flagChartVersion,
			Validate: func(s string) error {
				if s == "" {
					return fmt.Errorf("chart version cannot be empty")
				}
				return sanity.ValidateVersion(s)
			},
		},
		{
			FlagName:       "historic-retention",
			Title:          "Historic Block Retention Threshold",
			Description:    "Number of blocks to preserve in historic storage (0 = unlimited)",
			Placeholder:    effHistoricRetention,
			EffectiveValue: effHistoricRetention,
			Target:         flagHistoricRetention,
			Validate:       validateRetentionThreshold,
		},
		{
			FlagName:       "recent-retention",
			Title:          "Recent Block Retention Threshold",
			Description:    "Number of blocks to preserve in recent storage before deleting older files",
			Placeholder:    effRecentRetention,
			EffectiveValue: effRecentRetention,
			Target:         flagRecentRetention,
			Validate:       validateRetentionThreshold,
		},
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
