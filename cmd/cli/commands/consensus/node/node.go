// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var (
	flagNamespace        string
	flagNodeId           int64
	flagAccountId        string
	flagWeight           int
	flagLedgerId         string
	flagChainId          string
	flagImageRepo        string
	flagImageTag         string
	flagDeploymentPkgDir string
	flagGrpcTlsSecret    string
	flagSigningSecret    string
	flagProfile          string
	flagContainerName    string
	flagJavaHeapMin      string
	flagJavaHeapMax      string
	flagJavaOpts         string
	flagCPULimit         string
	flagCPURequest       string
	flagMemoryLimit      string
	flagMemoryRequest    string

	nodeCmd = &cobra.Command{
		Use:   "node",
		Short: "Manage consensus node lifecycle",
		Long:  "Deploy and manage Hedera consensus nodes via the solo-operator's ConsensusCapsule CRD",
		RunE:  common.DefaultRunE,
	}
)

func init() {
	nodeCmd.PersistentFlags().StringVar(&flagNamespace, "namespace", "hiero-network-1", "Kubernetes namespace (also used as the Orbit CR name). Deploy multiple networks in one cluster by using a distinct namespace per orbit (hiero-network-1, hiero-network-2, ...)")
	nodeCmd.PersistentFlags().Int64Var(&flagNodeId, "node-id", 0, "Consensus node ID (0-based)")
	nodeCmd.PersistentFlags().StringVar(&flagAccountId, "account-id", "0.0.3", "Node account ID (e.g. 0.0.3)")
	nodeCmd.PersistentFlags().IntVar(&flagWeight, "weight", 500, "Consensus weight for this node")
	nodeCmd.PersistentFlags().StringVar(&flagLedgerId, "ledger-id", "", "Hex ledger identity (e.g. 0x00 for mainnet, 0x01 for local/dev) — extracted from deployment package if not set")
	nodeCmd.PersistentFlags().StringVar(&flagChainId, "chain-id", "", "Decimal EVM chain ID (e.g. 295 for mainnet, 298 for local/dev) — extracted from deployment package if not set")
	nodeCmd.PersistentFlags().StringVar(&flagImageRepo, "image-repo", "", "Consensus node container image repository — extracted from deployment package if not set")
	nodeCmd.PersistentFlags().StringVar(&flagImageTag, "image-tag", "", "Consensus node container image tag — extracted from deployment package if not set")
	nodeCmd.PersistentFlags().StringVar(&flagDeploymentPkgDir, "deployment-package-dir", "", "Path to extracted HIP-1494 deployment package — config files at well-known paths override embedded defaults")
	nodeCmd.PersistentFlags().StringVar(&flagGrpcTlsSecret, "grpc-tls-secret", "", "Name of K8s Secret containing gRPC TLS key/cert (keys: hedera-node<N>.key, hedera-node<N>.crt)")
	nodeCmd.PersistentFlags().StringVar(&flagSigningSecret, "signing-secret", "", "Name of K8s Secret containing gossip signing key/cert (keys: private.pem, public.pem)")

	// Consensus-node container sizing + JVM tuning. Defaults are a working baseline;
	// the explicit Java heap is required or the node stalls on startup.
	nodeCmd.PersistentFlags().StringVar(&flagContainerName, "container-name", models.ConsensusDefaultContainerName, "Consensus-node container name")
	nodeCmd.PersistentFlags().StringVar(&flagJavaHeapMin, "java-heap-min", models.ConsensusDefaultJavaHeapMin, "JVM minimum heap (-Xms) for the consensus node")
	nodeCmd.PersistentFlags().StringVar(&flagJavaHeapMax, "java-heap-max", models.ConsensusDefaultJavaHeapMax, "JVM maximum heap (-Xmx) for the consensus node")
	nodeCmd.PersistentFlags().StringVar(&flagJavaOpts, "java-opts", models.ConsensusDefaultJavaOpts, "Additional JVM options for the consensus node")
	nodeCmd.PersistentFlags().StringVar(&flagCPULimit, "cpu-limit", models.ConsensusDefaultCPULimit, "Consensus-node CPU limit (Kubernetes quantity, e.g. 2)")
	nodeCmd.PersistentFlags().StringVar(&flagCPURequest, "cpu-request", models.ConsensusDefaultCPURequest, "Consensus-node CPU request (e.g. 250m)")
	nodeCmd.PersistentFlags().StringVar(&flagMemoryLimit, "memory-limit", models.ConsensusDefaultMemoryLimit, "Consensus-node memory limit (e.g. 5Gi)")
	nodeCmd.PersistentFlags().StringVar(&flagMemoryRequest, "memory-request", models.ConsensusDefaultMemoryRequest, "Consensus-node memory request (e.g. 1Gi)")

	// flagProfile is a binding target for Cobra; the install command reads the value via FlagProfile().Value()
	common.FlagProfile().SetVarP(nodeCmd, &flagProfile, false)
	nodeCmd.AddCommand(installCmd)
}

// GetCmd returns the consensus node command group.
func GetCmd() *cobra.Command {
	return nodeCmd
}
