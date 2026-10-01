// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"fmt"
	"strings"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	corekeys "github.com/hashgraph/solo-weaver/pkg/consensus/keys"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var (
	flagInitKeysDir   string
	flagInitIdentity  string
	flagInitMachineID string
	flagInitAdminAlgo string
	flagInitForce     bool

	initSelectors func() cnkeys.Selectors

	initCmd = &cobra.Command{
		Use:   "init",
		Short: "Generate a node's key material and create its secrets in one step",
		Long: "One-shot convenience for a node whose id is already known (genesis / known assignment): " +
			"generate the key material into --keys-dir, then create the per-node Kubernetes secrets from it. " +
			"Equivalent to `keys generate` followed by `keys import --node-id N`, reusing the same code paths.\n\n" +
			"For a DAB join the node-id is not known until the network assigns it, so use `keys generate` " +
			"(node-id-free) then `keys import` separately instead.\n\n" +
			"The admin private key is written to --keys-dir only (operator custody); only the public key is " +
			"pushed to Kubernetes.",
		RunE: runInit,
	}
)

func init() {
	initSelectors = addSelectorFlags(initCmd)
	initCmd.Flags().StringVar(&flagInitKeysDir, "keys-dir", "", "Directory for this node's generated key material (required); make it node-specific")
	initCmd.Flags().StringVar(&flagInitIdentity, "identity", "", "Optional human-readable label recorded in the gossip certificate OU/SAN")
	initCmd.Flags().StringVar(&flagInitMachineID, "machine-id", "", "Override the gossip certificate CN; defaults to the host machine UUID (/etc/machine-id)")
	initCmd.Flags().StringVar(&flagInitAdminAlgo, "admin-algo", string(corekeys.AdminEd25519), "Admin key algorithm: ed25519 or ecdsa (secp256k1)")
	initCmd.Flags().BoolVar(&flagInitForce, "force", false, "Overwrite the gossip signing secret if it already exists (changes the node's network identity)")
}

func runInit(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(flagInitKeysDir) == "" {
		return errx.Decorate(
			errorx.IllegalArgument.New("--keys-dir is required"),
			reasons.InvalidArgument,
			"pass --keys-dir <dir> (a node-specific directory); the admin private key is kept there")
	}

	adminAlgo := corekeys.AdminAlgo(strings.ToLower(strings.TrimSpace(flagInitAdminAlgo)))
	if adminAlgo != corekeys.AdminEd25519 && adminAlgo != corekeys.AdminECDSA {
		return errx.Decorate(
			errorx.IllegalArgument.New("unsupported --admin-algo %q", flagInitAdminAlgo),
			reasons.InvalidArgument,
			fmt.Sprintf("pass one of: %v", corekeys.SupportedAdminAlgos()))
	}

	namespace, err := requireNamespace(cmd)
	if err != nil {
		return err
	}
	nodeID, err := requireNodeID(cmd)
	if err != nil {
		return err
	}

	selectors := cnkeys.NormalizeSelectors(initSelectors())

	// 1. Generate into --keys-dir (same path as `keys generate`). Idempotent:
	// existing key types are kept unless --force, so re-running init is safe.
	toGen, need := planGeneration(flagInitKeysDir, selectors, flagInitForce)
	if need {
		set, err := cnkeys.Generate(cnkeys.GenerateOptions{
			Selectors: toGen,
			Identity:  flagInitIdentity,
			MachineID: flagInitMachineID,
			AdminAlgo: adminAlgo,
		})
		if err != nil {
			return errx.Decorate(err, reasons.Internal)
		}
		if err := set.WriteToDir(flagInitKeysDir); err != nil {
			return errx.Decorate(err, reasons.Internal,
				fmt.Sprintf("ensure %q is writable and has enough space", flagInitKeysDir))
		}
	}

	// 2. Import from --keys-dir (same path as `keys import`).
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
		FromDir:   flagInitKeysDir,
		Selectors: selectors,
		Force:     flagInitForce,
	})
	if err != nil {
		return errx.Decorate(err, reasons.Internal,
			fmt.Sprintf("the key material was generated in %q; re-run `keys import` from there once the cause is fixed", flagInitKeysDir))
	}

	logx.As().Info().Str("namespace", namespace).Int64("nodeId", nodeID).Str("keysDir", flagInitKeysDir).Strs("secrets", res.Applied).Msg("Initialized consensus node keys")

	cmd.Printf("Initialized key material for node%d in namespace %s\n", nodeID, namespace)
	cmd.Printf("  generated in: %s\n", flagInitKeysDir)
	for _, name := range res.Applied {
		cmd.Printf("  secret/%s\n", name)
	}
	if selectors.Admin {
		cmd.Println("Keep the admin private key (" + cnkeys.FileAdminPrivate + " in --keys-dir) off-cluster; only the public key was pushed to Kubernetes.")
	}
	return nil
}
