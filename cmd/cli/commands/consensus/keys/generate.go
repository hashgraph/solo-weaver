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
	flagKeysDir       string
	flagIdentity      string
	flagMachineID     string
	flagGossip        bool
	flagTLS           bool
	flagAdmin         bool
	flagAdminAlgo     string
	flagGenerateForce bool
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate one node's key material into a folder",
	Long: "Generate the gossip signing key, gRPC TLS key, and node admin key for one node into " +
		"--keys-dir as enhanced PEM. Generation is node-id-free: the gossip certificate CN is the " +
		"machine UUID and the gRPC certificate CN is localhost, so the material is valid before the " +
		"network assigns a node-id during a DAB join.\n\n" +
		"--keys-dir is one node's folder — make it node-specific. For a genesis network, run generate " +
		"once per node into its own --keys-dir.\n\n" +
		"The command prints the gRPC certificate hash (the one join input not written as a file) to " +
		"stdout; the gossip certificate and admin public key are files. The admin private key is " +
		"written to --keys-dir only — never pushed to Kubernetes. To also create the Kubernetes " +
		"secrets in one step, use `consensus keys init`.",
	RunE: runGenerate,
}

func init() {
	generateCmd.Flags().StringVar(&flagKeysDir, "keys-dir", "", "Directory for this node's key material (required); make it node-specific")
	generateCmd.Flags().StringVar(&flagIdentity, "identity", "", "Optional human-readable label recorded in the gossip certificate OU/SAN (e.g. operator or host name)")
	generateCmd.Flags().StringVar(&flagMachineID, "machine-id", "", "Override the gossip certificate CN; defaults to the host machine UUID (/etc/machine-id)")
	generateCmd.Flags().BoolVar(&flagGossip, "gossip", false, "Generate the gossip signing key (default: generate all types when no selector is given)")
	generateCmd.Flags().BoolVar(&flagTLS, "tls", false, "Generate the gRPC TLS key (default: generate all types when no selector is given)")
	generateCmd.Flags().BoolVar(&flagAdmin, "admin", false, "Generate the node admin key (default: generate all types when no selector is given)")
	generateCmd.Flags().StringVar(&flagAdminAlgo, "admin-algo", string(corekeys.AdminEd25519), "Admin key algorithm: ed25519 or ecdsa (secp256k1)")
	generateCmd.Flags().BoolVar(&flagGenerateForce, "force", false, "Regenerate key material even if it already exists in --keys-dir (overwrites the existing keys)")
}

func runGenerate(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(flagKeysDir) == "" {
		return errx.Decorate(
			errorx.IllegalArgument.New("--keys-dir is required"),
			reasons.InvalidArgument,
			"pass --keys-dir <dir> (a node-specific directory) for the key material")
	}

	adminAlgo := corekeys.AdminAlgo(strings.ToLower(strings.TrimSpace(flagAdminAlgo)))
	if adminAlgo != corekeys.AdminEd25519 && adminAlgo != corekeys.AdminECDSA {
		return errx.Decorate(
			errorx.IllegalArgument.New("unsupported --admin-algo %q", flagAdminAlgo),
			reasons.InvalidArgument,
			fmt.Sprintf("pass one of: %v", corekeys.SupportedAdminAlgos()))
	}

	requested := cnkeys.Selectors{Gossip: flagGossip, Grpc: flagTLS, Admin: flagAdmin}
	toGen, need := planGeneration(flagKeysDir, requested, flagGenerateForce)
	if !need {
		cmd.Printf("All selected key material already exists in %s; nothing to generate (pass --force to regenerate).\n", flagKeysDir)
		return nil
	}

	set, err := cnkeys.Generate(cnkeys.GenerateOptions{
		Selectors: toGen,
		Identity:  flagIdentity,
		MachineID: flagMachineID,
		AdminAlgo: adminAlgo,
	})
	if err != nil {
		return errx.Decorate(err, reasons.Internal)
	}

	if err := set.WriteToDir(flagKeysDir); err != nil {
		return errx.Decorate(err, reasons.Internal,
			fmt.Sprintf("ensure %q is writable and has enough space", flagKeysDir))
	}

	logx.As().Info().Str("keysDir", flagKeysDir).Msg("Generated consensus node key material")
	printSummary(cmd, flagKeysDir, set)
	return nil
}

// planGeneration decides which requested key types to (re)generate into keysDir.
// Generation is idempotent: an existing type is skipped (with a warning) unless
// force is set. The bool is false when nothing needs generating.
func planGeneration(keysDir string, requested cnkeys.Selectors, force bool) (cnkeys.Selectors, bool) {
	requested = cnkeys.NormalizeSelectors(requested)
	if force {
		return requested, requested.Any()
	}
	toGen := requested.Minus(cnkeys.ExistingSelectors(keysDir))
	warnSkipped(keysDir, requested.Minus(toGen))
	return toGen, toGen.Any()
}

// warnSkipped logs a warning for each already-present key type that is being kept.
func warnSkipped(keysDir string, skipped cnkeys.Selectors) {
	for _, k := range []struct {
		on   bool
		name string
	}{{skipped.Gossip, "gossip"}, {skipped.Grpc, "grpc"}, {skipped.Admin, "admin"}} {
		if k.on {
			logx.As().Warn().Str("keysDir", keysDir).Str("keyType", k.name).
				Msg("key material already exists; keeping it (pass --force to regenerate)")
		}
	}
}

// printSummary writes copy-pasteable guidance to stdout, including the gRPC
// certificate hash (the one join input not written as a file).
func printSummary(cmd *cobra.Command, dir string, set *cnkeys.NodeKeySet) {
	cmd.Printf("Generated key material in %s\n", dir)

	if set.Gossip != nil {
		cmd.Printf("  gossip signing key: %s, %s\n", cnkeys.FileGossipPrivate, cnkeys.FileGossipCert)
	}
	if set.Grpc != nil {
		cmd.Printf("  gRPC TLS key:       %s, %s\n", cnkeys.FileGrpcKey, cnkeys.FileGrpcCert)
		cmd.Printf("  gRPC cert hash (SHA-384): %s\n", set.Grpc.CertHashSHA384())
	}
	if set.Admin != nil {
		cmd.Printf("  admin key (%s): %s (private, off-cluster), %s (public)\n", set.Admin.Algo, cnkeys.FileAdminPrivate, cnkeys.FileAdminPublic)
	}

	cmd.Println()
	cmd.Println("Next steps:")
	cmd.Println("  - Genesis / known id:  consensus keys import --node-id <N> --keys-dir " + dir)
	cmd.Println("  - DAB join:            submit nodeCreate off-cluster using the gossip cert (" + cnkeys.FileGossipCert + "),")
	cmd.Println("                         the gRPC cert hash above, and the admin public key (" + cnkeys.FileAdminPublic + "),")
	cmd.Println("                         then import with the assigned id")
	if set.Admin != nil {
		cmd.Println("  - Keep the admin private key (" + cnkeys.FileAdminPrivate + ") off-cluster; only the public key is pushed to Kubernetes.")
	}
}
