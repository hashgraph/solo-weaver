// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/joomcode/errorx"
)

// Runner is the seam over the system `nft` binary for read, check and delete
// operations. Live rule application is done by writing the on-disk artifact
// and restarting the systemd service via DBus — so Apply is not part of this
// interface. Tests substitute a fake so the package builds and unit-tests on
// any platform (including macOS) without touching the kernel.
type Runner interface {
	// List returns the rendered ruleset for the inet weaver-host-firewall table
	// (`nft list table inet weaver-host-firewall`).
	List(ctx context.Context) (string, error)
	// Check dry-runs a rendered ruleset file (`nft -c -f <path>`) without
	// committing it, so a document the kernel would reject is caught before it
	// becomes the persisted boot artifact.
	Check(ctx context.Context, path string) error
	// Delete removes the inet weaver-host-firewall table (`nft delete table inet weaver-host-firewall`).
	Delete(ctx context.Context) error
	// Exists reports whether the inet weaver-host-firewall table is present in the kernel.
	Exists(ctx context.Context) (bool, error)
}

// nftBinCandidates are the absolute locations we look for the system nft binary,
// in order. We never exec a bare "nft" off PATH to avoid picking up a binary
// from an attacker-controlled directory (see docs/dev/security-model.md).
var nftBinCandidates = []string{"/usr/sbin/nft", "/sbin/nft", "/usr/bin/nft"}

// tableArgs splits TableName into the argv tokens nft expects for sub-commands
// that take a family and table name as separate arguments (list, delete).
// exec.Command does not tokenise arguments on whitespace, so passing TableName
// as a single arg would send "inet weaver-host-firewall" as one token instead of two.
var tableArgs = strings.Fields(TableName) // ["inet", "weaver-host-firewall"]

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

// Check dry-runs the document at path. `-c` makes nft parse and evaluate the
// ruleset against the current kernel state without committing it, so this is
// the same verdict a real load would give without any of the side effects.
func (r *execRunner) Check(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, r.bin, "-c", "-f", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		// nft exits non-zero for two unrelated reasons: a ruleset it parsed and
		// refused, and an environment failure (no CAP_NET_ADMIN, a netlink error,
		// a missing binary). Only the former reports a source position, so the
		// `<path>:<line>:<col>` prefix — not the exit code — is what separates
		// "the ruleset is wrong" from "the host is wrong". Treating every non-zero
		// exit as malformed input would style a privilege error as bad input and
		// point the operator at the ruleset instead of their permissions.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && isRulesetDiagnostic(path, msg) {
			return errorx.IllegalFormat.New("nft rejected the rendered ruleset: %s", msg)
		}
		return errorx.ExternalError.Wrap(err, "failed to check the rendered ruleset with %s -c -f: %s", r.bin, msg)
	}
	return nil
}

// isRulesetDiagnostic reports whether stderr carries an nft source position for
// path — the `<path>:<line>:<col>: Error: …` form nft prints when it has read the
// document and objected to its contents. Matching the position rather than the
// message keeps this independent of nft's wording across versions, and requiring
// the path to be the one we handed it stops a diagnostic about some other file
// (an `include`, say) from being read as a verdict on ours.
func isRulesetDiagnostic(path, stderr string) bool {
	for line := range strings.SplitSeq(stderr, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), path+":")
		if !ok {
			continue
		}
		// nft follows the path with "<line>:<col>". A digit is enough to tell a
		// position report from a message that merely mentions the file by name.
		if rest != "" && rest[0] >= '0' && rest[0] <= '9' {
			return true
		}
	}
	return false
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
	// `nft list table inet weaver-host-firewall` exits zero when the table is present and
	// non-zero (with "No such file or directory" on stderr) when it is absent.
	// There is no false-positive risk in treating any failure as "absent": the
	// subsequent Apply will surface a genuine nft/permission error if one exists.
	cmd := exec.CommandContext(ctx, r.bin, append([]string{"list", "table"}, tableArgs...)...)
	return cmd.Run() == nil, nil
}
