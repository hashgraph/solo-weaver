// SPDX-License-Identifier: Apache-2.0

package common

// Flag descriptor factories for the `block node` command tree.
//
// Each factory returns a fresh FlagDefinition value so callers always receive an
// independent copy. Call sites register these against nodeCmd in
// cmd/cli/commands/block/node/node.go via SetVarP and read them by referencing
// the package-level flagXxx variables.

func FlagChartRepo() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "chart-repo",
		ShortName:   "",
		Description: "Helm chart repository URL",
		Default:     "",
	}
}

func FlagNamespace() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "namespace",
		ShortName:   "",
		Description: "Kubernetes namespace for block node",
		Default:     "",
	}
}

func FlagReleaseName() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "release-name",
		ShortName:   "",
		Description: "Helm release name",
		Default:     "",
	}
}

func FlagBasePath() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "base-path",
		ShortName:   "",
		Description: "Base path for all storage (used when individual paths are not specified)",
		Default:     "",
	}
}

func FlagArchivePath() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "archive-path",
		ShortName:   "",
		Description: "Path for archive storage",
		Default:     "",
	}
}

func FlagLivePath() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "live-path",
		ShortName:   "",
		Description: "Path for live storage",
		Default:     "",
	}
}

func FlagLogPath() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "log-path",
		ShortName:   "",
		Description: "Path for log storage",
		Default:     "",
	}
}

func FlagVerificationPath() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "verification-path",
		ShortName:   "",
		Description: "Path for verification storage (chart versions below 0.37.0)",
		Default:     "",
	}
}

func FlagPluginsPath() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "plugins-path",
		ShortName:   "",
		Description: "Path for plugins storage",
		Default:     "",
	}
}

func FlagApplicationStatePath() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "application-state-path",
		ShortName:   "",
		Description: "Path for application-state storage (chart versions 0.37.0 and above)",
		Default:     "",
	}
}

func FlagLiveSize() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "live-size",
		ShortName:   "",
		Description: "Size for live storage PV/PVC (e.g., 5Gi, 10Gi)",
		Default:     "",
	}
}

func FlagArchiveSize() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "archive-size",
		ShortName:   "",
		Description: "Size for archive storage PV/PVC (e.g., 5Gi, 10Gi)",
		Default:     "",
	}
}

func FlagLogSize() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "log-size",
		ShortName:   "",
		Description: "Size for log storage PV/PVC (e.g., 5Gi, 10Gi)",
		Default:     "",
	}
}

func FlagVerificationSize() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "verification-size",
		ShortName:   "",
		Description: "Size for verification storage PV/PVC, e.g., 5Gi, 10Gi (chart versions below 0.37.0)",
		Default:     "",
	}
}

func FlagPluginsSize() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "plugins-size",
		ShortName:   "",
		Description: "Size for plugins storage PV/PVC (e.g., 5Gi, 10Gi)",
		Default:     "",
	}
}

func FlagApplicationStateSize() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "application-state-size",
		ShortName:   "",
		Description: "Size for application-state storage PV/PVC, e.g., 500Mi, 1Gi (chart versions 0.37.0 and above)",
		Default:     "",
	}
}

func FlagHistoricRetention() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "historic-retention",
		ShortName:   "",
		Description: "Historic block retention threshold (0 = unlimited, default: 0)",
		Default:     "",
	}
}

func FlagRecentRetention() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "recent-retention",
		ShortName:   "",
		Description: "Recent block retention threshold (default: 96000)",
		Default:     "",
	}
}

func FlagPluginPreset() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "plugin-preset",
		ShortName:   "",
		Description: "Plugin preset to deploy (tier1-lfh, tier1-rfh, custom, or none for no override — use --values/chart default); prompts interactively when omitted",
		Default:     "",
	}
}

func FlagPlugins() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "plugins",
		ShortName:   "",
		Description: "Comma-separated plugin list; overrides --plugin-preset when set",
		Default:     "",
	}
}

func FlagLoadBalancerEnabled() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "load-balancer-enabled",
		ShortName:   "",
		Description: "Inject MetalLB address-pool annotation into the block node service (disable for environments without MetalLB)",
		Default:     true,
	}
}
