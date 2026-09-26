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
	flagImportSuffix  string
	flagImportLabels  []string

	importSelectors func() cnkeys.Selectors

	importCmd = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "import",
		Short: "Create the per-node Kubernetes secrets from a folder of key material",
		Long: "Read the selected key material from a folder (e.g. one produced by `keys generate`) and " +
			"create the three per-node Kubernetes secrets the solo-operator consumes. Each secret is named " +
			"by the key's own date and fingerprint — node<N>-gossip-keys-<date>-<fp8>, " +
			"node<N>-grpc-tls-keys-<date>-<fp8>, and node<N>-admin-pubkey-<fp8> — so importing new key " +
			"material (a rotation) creates new secrets and never replaces the live ones.\n\n" +
			"Only the admin PUBLIC key is imported — the admin private key (admin.key) is never read or " +
			"pushed to the cluster. Re-importing identical material is idempotent. A secret of the same name " +
			"whose content was edited is refused unless --force is passed.",
		RunE: runImport,
	}
)

func init() {
	importSelectors = addSelectorFlags(importCmd)
	importCmd.Flags().StringVar(&flagImportKeysDir, "keys-dir", "", "Node key folder to import from (required for --source folder)")
	importCmd.Flags().StringVar(&flagImportSource, "source", sourceFolder, "Key material source: folder (vault support is tracked separately)")
	importCmd.Flags().BoolVar(&flagImportForce, "force", false, "Overwrite a same-named secret whose content differs (e.g. one edited by hand); re-importing identical material is idempotent without this")
	importCmd.Flags().StringArrayVar(&flagImportLabels, "label", nil, "Extra label to put on every secret, as key=value, e.g. example.com/external-ref=ticket-42 (repeatable). On a secret whose content is already up to date only labels are changed")
	importCmd.Flags().StringVar(&flagImportSuffix, "secret-suffix", "", "Replace the certificate date in the secret names, e.g. v0.75.6 (lowercase letters, digits, '-' and '.')")
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

	labels, err := parseLabelFlags(flagImportLabels)
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
		Suffix:    flagImportSuffix,
		Labels:    labels,
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
	for _, name := range res.Relabeled {
		cmd.Printf("  secret/%s (content unchanged; labels updated)\n", name)
	}
	for _, name := range res.Skipped {
		cmd.Printf("  secret/%s (already up to date)\n", name)
	}
	return nil
}

// parseLabelFlags turns repeated --label key=value flags into a validated map.
func parseLabelFlags(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	labels := make(map[string]string, len(raw))
	for _, r := range raw {
		k, v, ok := strings.Cut(r, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, errx.Decorate(
				errorx.IllegalArgument.New("invalid --label %q: expected key=value", r),
				reasons.InvalidArgument,
				"pass --label key=value, e.g. --label example.com/external-ref=ticket-42")
		}
		labels[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if err := cnkeys.ValidateLabels(labels); err != nil {
		return nil, errx.Decorate(err, reasons.InvalidArgument,
			"use Kubernetes label syntax, and not the solo.hiero.org/node, key-type or cert-fingerprint labels")
	}
	return labels, nil
}
