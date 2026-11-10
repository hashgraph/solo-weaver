// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var (
	flagNamespace         string
	flagNodeId            int64
	flagAccountId         string
	flagWeight            int
	flagLedgerId          string
	flagChainId           string
	flagImageRepo         string
	flagImageTag          string
	flagUCImageRepo       string
	flagUCImageTag        string
	flagDeploymentPkgDir  string
	flagGrpcTlsSecret     string
	flagSigningSecret     string
	flagImagePullSecret   []string
	flagRegistryStrategy  string
	flagProfile           string
	flagContainerName     string
	flagJavaHeapMin       string
	flagJavaHeapMax       string
	flagJavaOpts          string
	flagCPULimit          string
	flagCPURequest        string
	flagMemoryLimit       string
	flagMemoryRequest     string
	flagProvisionerDaemon bool

	// Volume backing configuration.
	flagDefaultVolumeType      string
	flagDefaultPVCSize         string
	flagDefaultPVCStorageClass string
	flagDefaultPVCAccessMode   string
	flagVolumes                []string
	flagVolumesFile            string
	flagHostPathUID            int
	flagHostPathGID            int

	nodeCmd = &cobra.Command{
		Use:   "node",
		Short: "Manage consensus node lifecycle",
		Long:  "Deploy and manage Hedera consensus nodes via the solo-operator's ConsensusCapsule CRD",
		RunE:  common.DefaultRunE,
	}
)

func init() {
	// Only flags that every subcommand uses live here; install-only flags are on installCmd.
	nodeCmd.PersistentFlags().StringVar(&flagNamespace, "namespace", models.ConsensusDefaultNamespace, "Kubernetes namespace (also used as the Orbit CR name). Deploy multiple networks in one cluster by using a distinct namespace per orbit (hiero-network-1, hiero-network-2, ...) (alias: --orbit)")
	// --orbit is a hidden alias for --namespace (the namespace IS the Orbit name),
	// bound to the same variable so either spelling works; keep the default identical.
	nodeCmd.PersistentFlags().StringVar(&flagNamespace, "orbit", models.ConsensusDefaultNamespace, "Alias for --namespace (the Orbit CR name)")
	_ = nodeCmd.PersistentFlags().MarkHidden("orbit")
	nodeCmd.PersistentFlags().Int64Var(&flagNodeId, "node-id", 0, "Consensus node ID (0-based)")
	nodeCmd.AddCommand(installCmd)
	nodeCmd.AddCommand(uninstallCmd)
}

// GetCmd returns the consensus node command group.
func GetCmd() *cobra.Command {
	return nodeCmd
}
