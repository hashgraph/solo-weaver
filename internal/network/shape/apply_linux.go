// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

import (
	"context"

	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

// ApplyTcEgressScript ensures the tc-egress systemd unit is installed, then
// restarts it so the kernel picks up the HTB hierarchy immediately without
// waiting for a reboot. EnsureTcEgressUnit is idempotent (SHA-256 gated) so
// repeated calls are cheap. RestartService resets a previously-failed unit
// before running it, so a corrected script takes effect without a manual
// reset-failed. Using restart (rather than executing the script directly) keeps
// unit state visible: on success the unit shows active (exited), on failure
// failed — matching what operators see after a reboot.
func ApplyTcEgressScript(ctx context.Context) error {
	if err := EnsureTcEgressUnit(ctx); err != nil {
		return errorx.Decorate(err, "failed to install tc-egress service unit")
	}
	if err := soos.RestartService(ctx, TcEgressService); err != nil {
		return errorx.Decorate(err, "tc-egress script failed")
	}
	return nil
}
