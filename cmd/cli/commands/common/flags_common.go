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
		Description: "Output format (text, json)",
		Default:     "text",
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

func FlagESONamespace() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "namespace",
		ShortName:   "",
		Description: "Kubernetes namespace for the External Secrets Operator",
		Default:     "external-secrets",
	}
}

func FlagESOChartVersion() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "chart-version",
		ShortName:   "",
		Description: "External Secrets Operator chart version to install (must be declared in the infrastructure catalog; defaults to the catalog default)",
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
		Description: "Wipe block node data directories; PVs and PVCs are preserved",
		Default:     false,
	}
}

func FlagPurgeStorage() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "purge-storage",
		ShortName:   "",
		Description: "Delete PersistentVolumes and PersistentVolumeClaims in addition to wiping data (implies --with-reset)",
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

func FlagNoRestart() FlagDefinition[bool] {
	return FlagDefinition[bool]{
		Name:        "no-restart",
		ShortName:   "",
		Description: "Skip the rollout-restart of the block node pod after reconfiguring (use when the chart guarantees pod-spec changes trigger restarts automatically)",
		Default:     false,
	}
}

// FlagSkipHardwareChecks is a hidden persistent flag registered on the root command.
// When set, it skips CPU, memory, and storage validation in NewNodeSafetyCheckWorkflow
// (see internal/workflows/preflight.go). Privilege, user, and host profile checks
// still run. This flag is intentionally not supported by the "check" command since its
// purpose is to validate hardware requirements.
//
// Used by: block node install, kube cluster install.
// Registered in: cmd/cli/commands/root.go (hidden).
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

// ── Daemon service install flags ─────────────────────────────────────────────

// FlagDaemonComponents selects which daemon components to enable.
// Accepts a comma-separated list of component names: "consensus-node", "block-node".
// When omitted the operator is prompted interactively.
func FlagDaemonComponents() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "components",
		ShortName:   "",
		Description: `Comma-separated list of daemon components to enable (choices: "consensus-node", "block-node")`,
		Default:     "",
	}
}

// FlagDaemonCNNodeID is the numeric node identifier written into daemon.yaml as
// the consensus-node node_id. Required when daemon.yaml does not already exist
// and consensus-node is enabled.
func FlagDaemonCNNodeID() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "cn-node-id",
		ShortName:   "",
		Description: `Numeric node identifier for the consensus node (e.g. "0", "1", "2")`,
		Default:     "",
	}
}

// FlagDaemonCNOrbit is the Kubernetes namespace (orbit) for the consensus-node
// component written into daemon.yaml. Required when daemon.yaml does not already
// exist and consensus-node is enabled.
func FlagDaemonCNOrbit() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "cn-orbit",
		ShortName:   "",
		Description: "Kubernetes namespace (orbit) where consensus-node NetworkUpgradeExecute CRs are watched",
		Default:     "",
	}
}

// FlagDaemonCNUpgradeDir is an optional override for the consensus-node upgrade
// staging directory.
// Defaults to /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current.
func FlagDaemonCNUpgradeDir() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "cn-upgrade-dir",
		ShortName:   "",
		Description: "Path to the consensus-node upgrade staging directory (default: /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current)",
		Default:     "",
	}
}

// FlagDaemonBNOrbit is the Kubernetes namespace (orbit) for the block-node
// component written into daemon.yaml. Required when daemon.yaml does not already
// exist and block-node is enabled.
func FlagDaemonBNOrbit() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "bn-orbit",
		ShortName:   "",
		Description: "Kubernetes namespace (orbit) for the block-node component",
		Default:     "",
	}
}

// FlagDaemonFromConfig is an optional path to an existing daemon.yaml to copy
// into place before running the install workflow.
func FlagDaemonFromConfig() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "from-config",
		ShortName:   "",
		Description: "Path to an existing daemon.yaml to copy into /opt/solo/weaver/config/daemon.yaml",
		Default:     "",
	}
}

// FlagDaemonBin is the optional path to a locally-built solo-provisioner-daemon
// binary. When omitted the binary is auto-downloaded from the official GitHub
// Releases URL embedded in the infrastructure catalog.
func FlagDaemonBin() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "daemon-bin",
		ShortName:   "",
		Description: "Path to a locally-built solo-provisioner-daemon binary to install; omit to auto-download from the embedded catalog",
		Default:     "",
	}
}

// FlagDaemonChecksum is an optional sha256 hex digest used to verify a binary
// supplied via --daemon-bin. Ignored when --daemon-bin is not set (the catalog
// checksum is used for auto-downloaded binaries).
func FlagDaemonChecksum() FlagDefinition[string] {
	return FlagDefinition[string]{
		Name:        "daemon-checksum",
		ShortName:   "",
		Description: "SHA-256 hex digest of the binary supplied via --daemon-bin (e.g. the value from a .sha256 release asset)",
		Default:     "",
	}
}
