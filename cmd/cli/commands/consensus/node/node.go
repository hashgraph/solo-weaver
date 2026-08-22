// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var (
	flagNamespace    string
	flagOrbitName    string
	flagNodeId       int64
	flagAccountId    string
	flagWeight       int
	flagLedgerId     string
	flagChainId      string
	flagImageRepo    string
	flagImageTag     string
	flagWithProxy    string
	flagLog4j2File      string
	flagSettingsFile    string
	flagAppPropsFile    string
	flagGrpcTlsSecret   string
	flagSigningSecret   string
	flagHapiAppSecret   string
	flagUpgradeOperator bool

	nodeCmd = &cobra.Command{
		Use:   "node",
		Short: "Manage consensus node lifecycle",
		Long:  "Deploy and manage Hedera consensus nodes via the solo-operator's ConsensusCapsule CRD",
		RunE:  common.DefaultRunE,
	}
)

func init() {
	nodeCmd.PersistentFlags().StringVar(&flagNamespace, "namespace", "solo-orbit", "Kubernetes namespace for the consensus node")
	nodeCmd.PersistentFlags().StringVar(&flagOrbitName, "orbit", "solo-orbit", "Name of the Orbit CR")
	nodeCmd.PersistentFlags().Int64Var(&flagNodeId, "node-id", 0, "Consensus node ID (0-based)")
	nodeCmd.PersistentFlags().StringVar(&flagAccountId, "account-id", "0.0.3", "Node account ID (e.g. 0.0.3)")
	nodeCmd.PersistentFlags().IntVar(&flagWeight, "weight", 500, "Consensus weight for this node")
	nodeCmd.PersistentFlags().StringVar(&flagLedgerId, "ledger-id", "0x01", "Hex ledger identity (e.g. 0x00 for mainnet, 0x01 for local/dev)")
	nodeCmd.PersistentFlags().StringVar(&flagChainId, "chain-id", "298", "Decimal EVM chain ID (e.g. 295 for mainnet, 298 for local/dev)")
	nodeCmd.PersistentFlags().StringVar(&flagImageRepo, "image-repo", "gcr.io/hedera-registry", "Consensus node container image repository")
	nodeCmd.PersistentFlags().StringVar(&flagImageTag, "image-tag", "0.74.0", "Consensus node container image tag")
	nodeCmd.PersistentFlags().StringVar(&flagWithProxy, "with-proxy", "", "Also create a proxy CR (haproxy or envoy)")
	nodeCmd.PersistentFlags().StringVar(&flagLog4j2File, "log4j2-config-file", "", "Path to custom log4j2.xml (default: built-in)")
	nodeCmd.PersistentFlags().StringVar(&flagSettingsFile, "settings-file", "", "Path to custom settings.txt (default: built-in)")
	nodeCmd.PersistentFlags().StringVar(&flagAppPropsFile, "application-properties-file", "", "Path to custom application.properties (default: built-in)")
	nodeCmd.PersistentFlags().StringVar(&flagGrpcTlsSecret, "grpc-tls-secret", "", "Name of K8s Secret containing gRPC TLS key/cert (keys: hedera-node<N>.key, hedera-node<N>.crt)")
	nodeCmd.PersistentFlags().StringVar(&flagSigningSecret, "signing-secret", "", "Name of K8s Secret containing gossip signing key/cert (keys: private.pem, public.pem)")
	nodeCmd.PersistentFlags().StringVar(&flagHapiAppSecret, "hapi-app-secret", "", "Name of K8s Secret containing hedera.crt and hedera.key for HAPI")
	nodeCmd.PersistentFlags().BoolVar(&flagUpgradeOperator, "upgrade-operator", false, "Upgrade solo-operator if installed version differs from the expected version")
	nodeCmd.AddCommand(installCmd)
}

// GetCmd returns the consensus node command group.
func GetCmd() *cobra.Command {
	return nodeCmd
}
