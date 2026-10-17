// SPDX-License-Identifier: Apache-2.0

// Package keys implements the `consensus keys` command family: generate, import,
// export, verify, and init the cryptographic material a consensus node needs, and
// materialize it as the Kubernetes secrets the solo-operator consumes. This file
// wires the command group; each verb lives in its own file.
package keys

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var keysCmd = &cobra.Command{
	Use:   "keys",
	Short: "Generate, import, verify, and export consensus node key material",
	Long: "Manage the cryptographic material a consensus node needs — the gossip " +
		"signing key (its network identity), the gRPC TLS key, and the node admin " +
		"key — and turn it into the Kubernetes secrets the solo-operator consumes.\n\n" +
		"Key types are selected with --gossip / --tls / --admin; when none is given, " +
		"all apply.",
	RunE: common.DefaultRunE,
}

func init() {
	// Sibling of `consensus node`, so it does not inherit the node install flags.
	keysCmd.PersistentFlags().String("namespace", models.ConsensusDefaultNamespace, "Kubernetes namespace (the Orbit CR name) holding the node's secrets")
	keysCmd.PersistentFlags().Int64("node-id", 0, "Consensus node ID (0-based)")

	keysCmd.AddCommand(generateCmd)
}

// GetCmd returns the consensus keys command group.
func GetCmd() *cobra.Command {
	return keysCmd
}
