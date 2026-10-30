// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"fmt"
	"net"
	"strings"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	corekeys "github.com/hashgraph/solo-weaver/pkg/consensus/keys"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var (
	readGrpcFlags func() (corekeys.GrpcOptions, error)

	flagKeysDir       string
	flagIdentity      string
	flagMachineID     string
	flagGossip        bool
	flagTLS           bool
	flagGenerateForce bool
)

var generateCmd = &cobra.Command{
	Args:  cobra.NoArgs,
	Use:   "generate",
	Short: "Generate one node's key material into a folder",
	Long: "Generate the gossip signing key and gRPC TLS key for one node into " +
		"--keys-dir as enhanced PEM. Generation is node-id-free: the gossip certificate CN is the " +
		"machine UUID and the gRPC certificate CN is localhost, so the material is valid before the " +
		"network assigns a node-id during a DAB join.\n\n" +
		"--keys-dir is one node's folder — make it node-specific. For a genesis network, run generate " +
		"once per node into its own --keys-dir.\n\n" +
		"The command prints the gRPC certificate hash (the one join input not written as a file) to " +
		"stdout; the gossip certificate is a file. The node admin key is not generated here: create it in " +
		"an external wallet and provide only its public key. To also create the Kubernetes " +
		"secrets in one step, use `consensus keys init`.",
	RunE: runGenerate,
}

func init() {
	generateCmd.Flags().StringVar(&flagKeysDir, "keys-dir", "", "Directory for this node's key material (required); make it node-specific")
	generateCmd.Flags().StringVar(&flagIdentity, "identity", "", "Optional human-readable label recorded in the gossip certificate OU/SAN (e.g. operator or host name)")
	generateCmd.Flags().StringVar(&flagMachineID, "machine-id", "", "Override the gossip certificate CN; defaults to the host machine UUID (/etc/machine-id)")
	generateCmd.Flags().BoolVar(&flagGossip, "gossip", false, "Generate the gossip signing key (default: generate all types when no selector is given)")
	generateCmd.Flags().BoolVar(&flagTLS, "tls", false, "Generate the gRPC TLS key (default: generate all types when no selector is given)")
	readGrpcFlags = addGrpcFlags(generateCmd)
	generateCmd.Flags().BoolVar(&flagGenerateForce, "force", false, "Regenerate key material even if it already exists in --keys-dir (overwrites the existing keys)")
}

func runGenerate(cmd *cobra.Command, _ []string) error {
	if strings.TrimSpace(flagKeysDir) == "" {
		return errx.Decorate(
			errorx.IllegalArgument.New("--keys-dir is required"),
			reasons.InvalidArgument,
			"pass --keys-dir <dir> (a node-specific directory) for the key material")
	}

	grpcOpts, err := readGrpcFlags()
	if err != nil {
		return err
	}

	requested := cnkeys.Selectors{Gossip: flagGossip, Grpc: flagTLS}
	toGen, need := planGeneration(flagKeysDir, requested, flagGenerateForce)
	if !need {
		cmd.Printf("All selected key material already exists in %s; nothing to generate (pass --force to regenerate).\n", flagKeysDir)
		return nil
	}

	set, err := cnkeys.Generate(cnkeys.GenerateOptions{
		Selectors: toGen,
		Identity:  flagIdentity,
		MachineID: flagMachineID,
		Grpc:      grpcOpts,
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

// addGrpcFlags registers the gRPC certificate flags on cmd and returns a reader
// that validates them. Fresh vars per command avoid shared-flag clobbering.
func addGrpcFlags(cmd *cobra.Command) func() (corekeys.GrpcOptions, error) {
	dns := cmd.Flags().StringSlice("grpc-dns", nil, "Extra fully qualified DNS SAN for the gRPC TLS certificate, added to localhost (repeatable)")
	ips := cmd.Flags().StringSlice("grpc-ip", nil, "Extra IP SAN for the gRPC TLS certificate, added to the loopback addresses (repeatable)")
	cn := cmd.Flags().String("grpc-cn", "", "Common name (subject and issuer) of the gRPC TLS certificate; default is localhost")
	org := cmd.Flags().String("grpc-org", "", "Organization (subject and issuer O) of the gRPC TLS certificate; omitted by default")
	days := cmd.Flags().Int("grpc-validity-days", 0, fmt.Sprintf("gRPC TLS certificate lifetime in days (1-%d); default is 100 years", corekeys.MaxGrpcValidityDays))
	return func() (corekeys.GrpcOptions, error) {
		opts := corekeys.GrpcOptions{ValidityDays: *days}
		if *cn != "" {
			name, err := corekeys.NormalizeGrpcCommonName(*cn)
			if err != nil {
				return opts, errx.Decorate(err, reasons.InvalidArgument,
					fmt.Sprintf("pass a non-blank --grpc-cn of at most %d characters without control characters", corekeys.MaxSubjectFieldLength))
			}
			opts.CommonName = name
		}
		if *org != "" {
			name, err := corekeys.NormalizeGrpcOrganization(*org)
			if err != nil {
				return opts, errx.Decorate(err, reasons.InvalidArgument,
					fmt.Sprintf("pass a non-blank --grpc-org of at most %d characters without control characters", corekeys.MaxSubjectFieldLength))
			}
			opts.Organization = name
		}
		for _, d := range *dns {
			d = strings.TrimSpace(d)
			if err := sanity.ValidateFQDN(d); err != nil {
				return opts, errx.Decorate(err, reasons.InvalidArgument, "pass a fully qualified host name (with at least one dot) to --grpc-dns, e.g. node0.example.org")
			}
			opts.DNSNames = append(opts.DNSNames, d)
		}
		for _, raw := range *ips {
			ip := net.ParseIP(strings.TrimSpace(raw))
			if ip == nil {
				return opts, errx.Decorate(
					errorx.IllegalArgument.New("invalid --grpc-ip %q", raw),
					reasons.InvalidArgument, "pass an IPv4 or IPv6 address, e.g. 10.1.2.3")
			}
			opts.IPAddresses = append(opts.IPAddresses, ip)
		}
		if *days < 0 || *days > corekeys.MaxGrpcValidityDays {
			return opts, errx.Decorate(
				errorx.IllegalArgument.New("--grpc-validity-days must be between 1 and %d, got %d", corekeys.MaxGrpcValidityDays, *days),
				reasons.InvalidArgument, "omit the flag for the default 100-year lifetime")
		}
		return opts, nil
	}
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
	}{{skipped.Gossip, "gossip"}, {skipped.Grpc, "grpc"}} {
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

	cmd.Println()
	cmd.Println("Next steps:")
	cmd.Println("  - Genesis / known id:  consensus keys import --node-id <N> --keys-dir " + dir)
	cmd.Println("  - DAB join:            submit nodeCreate off-cluster using the gossip cert (" + cnkeys.FileGossipCert + "),")
	cmd.Println("                         the gRPC cert hash above, and the admin public key from your wallet,")
	cmd.Println("                         then import with the assigned id")
}
