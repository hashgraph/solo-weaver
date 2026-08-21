// SPDX-License-Identifier: Apache-2.0

// verify-signatures checks every detached signature produced by `task sign`
// against the trust anchors embedded in pkg/codesign — the exact decision a
// released binary makes when it auto-downloads another release artifact.
//
// This is the release-time guard for a signing-key rotation: if the key that
// just signed the artifacts is not embedded in this build, the release fails
// here instead of shipping assets no deployed CLI can verify (see #1036, where
// the key rotated at v0.28.0 and the daemon auto-download path stayed broken
// for two releases).
//
// It scans a directory (default `bin`) for `*.asc` files and verifies each
// against the artifact of the same name with the suffix removed, so both the
// binary signatures and the `.sha256.asc` signatures are covered.
//
// Invoke via the Taskfile:
//
//	task verify:signatures
//	task verify:signatures BIN_DIR=some/other/dir
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/pkg/codesign"
)

// sigSuffix is the extension `task sign` gives every detached signature.
const sigSuffix = ".asc"

func main() {
	dir := "bin"
	if len(os.Args) > 1 && os.Args[1] != "" {
		dir = os.Args[1]
	}
	if err := run(dir); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	sigs, err := filepath.Glob(filepath.Join(dir, "*"+sigSuffix))
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to scan %s for signatures", dir)
	}
	if len(sigs) == 0 {
		return errorx.IllegalState.New(
			"no %s files found in %s: run `task sign` first", sigSuffix, dir)
	}
	sort.Strings(sigs)

	var failed []string
	for _, sig := range sigs {
		artifact := strings.TrimSuffix(sig, sigSuffix)
		if err := verifyOne(artifact, sig); err != nil {
			fmt.Printf("FAIL %s\n     %v\n", filepath.Base(artifact), err)
			failed = append(failed, filepath.Base(artifact))
			continue
		}
		fmt.Printf("ok   %s\n", filepath.Base(artifact))
	}

	if len(failed) > 0 {
		return errorx.RejectedOperation.New(
			"%d of %d release artifacts do not verify against the embedded trust anchors (%s): "+
				"if the release signing key was rotated, add its public key to pkg/codesign/keys/",
			len(failed), len(sigs), strings.Join(failed, ", "))
	}

	fmt.Printf("\nAll %d release signatures verify against the embedded trust anchors.\n", len(sigs))
	return nil
}

// verifyOne verifies a single artifact against its detached signature.
func verifyOne(artifact, sig string) error {
	content, err := os.Open(artifact)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "signature %s has no matching artifact", filepath.Base(sig))
	}
	defer content.Close()

	signature, err := os.Open(sig)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to read signature %s", filepath.Base(sig))
	}
	defer signature.Close()

	return codesign.Verify(content, signature)
}
