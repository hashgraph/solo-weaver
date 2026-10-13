// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"encoding/json"
	"strings"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var (
	flagInspectKeysDir string
	flagInspectFile    string

	inspectSelectors func() cnkeys.Selectors

	inspectCmd = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "inspect",
		Short: "Show the details of consensus node certificates and public keys",
		Long: "Print the details of the certificates and public keys in a key folder: subject, issuer, validity, signature algorithm, key type and size, SANs, key usage, " +
			"fingerprints, and the gRPC certificate hash. It replaces `openssl x509 -text -noout`.\n\n" +
			"Private key files are never read. To check that keys match their certificates, use " +
			"`consensus keys verify`. Use --file to inspect a single certificate or admin public key file.",
		RunE: runInspect,
	}
)

func init() {
	inspectSelectors = addSelectorFlags(inspectCmd)
	inspectCmd.Flags().StringVar(&flagInspectKeysDir, "keys-dir", "", "Node key folder to inspect")
	inspectCmd.Flags().StringVar(&flagInspectFile, "file", "", "Inspect a single certificate (PEM) or admin public key (hex) file")
	inspectCmd.MarkFlagsMutuallyExclusive("keys-dir", "file")
}

func runInspect(cmd *cobra.Command, _ []string) error {
	file := strings.TrimSpace(flagInspectFile)
	fromDir := strings.TrimSpace(flagInspectKeysDir)
	if file == "" && fromDir == "" {
		return errx.Decorate(
			errorx.IllegalArgument.New("one of --keys-dir or --file is required"),
			reasons.InvalidArgument,
			"pass --keys-dir <dir> to inspect a folder or --file <path> for a single file; to inspect cluster secrets, run `consensus keys export` first")
	}

	var (
		report *cnkeys.InspectReport
		err    error
	)
	switch {
	case file != "":
		report, err = cnkeys.InspectFile(file)
	default:
		report, err = cnkeys.InspectDir(fromDir, inspectSelectors())
	}
	if err != nil {
		return errx.Decorate(err, reasons.Internal)
	}

	if len(report.Keys) == 0 && !report.HasErrors() {
		return errx.Decorate(
			errorx.IllegalState.New("no key files found to inspect in %s", report.Source),
			reasons.PreconditionNotMet,
			"check the path, or generate key material with `consensus keys generate`")
	}

	if common.OutputIsJSON() {
		b, merr := json.MarshalIndent(report, "", "  ")
		if merr != nil {
			return errx.Decorate(errorx.IllegalState.Wrap(merr, "encode inspect report"), reasons.Internal)
		}
		cmd.Println(string(b))
	} else {
		printInspectReport(cmd, report)
	}

	if report.HasErrors() {
		return errx.Decorate(
			errorx.IllegalState.New("some key material could not be inspected"),
			reasons.PreconditionNotMet,
			"fix or replace the file(s) reported above; regenerate with `consensus keys generate` or re-import the key material")
	}
	return nil
}

func printInspectReport(cmd *cobra.Command, r *cnkeys.InspectReport) {
	const layout = "2006-01-02 15:04:05 UTC"
	cmd.Printf("Inspecting %s:\n", r.Source)
	for _, k := range r.Keys {
		if k.Kind != "" {
			cmd.Printf("\n[%s | %s]\n", k.File, k.Kind)
		} else {
			cmd.Printf("\n[%s]\n", k.File)
		}
		if k.Error != "" {
			cmd.Printf("  ERROR: %s\n", k.Error)
		}
		if c := k.Certificate; c != nil {
			cmd.Printf("  Subject:         %s\n", c.Subject)
			cmd.Printf("  Issuer:          %s\n", c.Issuer)
			cmd.Printf("  Self-signed:     %t\n", c.SelfSigned)
			cmd.Printf("  Serial:          %s\n", c.SerialNumber)
			cmd.Printf("  Not before:      %s\n", c.NotBefore.Format(layout))
			cmd.Printf("  Not after:       %s (%d days remaining)\n", c.NotAfter.Format(layout), c.DaysRemaining)
			cmd.Printf("  Signature:       %s\n", c.SignatureAlgorithm)
			cmd.Printf("  Public key:      %s-%d\n", c.PublicKeyAlgorithm, c.PublicKeyBits)
			if len(c.DNSNames) > 0 || len(c.IPAddresses) > 0 {
				cmd.Printf("  SANs:            %s\n", strings.Join(append(append([]string{}, c.DNSNames...), c.IPAddresses...), ", "))
			}
			if len(c.KeyUsage) > 0 {
				cmd.Printf("  Key usage:       %s\n", strings.Join(c.KeyUsage, ", "))
			}
			if len(c.ExtKeyUsage) > 0 {
				cmd.Printf("  Ext key usage:   %s\n", strings.Join(c.ExtKeyUsage, ", "))
			}
			cmd.Printf("  CA:              %t\n", c.IsCA)
			cmd.Printf("  SHA-256:         %s\n", c.SHA256)
			cmd.Printf("  SHA-384:         %s\n", c.SHA384)
		}
		if a := k.AdminPublic; a != nil {
			cmd.Printf("  Algorithm:       %s\n", a.Algorithm)
			cmd.Printf("  Public key:      %s\n", a.PublicKeyHex)
		}
	}
	if len(r.Missing) > 0 {
		cmd.Printf("\nNot found (skipped): %s\n", strings.Join(r.Missing, ", "))
	}
	if hasCertificate(r) {
		cmd.Printf("\nFor the full X.509 dump: openssl x509 -in <file> -text -noout\n")
		cmd.Printf("To check that keys match their certificates: consensus keys verify\n")
	}
}

func hasCertificate(r *cnkeys.InspectReport) bool {
	for _, k := range r.Keys {
		if k.Certificate != nil {
			return true
		}
	}
	return false
}
