// SPDX-License-Identifier: Apache-2.0

package network

import (
	"time"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var (
	flagNamespace    string
	flagPkgDir       string
	flagGenesisFile  string
	flagReadyTimeout time.Duration

	networkCmd = &cobra.Command{
		Use:   "network",
		Short: "Manage consensus network-level operations",
		Long:  "Orbit/network-level operations such as generating the network genesis for a fresh consensus network.",
		RunE:  common.DefaultRunE,
	}
)

func init() {
	networkCmd.PersistentFlags().StringVar(&flagNamespace, "namespace", "hiero-network-1", "Kubernetes namespace / Orbit name of the network")
	networkCmd.PersistentFlags().StringVar(&flagPkgDir, "deployment-package-dir", "", "Path to an extracted HIP-1494 deployment package; its genesis-network.json is applied as the pre-built genesis (omit to discover the roster from the cluster)")
	networkCmd.PersistentFlags().StringVar(&flagGenesisFile, "genesis-file", "", "Path to a genesis-network.json to apply verbatim; overrides the deployment package's genesis-network.json when both are set")
	networkCmd.PersistentFlags().DurationVar(&flagReadyTimeout, "ready-timeout", 5*time.Minute, "How long to wait for the operator to generate the genesis ConfigMap (0 disables the wait)")
	networkCmd.AddCommand(genesisCmd)
}

// GetCmd returns the consensus network command group.
func GetCmd() *cobra.Command {
	return networkCmd
}
