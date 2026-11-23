// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"github.com/joomcode/errorx"
)

// LaneTrees lists every device carrying weaver's lane tree, in one tc call.
func LaneTrees(ctx context.Context) ([]LaneTree, error) {
	out, err := tcOutput(ctx, "-j", "qdisc", "show")
	if err != nil {
		return nil, err
	}
	return parseLaneTrees(out)
}

// TreeFilters describes every tc filter on dev at the points HTB consults.
func TreeFilters(ctx context.Context, dev string) ([]string, error) {
	var all []string
	for _, parent := range FilterParents {
		out, err := tcOutput(ctx, "-j", "filter", "show", "dev", dev, "parent", parent)
		if err != nil {
			return nil, err
		}
		descs, err := parseFilters(out, parent)
		if err != nil {
			return nil, err
		}
		all = append(all, descs...)
	}
	return all, nil
}

// DeviceClassStats is TCRunner.ClassStats for callers without a Manager.
func DeviceClassStats(ctx context.Context, dev string) (map[string]ClassStat, error) {
	return newExecTCRunner().ClassStats(ctx, dev)
}

// tcOutput runs tc and returns stdout, folding stderr into the error.
func tcOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, tcBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "tc %s failed: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
