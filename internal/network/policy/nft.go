// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/joomcode/errorx"
)

// Runner is the seam over the system `nft` binary. Unlike the firewall package
// (which applies via a systemd service restart), `network policy` applies the
// rendered chain directly with `nft -f` (design §8.4.5) — the shared boot
// oneshot does not load network-weaver.nft until #780 extends it. Tests
// substitute a fake so the package builds and unit-tests on any platform
// (including macOS) without touching the kernel.
type Runner interface {
	// Apply loads a full nft document into the kernel (`nft -f -`). The
	// rendered document carries the idempotent `add table / delete table / add
	// table` prefix, so a re-apply atomically replaces the inet weaver table.
	Apply(ctx context.Context, doc string) error
	// AddElements adds initial membership to a policy's set
	// (`nft add element inet weaver <set> { … }`). Applied to the live kernel
	// only — set membership is never persisted (§8.3.1).
	AddElements(ctx context.Context, set string, elements []string) error
	// ListElements returns one set's live elements
	// (`nft list set inet weaver <set>`), or nil if the set has no elements
	// or does not exist. Used by Manager.Create to snapshot every policy's
	// membership before the destructive delete/recreate Apply() performs, so
	// it can be restored afterward (see Manager.Create for why).
	ListElements(ctx context.Context, set string) ([]string, error)
	// List returns the rendered inet weaver table (`nft list table inet weaver`).
	List(ctx context.Context) (string, error)
	// Delete removes the inet weaver table (`nft delete table inet weaver`).
	Delete(ctx context.Context) error
	// Exists reports whether the inet weaver table is present in the kernel.
	Exists(ctx context.Context) (bool, error)
}

// nftBinCandidates are the absolute locations we look for the system nft binary,
// in order. We never exec a bare "nft" off PATH to avoid picking up a binary
// from an attacker-controlled directory (see docs/dev/security-model.md).
var nftBinCandidates = []string{"/usr/sbin/nft", "/sbin/nft", "/usr/bin/nft"}

// tableArgs splits TableName into the argv tokens nft expects for sub-commands
// that take a family and table name as separate arguments (list, delete).
var tableArgs = strings.Fields(TableName) // ["inet", "weaver"]

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

func (r *execRunner) Apply(ctx context.Context, doc string) error {
	cmd := exec.CommandContext(ctx, r.bin, "-f", "-")
	cmd.Stdin = strings.NewReader(doc)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errorx.ExternalError.Wrap(err, "nft -f failed: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r *execRunner) AddElements(ctx context.Context, set string, elements []string) error {
	if len(elements) == 0 {
		return nil
	}
	// Fed through `nft -f -` rather than as argv tokens: element values contain
	// spaces (compound `<ip> . <port>` keys) and exec.Command does not tokenise
	// arguments on whitespace, so a single-string spec would be mis-parsed.
	spec := "add element " + TableName + " " + set + " { " + strings.Join(elements, ", ") + " }\n"
	cmd := exec.CommandContext(ctx, r.bin, "-f", "-")
	cmd.Stdin = strings.NewReader(spec)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errorx.ExternalError.Wrap(err, "nft add element %s failed: %s", set, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (r *execRunner) ListElements(ctx context.Context, set string) ([]string, error) {
	args := append([]string{"list", "set"}, tableArgs...)
	args = append(args, set)
	cmd := exec.CommandContext(ctx, r.bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// A missing set/table ("No such file or directory") is expected --
		// snapshotMembership calls this for every registry entry, including
		// ones never yet applied to the kernel, so treating that one
		// failure as "no elements" is safe (same reasoning as Exists()).
		// Any OTHER failure (permission denied, missing nft binary, a
		// syntax/version issue) must NOT be swallowed the same way:
		// Manager.Create's membership snapshot/restore safety net depends
		// on a real ListElements failure surfacing and aborting before the
		// destructive Apply() runs, not masquerading as "nothing to
		// preserve" and silently losing live membership.
		if isSetNotExistError(stderr.String()) {
			return nil, nil
		}
		return nil, errorx.ExternalError.Wrap(err, "nft list set %s failed: %s", set, strings.TrimSpace(stderr.String()))
	}
	return parseElementsLine(stdout.String()), nil
}

// isSetNotExistError reports whether nft's stderr indicates the set (or its
// table) doesn't exist -- the one ListElements failure mode that's safe to
// treat as "no elements" rather than propagating.
func isSetNotExistError(stderr string) bool {
	return strings.Contains(stderr, "No such file or directory")
}

// parseElementsLine extracts a set's live elements from `nft list set`
// output. nft omits the `elements = { … }` line entirely when a set has no
// elements, rather than printing an empty one, so absence of the marker
// means "no elements", not a parse failure.
func parseElementsLine(output string) []string {
	const marker = "elements = {"
	start := strings.Index(output, marker)
	if start == -1 {
		return nil
	}
	rest := output[start+len(marker):]
	end := strings.Index(rest, "}")
	if end == -1 {
		return nil
	}
	inner := strings.TrimSpace(rest[:end])
	if inner == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
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
	// `nft list table inet weaver` exits zero when the table is present and
	// non-zero when absent. Treating any failure as "absent" is safe: a genuine
	// nft/permission error surfaces on the subsequent Apply.
	cmd := exec.CommandContext(ctx, r.bin, append([]string{"list", "table"}, tableArgs...)...)
	return cmd.Run() == nil, nil
}
