// SPDX-License-Identifier: Apache-2.0

package node

import (
	"time"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
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
	flagUpgradeOperator  bool
	flagProfile          string
	flagReadyTimeout     time.Duration

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
	nodeCmd.PersistentFlags().BoolVar(&flagUpgradeOperator, "upgrade-operator", false, "Upgrade solo-operator if installed version differs from the expected version")
	nodeCmd.PersistentFlags().DurationVar(&flagReadyTimeout, "ready-timeout", 5*time.Minute, "How long to wait for the consensus node to become Running/Active after the CR is created (0 disables the wait)")
	// flagProfile is a binding target for Cobra; the install command reads the value via FlagProfile().Value()
	common.FlagProfile().SetVarP(nodeCmd, &flagProfile, false)
	nodeCmd.AddCommand(installCmd)
}

// GetCmd returns the consensus node command group.
func GetCmd() *cobra.Command {
	return nodeCmd
}
