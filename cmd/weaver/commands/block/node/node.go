// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/spf13/cobra"
)

var (
	nodeType = core.NodeTypeBlock

	flagStopOnError     bool
	flagRollbackOnError bool
	flagContinueOnError bool

	flagValuesFile       string
	flagChartVersion     string
	flagChartRepo        string
	flagNamespace        string
	flagReleaseName      string
	flagBasePath         string
	flagArchivePath      string
	flagLivePath         string
	flagLogPath          string
	flagVerificationPath string
	flagLiveSize         string
	flagArchiveSize      string
	flagLogSize          string
	flagVerificationSize string

	nodeCmd = &cobra.Command{
		Use:   "node",
		Short: "Manage lifecycle of a Hedera Block Node",
		Long:  "Manage lifecycle of a Hedera Block Node",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable
	}
)

func init() {
	// Helm chart configuration flags
	nodeCmd.PersistentFlags().StringVar(&flagChartVersion, "chart-version", "", "Helm chart version to use")
	nodeCmd.PersistentFlags().StringVar(&flagChartRepo, "chart-repo", "", "Helm chart repository URL")
	nodeCmd.PersistentFlags().StringVar(&flagNamespace, "namespace", "", "Kubernetes namespace for block node")
	nodeCmd.PersistentFlags().StringVar(&flagReleaseName, "release-name", "", "Helm release name")

	// Storage path configuration flags
	nodeCmd.PersistentFlags().StringVar(&flagBasePath, "base-path", "", "Base path for all storage (used when individual paths are not specified)")
	nodeCmd.PersistentFlags().StringVar(&flagArchivePath, "archive-path", "", "Path for archive storage")
	nodeCmd.PersistentFlags().StringVar(&flagLivePath, "live-path", "", "Path for live storage")
	nodeCmd.PersistentFlags().StringVar(&flagLogPath, "log-path", "", "Path for log storage")
	nodeCmd.PersistentFlags().StringVar(&flagVerificationPath, "verification-path", "", "Path for verification storage")

	// Storage size configuration flags
	nodeCmd.PersistentFlags().StringVar(&flagLiveSize, "live-size", "", "Size for live storage PV/PVC (e.g., 5Gi, 10Gi)")
	nodeCmd.PersistentFlags().StringVar(&flagArchiveSize, "archive-size", "", "Size for archive storage PV/PVC (e.g., 5Gi, 10Gi)")
	nodeCmd.PersistentFlags().StringVar(&flagLogSize, "log-size", "", "Size for log storage PV/PVC (e.g., 5Gi, 10Gi)")
	nodeCmd.PersistentFlags().StringVar(&flagVerificationSize, "verification-size", "", "Size for verification storage PV/PVC (e.g., 5Gi, 10Gi)")

	nodeCmd.AddCommand(checkCmd, installCmd, upgradeCmd, resetCmd)
}

func GetCmd() *cobra.Command {
	return nodeCmd
}
