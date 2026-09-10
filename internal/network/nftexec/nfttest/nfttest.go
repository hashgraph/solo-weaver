// SPDX-License-Identifier: Apache-2.0

// Package nfttest provides a fake nft binary for the nftexec and manager tests.
package nfttest

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// FakeNft returns the path to a script that prints stdout and exits with code.
func FakeNft(t *testing.T, stdout string, code int) string {
	t.Helper()
	return FakeNftStderr(t, stdout, "", code)
}

// FakeNftStderr is FakeNft with stderr as well.
func FakeNftStderr(t *testing.T, stdout, stderr string, code int) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "stdout")
	if err := os.WriteFile(out, []byte(stdout), 0o600); err != nil {
		t.Fatalf("write fake nft stdout: %v", err)
	}
	errOut := filepath.Join(dir, "stderr")
	if err := os.WriteFile(errOut, []byte(stderr), 0o600); err != nil {
		t.Fatalf("write fake nft stderr: %v", err)
	}

	bin := filepath.Join(dir, "nft")
	script := "#!/bin/sh\ncat " + out + "\ncat " + errOut + " >&2\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake nft script: %v", err)
	}
	return bin
}
