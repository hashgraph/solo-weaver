// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

import (
	"context"
	"os/exec"
	"strconv"
	"strings"

	"github.com/joomcode/errorx"
)

// tcBin is the absolute path to the tc binary. Absolute (never bare "tc" off
// PATH) so a caller cannot hijack the privileged invocation via an
// attacker-controlled directory (see docs/dev/security-model.md).
const tcBin = "/sbin/tc"

type execTCRunner struct{}

// run execs `tc <args...>` and wraps any non-zero exit with the combined output.
func (r *execTCRunner) run(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, tcBin, args...).CombinedOutput()
	if err != nil {
		return errorx.ExternalError.Wrap(err,
			"tc %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

func (r *execTCRunner) ClassChange(ctx context.Context, nic, minor, rate, ceil string, prio int) error {
	return r.run(ctx, "class", "change",
		"dev", nic, "parent", "1:1", "classid", "1:"+minor,
		"htb", "rate", rate, "ceil", ceil, "prio", strconv.Itoa(prio))
}

func (r *execTCRunner) QdiscDelRoot(ctx context.Context, nic string) error {
	// Best-effort: swallow the error so a fresh veth (no root qdisc yet) or a
	// recycled veth name both start from a clean rebuild.
	_ = exec.CommandContext(ctx, tcBin, "qdisc", "del", "dev", nic, "root").Run()
	return nil
}

func (r *execTCRunner) QdiscAddRoot(ctx context.Context, nic, defaultMinor string) error {
	return r.run(ctx, "qdisc", "add",
		"dev", nic, "root", "handle", "1:", "htb", "default", defaultMinor)
}

func (r *execTCRunner) ClassAddRoot(ctx context.Context, nic, rate, ceil string) error {
	return r.run(ctx, "class", "add",
		"dev", nic, "parent", "1:", "classid", "1:1", "htb", "rate", rate, "ceil", ceil)
}

func (r *execTCRunner) ClassAdd(ctx context.Context, nic, minor, rate, ceil string, prio int) error {
	return r.run(ctx, "class", "add",
		"dev", nic, "parent", "1:1", "classid", "1:"+minor,
		"htb", "rate", rate, "ceil", ceil, "prio", strconv.Itoa(prio))
}

func (r *execTCRunner) QdiscAddFqCodel(ctx context.Context, nic, minor, handle string) error {
	return r.run(ctx, "qdisc", "add",
		"dev", nic, "parent", "1:"+minor, "handle", handle+":", "fq_codel")
}

// newExecTCRunner returns the production TC runner that shells out to /sbin/tc.
func newExecTCRunner() TCRunner {
	return &execTCRunner{}
}
