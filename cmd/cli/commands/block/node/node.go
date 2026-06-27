// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var (
	nodeType = models.NodeTypeBlock

	flagStopOnError     bool
	flagRollbackOnError bool
	flagContinueOnError bool

	flagValuesFile           string
	flagChartVersion         string
	flagChartRepo            string
	flagNamespace            string
	flagReleaseName          string
	flagBasePath             string
	flagArchivePath          string
	flagLivePath             string
	flagLogPath              string
	flagVerificationPath     string
	flagPluginsPath          string
	flagApplicationStatePath string
	flagLiveSize             string
	flagArchiveSize          string
	flagLogSize              string
	flagVerificationSize     string
	flagPluginsSize          string
	flagApplicationStateSize string
	flagHistoricRetention    string
	flagRecentRetention      string
	flagPluginPreset         string
	flagPlugins              string
	flagLoadBalancerEnabled  bool

	nodeCmd = &cobra.Command{
		Use:   "node",
		Short: "Manage lifecycle of a Hedera Block Node",
		Long:  "Manage lifecycle of a Hedera Block Node",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
	}
)

// BlockNodeFlags contains the root-level flags plus the profile flag
// required by all block node subcommands (install, upgrade, reset, uninstall, check).
type BlockNodeFlags struct {
	common.RootFlags
	Profile string
}

func init() {
	common.FlagStopOnError().SetVarP(nodeCmd, &flagStopOnError, false)
	common.FlagRollbackOnError().SetVarP(nodeCmd, &flagRollbackOnError, false)
	common.FlagContinueOnError().SetVarP(nodeCmd, &flagContinueOnError, false)
	nodeCmd.MarkFlagsMutuallyExclusive(
		common.FlagStopOnError().Name,
		common.FlagContinueOnError().Name,
		common.FlagRollbackOnError().Name,
	)

	// Helm chart configuration flags
	common.FlagChartRepo().SetVarP(nodeCmd, &flagChartRepo, false)
	common.FlagNamespace().SetVarP(nodeCmd, &flagNamespace, false)
	common.FlagReleaseName().SetVarP(nodeCmd, &flagReleaseName, false)

	// Storage path configuration flags
	common.FlagBasePath().SetVarP(nodeCmd, &flagBasePath, false)
	common.FlagArchivePath().SetVarP(nodeCmd, &flagArchivePath, false)
	common.FlagLivePath().SetVarP(nodeCmd, &flagLivePath, false)
	common.FlagLogPath().SetVarP(nodeCmd, &flagLogPath, false)
	common.FlagVerificationPath().SetVarP(nodeCmd, &flagVerificationPath, false)
	common.FlagPluginsPath().SetVarP(nodeCmd, &flagPluginsPath, false)
	common.FlagApplicationStatePath().SetVarP(nodeCmd, &flagApplicationStatePath, false)

	// Storage size configuration flags
	common.FlagLiveSize().SetVarP(nodeCmd, &flagLiveSize, false)
	common.FlagArchiveSize().SetVarP(nodeCmd, &flagArchiveSize, false)
	common.FlagLogSize().SetVarP(nodeCmd, &flagLogSize, false)
	common.FlagVerificationSize().SetVarP(nodeCmd, &flagVerificationSize, false)
	common.FlagPluginsSize().SetVarP(nodeCmd, &flagPluginsSize, false)
	common.FlagApplicationStateSize().SetVarP(nodeCmd, &flagApplicationStateSize, false)

	// Block retention configuration flags
	common.FlagHistoricRetention().SetVarP(nodeCmd, &flagHistoricRetention, false)
	common.FlagRecentRetention().SetVarP(nodeCmd, &flagRecentRetention, false)

	// Plugin configuration flags
	common.FlagPluginPreset().SetVarP(nodeCmd, &flagPluginPreset, false)
	common.FlagPlugins().SetVarP(nodeCmd, &flagPlugins, false)

	// LoadBalancer / MetalLB annotation flag
	common.FlagLoadBalancerEnabled().SetVarP(nodeCmd, &flagLoadBalancerEnabled, false)

	nodeCmd.AddCommand(checkCmd, installCmd, upgradeCmd, reconfigureCmd, resetCmd, uninstallCmd)
}

func GetCmd() *cobra.Command {
	return nodeCmd
}
