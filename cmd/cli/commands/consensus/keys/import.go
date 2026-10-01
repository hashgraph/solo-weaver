// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"fmt"
	"strings"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

const sourceFolder = "folder"

var (
	flagImportKeysDir string
	flagImportSource  string
	flagImportForce   bool

	importSelectors func() cnkeys.Selectors

	importCmd = &cobra.Command{
		Use:   "import",
		Short: "Create the per-node Kubernetes secrets from a folder of key material",
		Long: "Read the selected key material from a folder (e.g. one produced by `keys generate`) and " +
			"create the three per-node Kubernetes secrets the solo-operator consumes: " +
			"node<N>-gossip-keys, node<N>-grpc-tls-keys, and node<N>-admin-pubkey.\n\n" +
			"Only the admin PUBLIC key is imported — the admin private key (admin.key) is never read or " +
			"pushed to the cluster. Re-importing identical material is idempotent; the gossip signing secret " +
			"is the node's network identity, so overwriting it with different material requires --force.",
		RunE: runImport,
	}
)

func init() {
	importSelectors = addSelectorFlags(importCmd)
	importCmd.Flags().StringVar(&flagImportKeysDir, "keys-dir", "", "Node key folder to import from (required for --source folder)")
	importCmd.Flags().StringVar(&flagImportSource, "source", sourceFolder, "Key material source: folder (vault support is tracked separately)")
	importCmd.Flags().BoolVar(&flagImportForce, "force", false, "Overwrite the gossip signing secret when its material differs; re-importing identical material is idempotent without this")
}

func runImport(cmd *cobra.Command, _ []string) error {
	source := strings.ToLower(strings.TrimSpace(flagImportSource))
	if source != sourceFolder {
		return errx.Decorate(
			errorx.IllegalArgument.New("unsupported --source %q", flagImportSource),
			reasons.InvalidArgument,
			"only --source folder is available today; vault-backed import is tracked separately")
	}
	if strings.TrimSpace(flagImportKeysDir) == "" {
		return errx.Decorate(
			errorx.IllegalArgument.New("--keys-dir is required for --source folder"),
			reasons.InvalidArgument,
			"pass --keys-dir <dir> pointing at the node key folder (e.g. the output of `keys generate`)")
	}

	namespace, err := requireNamespace(cmd)
	if err != nil {
		return err
	}
	nodeID, err := requireNodeID(cmd)
	if err != nil {
		return err
	}

	client, err := newSecretClient(cmd.Context())
	if err != nil {
		return errx.Decorate(
			errorx.IllegalState.Wrap(err, "connect to the Kubernetes cluster"),
			reasons.PreconditionNotMet,
			"ensure a cluster is reachable (KUBECONFIG or in-cluster config)")
	}

	res, err := cnkeys.Import(cmd.Context(), client, cnkeys.ImportOptions{
		Namespace: namespace,
		NodeId:    nodeID,
		FromDir:   flagImportKeysDir,
		Selectors: importSelectors(),
		Force:     flagImportForce,
	})
	if err != nil {
		return errx.Decorate(err, reasons.Internal,
			fmt.Sprintf("verify the key files exist under %q and the namespace %q is writable", flagImportKeysDir, namespace))
	}

	logx.As().Info().Str("namespace", namespace).Int64("nodeId", nodeID).Strs("secrets", res.Applied).Msg("Imported consensus node key secrets")

	cmd.Printf("Imported key material for node%d into namespace %s:\n", nodeID, namespace)
	for _, name := range res.Applied {
		cmd.Printf("  secret/%s (created or updated)\n", name)
	}
	for _, name := range res.Skipped {
		cmd.Printf("  secret/%s (already up to date)\n", name)
	}
	return nil
}
