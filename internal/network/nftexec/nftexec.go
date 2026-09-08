// SPDX-License-Identifier: Apache-2.0

// Package nftexec holds the `nft` commands shared by the host firewall and
// workload policy managers.
package nftexec

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/joomcode/errorx"
)

// binCandidates are the absolute paths tried for the nft binary, in order.
// Never a bare "nft" off PATH (see docs/dev/security-model.md).
var binCandidates = []string{"/usr/sbin/nft", "/sbin/nft", "/usr/bin/nft"}

// Binary resolves the nft binary and reports whether one was found. With none
// found it returns the first candidate so callers can still exec and fail.
func Binary() (string, bool) {
	for _, c := range binCandidates {
		if _, err := os.Stat(c); err == nil {
			return c, true
		}
	}
	return binCandidates[0], false
}

// TableExists reports whether table ("<family> <name>") is live. An error means
// presence is unknown and must not be read as absence.
func TableExists(ctx context.Context, bin, table string) (bool, error) {
	// `list tables` exits 0 even when the table is absent, unlike `list table`.
	cmd := exec.CommandContext(ctx, bin, "list", "tables")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false, errorx.ExternalError.Wrap(err,
			"nft list tables failed, so whether %s is present cannot be determined: %s",
			table, strings.TrimSpace(stderr.String()))
	}
	return TableListed(stdout.String(), table), nil
}

// DeleteTable removes table from the kernel. An already-absent table is not an
// error.
func DeleteTable(ctx context.Context, bin, table string) error {
	cmd := exec.CommandContext(ctx, bin, append([]string{"delete", "table"}, strings.Fields(table)...)...)
	// LC_ALL=C pins the error text so the match below holds under any locale.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "No such file or directory") {
			return nil
		}
		return errorx.ExternalError.Wrap(err, "nft delete table %s failed: %s", table, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// TableListed reports whether `nft list tables` output declares table. Empty
// output is a valid "no".
func TableListed(out, table string) bool {
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "table" && f[1]+" "+f[2] == table {
			return true
		}
	}
	return false
}
