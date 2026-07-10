// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/joomcode/errorx"
)

// TCRunner abstracts live kernel tc class operations for testability.
type TCRunner interface {
	// ClassChange runs `tc class change dev <nic> parent 1:1 classid 1:<minor>
	// htb rate <rate> ceil <ceil> prio <prio>` on the live kernel.
	ClassChange(ctx context.Context, nic, minor, rate, ceil string, prio int) error
}

type execTCRunner struct{}

func (r *execTCRunner) ClassChange(ctx context.Context, nic, minor, rate, ceil string, prio int) error {
	cmd := exec.CommandContext(ctx, "/sbin/tc",
		"class", "change",
		"dev", nic,
		"parent", "1:1",
		"classid", "1:"+minor,
		"htb",
		"rate", rate,
		"ceil", ceil,
		"prio", fmt.Sprintf("%d", prio),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errorx.ExternalError.Wrap(err,
			"tc class change dev %s classid 1:%s failed: %s", nic, minor, strings.TrimSpace(string(out)))
	}
	return nil
}

// newExecTCRunner returns the production TC runner that shells out to /sbin/tc.
func newExecTCRunner() TCRunner {
	return &execTCRunner{}
}
