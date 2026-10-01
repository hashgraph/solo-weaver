// SPDX-License-Identifier: Apache-2.0

// Package keys implements the `consensus keys` command family: generate, import,
// export, verify, and init the cryptographic material a consensus node needs, and
// materialize it as the Kubernetes secrets the solo-operator consumes. This file
// wires the command group; each verb lives in its own file.
package keys

import (
	"context"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// newSecretClient is indirected through a var so command tests can stub cluster access.
var newSecretClient = func(_ context.Context) (cnkeys.SecretClient, error) {
	return kube.NewClient()
}

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
	keysCmd.AddCommand(importCmd)
	keysCmd.AddCommand(exportCmd)
}

// GetCmd returns the consensus keys command group.
func GetCmd() *cobra.Command {
	return keysCmd
}

// addSelectorFlags registers the --gossip/--tls/--admin selectors on cmd and
// returns a reader. Fresh bools per command avoid shared-var default clobbering.
func addSelectorFlags(cmd *cobra.Command) func() cnkeys.Selectors {
	g := cmd.Flags().Bool("gossip", false, "Select the gossip signing key (default: all types when no selector is given)")
	t := cmd.Flags().Bool("tls", false, "Select the gRPC TLS key (default: all types when no selector is given)")
	a := cmd.Flags().Bool("admin", false, "Select the node admin key (default: all types when no selector is given)")
	return func() cnkeys.Selectors {
		return cnkeys.Selectors{Gossip: *g, Grpc: *t, Admin: *a}
	}
}

// requireNodeID reads --node-id, requiring it to be explicitly set (0 is valid).
func requireNodeID(cmd *cobra.Command) (int64, error) {
	if !cmd.Flags().Changed("node-id") {
		return 0, errx.Decorate(
			errorx.IllegalArgument.New("--node-id is required"),
			reasons.InvalidArgument,
			"pass --node-id <N> (0-based); it selects the per-node secrets to write or read")
	}
	id, err := cmd.Flags().GetInt64("node-id")
	if err != nil {
		return 0, errx.Decorate(errorx.IllegalArgument.Wrap(err, "read --node-id"), reasons.Internal)
	}
	if id < 0 {
		return 0, errx.Decorate(errorx.IllegalArgument.New("--node-id must be >= 0"), reasons.InvalidArgument)
	}
	return id, nil
}

func requireNamespace(cmd *cobra.Command) (string, error) {
	ns, err := cmd.Flags().GetString("namespace")
	if err != nil {
		return "", errx.Decorate(errorx.IllegalArgument.Wrap(err, "read --namespace"), reasons.Internal)
	}
	if err := common.ValidateNamespaceFlag(ns); err != nil {
		return "", err
	}
	return ns, nil
}
