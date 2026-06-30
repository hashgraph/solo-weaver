// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/joomcode/errorx"
)

// Runner is the seam over the system `nft` binary for read and delete
// operations. Live rule application is done by writing the on-disk artifact
// and restarting the systemd service via DBus — so Apply is not part of this
// interface. Tests substitute a fake so the package builds and unit-tests on
// any platform (including macOS) without touching the kernel.
type Runner interface {
	// List returns the rendered ruleset for the inet host table
	// (`nft list table inet host`).
	List(ctx context.Context) (string, error)
	// Delete removes the inet host table (`nft delete table inet host`).
	Delete(ctx context.Context) error
	// Exists reports whether the inet host table is present in the kernel.
	Exists(ctx context.Context) (bool, error)
}

// nftBinCandidates are the absolute locations we look for the system nft binary,
// in order. We never exec a bare "nft" off PATH to avoid picking up a binary
// from an attacker-controlled directory (see docs/dev/security-model.md).
var nftBinCandidates = []string{"/usr/sbin/nft", "/sbin/nft", "/usr/bin/nft"}

// tableArgs splits TableName into the argv tokens nft expects for sub-commands
// that take a family and table name as separate arguments (list, delete).
// exec.Command does not tokenise arguments on whitespace, so passing TableName
// as a single arg would send "inet host" as one token instead of two.
var tableArgs = strings.Fields(TableName) // ["inet", "host"]

// execRunner shells out to the system nft binary.
type execRunner struct {
	bin string
}

// NewExecRunner resolves the nft binary path and returns a Runner that applies
// changes to the live kernel.
func NewExecRunner() Runner {
	bin := nftBinCandidates[0]
	for _, c := range nftBinCandidates {
		if _, err := os.Stat(c); err == nil {
			bin = c
			break
		}
	}
	return &execRunner{bin: bin}
}

func (r *execRunner) List(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, r.bin, append([]string{"list", "table"}, tableArgs...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", errorx.ExternalError.Wrap(err, "nft list table %s failed: %s", TableName, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (r *execRunner) Delete(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, r.bin, append([]string{"delete", "table"}, tableArgs...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errorx.ExternalError.Wrap(err, "nft delete table %s failed: %s", TableName, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r *execRunner) Exists(ctx context.Context) (bool, error) {
	// `nft list table inet host` exits zero when the table is present and
	// non-zero (with "No such file or directory" on stderr) when it is absent.
	// There is no false-positive risk in treating any failure as "absent": the
	// subsequent Apply will surface a genuine nft/permission error if one exists.
	cmd := exec.CommandContext(ctx, r.bin, append([]string{"list", "table"}, tableArgs...)...)
	return cmd.Run() == nil, nil
}
