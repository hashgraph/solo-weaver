// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"strings"

	"github.com/automa-saga/errx"
	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var (
	flagVerifyKeysDir     string
	flagVerifyFromCluster bool

	verifySelectors func() cnkeys.Selectors

	verifyCmd = &cobra.Command{
		Use:   "verify",
		Short: "Validate consensus node key material in a folder or in the cluster",
		Long: "Check that the selected key material is well-formed: private keys and certificates parse, " +
			"each certificate's public key matches its private key, the gossip key is RSA-3072, and the admin " +
			"public key is a valid Ed25519 or secp256k1 key. Verify a folder with --keys-dir, or the in-cluster " +
			"secrets with --from-cluster --node-id N.\n\n" +
			"When a folder also contains the admin private key, its correspondence to the public key is checked too.",
		RunE: runVerify,
	}
)

func init() {
	verifySelectors = addSelectorFlags(verifyCmd)
	verifyCmd.Flags().StringVar(&flagVerifyKeysDir, "keys-dir", "", "Node key folder to validate")
	verifyCmd.Flags().BoolVar(&flagVerifyFromCluster, "from-cluster", false, "Validate the in-cluster per-node secrets instead of a folder (requires --node-id)")
	verifyCmd.MarkFlagsMutuallyExclusive("keys-dir", "from-cluster")
}

func runVerify(cmd *cobra.Command, _ []string) error {
	fromDir := strings.TrimSpace(flagVerifyKeysDir)
	if fromDir == "" && !flagVerifyFromCluster {
		return errx.Decorate(
			errorx.IllegalArgument.New("one of --keys-dir or --from-cluster is required"),
			reasons.InvalidArgument,
			"pass --keys-dir <dir> to validate a folder, or --from-cluster --node-id <N> to validate the cluster secrets")
	}

	var (
		report *cnkeys.VerifyReport
		err    error
	)

	if flagVerifyFromCluster {
		namespace, nerr := requireNamespace(cmd)
		if nerr != nil {
			return nerr
		}
		nodeID, nerr := requireNodeID(cmd)
		if nerr != nil {
			return nerr
		}
		client, cerr := newSecretClient(cmd.Context())
		if cerr != nil {
			return errx.Decorate(
				errorx.IllegalState.Wrap(cerr, "connect to the Kubernetes cluster"),
				reasons.PreconditionNotMet,
				"ensure a cluster is reachable (KUBECONFIG or in-cluster config)")
		}
		report, err = cnkeys.VerifyCluster(cmd.Context(), client, namespace, nodeID, verifySelectors())
	} else {
		report, err = cnkeys.VerifyDir(fromDir, verifySelectors())
	}
	if err != nil {
		return errx.Decorate(err, reasons.Internal)
	}

	cmd.Printf("Verifying %s:\n", report.Source)
	for _, f := range report.Findings {
		status := "OK"
		if !f.OK {
			status = "FAIL"
		}
		cmd.Printf("  [%-4s] %-6s %s\n", status, f.Key, f.Detail)
	}

	if !report.OK() {
		return errx.Decorate(
			errorx.IllegalState.New("key material verification failed"),
			reasons.PreconditionNotMet,
			"regenerate the failing key material with `consensus keys generate`, or re-import it")
	}
	cmd.Println("All checks passed.")
	return nil
}
