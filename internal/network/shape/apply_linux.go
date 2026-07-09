// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

import (
	"context"

	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

// ApplyTcEgressScript restarts the tc-egress systemd unit so the kernel picks up
// the HTB hierarchy immediately after install without waiting for a reboot.
// RestartService resets a previously-failed unit before running it, so a
// corrected script takes effect on re-install without a manual reset-failed.
// Using restart (rather than executing the script directly) keeps the unit
// state visible: on success the unit shows active (exited), on failure failed —
// matching what operators see after a reboot.
func ApplyTcEgressScript(ctx context.Context) error {
	if err := soos.RestartService(ctx, TcEgressService); err != nil {
		return errorx.Decorate(err, "tc-egress script failed")
	}
	return nil
}
