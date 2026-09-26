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

var (
	flagExportKeysDir string
	flagExportForce   bool

	exportSelectors func() cnkeys.Selectors

	exportCmd = &cobra.Command{
		Use:   "export",
		Short: "Export the per-node Kubernetes secrets to a folder (backup)",
		Long: "Read the selected per-node secrets from the cluster and write their contents to a folder. " +
			"Useful as a backup or to move a node's material between namespaces.\n\n" +
			"The admin secret holds only the public key, so the admin private key cannot be recovered from " +
			"the cluster — keep the off-cluster admin.key from `keys generate` safe.",
		RunE: runExport,
	}
)

func init() {
	exportSelectors = addSelectorFlags(exportCmd)
	exportCmd.Flags().StringVar(&flagExportKeysDir, "keys-dir", "", "Node key folder to export into (required)")
	exportCmd.Flags().BoolVar(&flagExportForce, "force", false, "Overwrite existing files whose content differs (identical files are left as-is)")
}

func runExport(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(flagExportKeysDir) == "" {
		return errx.Decorate(
			errorx.IllegalArgument.New("--keys-dir is required"),
			reasons.InvalidArgument,
			"pass --keys-dir <dir> to choose where the exported material is written")
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

	res, err := cnkeys.Export(cmd.Context(), client, cnkeys.ExportOptions{
		Namespace: namespace,
		NodeId:    nodeID,
		OutDir:    flagExportKeysDir,
		Selectors: exportSelectors(),
		Force:     flagExportForce,
	})
	if err != nil {
		return errx.Decorate(err, reasons.Internal,
			fmt.Sprintf("verify the node%d secrets exist in namespace %q and %q is writable", nodeID, namespace, flagExportKeysDir))
	}

	logx.As().Info().Str("namespace", namespace).Int64("nodeId", nodeID).Strs("secrets", res.Exported).Msg("Exported consensus node key secrets")

	cmd.Printf("Exported key material for node%d from namespace %s to %s:\n", nodeID, namespace, flagExportKeysDir)
	for _, name := range res.Exported {
		cmd.Printf("  secret/%s (written)\n", name)
	}
	for _, name := range res.Skipped {
		cmd.Printf("  secret/%s (already up to date)\n", name)
	}
	return nil
}
