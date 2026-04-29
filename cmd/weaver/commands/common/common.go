// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

// Flag descriptor factories.
//
// Each function returns a fresh FlagDefinition value so callers always receive an
// independent copy. Direct mutation of the descriptor (e.g. FlagConfig().ShortName = "z")
// is harmless because it only affects the caller's local copy; the next call to
// FlagConfig() returns a pristine value.
//
// Call sites:
//
//	common.FlagConfig().SetVarP(cmd, &flagConfig, false)
//	val, err := common.FlagConfig().Value(cmd, args)
//	flagName := common.FlagConfig().Name

func FlagConfig() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "config",
		ShortName:   "c",
		Description: "Path to config file",
		Default:     "",
	}
}

func FlagVersion() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "version",
		ShortName:   "v",
		Description: "Print version and exit",
		Default:     false,
	}
}

func FlagOutputFormat() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "output",
		ShortName:   "o",
		Description: "Output format (json, yaml)",
		Default:     "json",
	}
}

func FlagNodeType() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "node-type",
		ShortName:   "n",
		Description: fmt.Sprintf("Type of node to deploy %s", []string{models.NodeTypeBlock, models.NodeTypeMirror, models.NodeTypeConsensus}),
		Default:     models.NodeTypeBlock,
	}
}

func FlagStopOnError() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "stop-on-error",
		ShortName:   "",
		Description: "Stop execution on first error (default behaviour when no execution-mode flag is set)",
		Default:     false,
	}
}

func FlagRollbackOnError() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "rollback-on-error",
		ShortName:   "",
		Description: "Rollback executed steps on error",
		Default:     false,
	}
}

func FlagContinueOnError() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "continue-on-error",
		ShortName:   "",
		Description: "Continue executing steps even if some steps fail",
		Default:     false,
	}
}

func FlagProfile() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "profile",
		ShortName:   "p",
		Description: fmt.Sprintf("Deployment profiles %s", models.SupportedProfiles()),
		Default:     "",
	}
}

func FlagValuesFile() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "values",
		ShortName:   "f",
		Description: "Path to custom values file for chart",
		Default:     "",
	}
}

func FlagMetricsServer() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "metrics-server",
		ShortName:   "m",
		Description: "Install Metrics Server",
		Default:     true,
	}
}

func FlagWithStorageReset() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "with-reset",
		ShortName:   "",
		Description: "Reset storage before upgrading (clears all data)",
		Default:     false,
	}
}

func FlagNoReuseValues() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "no-reuse-values",
		ShortName:   "",
		Description: "Do not reuse values from previous installations (resets to chart defaults)",
	}
}

// FlagSkipHardwareChecks is a hidden persistent flag registered on the root command.
// When set, it skips CPU, memory, and storage validation in NewNodeSafetyCheckWorkflow
// (see internal/workflows/preflight.go). Privilege, user, and host profile checks
// still run. This flag is intentionally not supported by the "check" command since its
// purpose is to validate hardware requirements.
//
// Used by: block node install, kube cluster install.
// Registered in: cmd/weaver/commands/root.go (hidden).
// See docs/dev/hidden-flags.md for full documentation.
func FlagSkipHardwareChecks() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "skip-hardware-checks",
		ShortName:   "",
		Description: "DANGEROUS: Skip hardware validation checks. May cause node instability or data loss.",
		Default:     false,
	}
}

func FlagForce() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "force",
		ShortName:   "y",
		Description: fmt.Sprintf("Force override or skip prompts where applicable"),
		Default:     false,
	}
}

func FlagLogLevel() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "log-level",
		ShortName:   "",
		Description: "Set log level (debug, info, warn, error)",
		Default:     "",
	}
}

func FlagNonInteractive() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "non-interactive",
		ShortName:   "",
		Description: "Disable TUI and output raw logs (for CI/pipelines)",
		Default:     false,
	}
}

func FlagVerbose() CountFlagDefinition {
	return CountFlagDefinition{
		Name:        "verbose",
		ShortName:   "V",
		Description: "Show expanded step-by-step output",
	}
}
