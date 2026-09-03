// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

import (
	"context"

	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
)

// ApplyTcEgressScript ensures the bandwidth-shaper unit is current, then restarts
// it. Callers must render the boot script first — restart is a no-op without it.
func ApplyTcEgressScript(ctx context.Context) error {
	if err := EnsureTcEgressUnit(ctx); err != nil {
		return errorx.Decorate(err, "failed to install bandwidth-shaper service unit")
	}
	if err := soos.RestartService(ctx, TcEgressService); err != nil {
		return errorx.Decorate(err, "bandwidth-shaper script failed")
	}
	return nil
}
