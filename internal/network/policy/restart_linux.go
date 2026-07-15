// SPDX-License-Identifier: Apache-2.0

//go:build linux

package policy

import (
	"context"

	soos "github.com/hashgraph/solo-weaver/pkg/os"
)

// RestartNetworkNftService restarts the shared solo-provisioner-network-nft.service
// oneshot so the kernel picks up any nft files written since the last restart.
// The unit's RemainAfterExit=yes state is updated to reflect both
// network-host.nft and network-weaver.nft as loaded.
func RestartNetworkNftService(ctx context.Context) error {
	return soos.RestartService(ctx, NetworkNftService)
}
