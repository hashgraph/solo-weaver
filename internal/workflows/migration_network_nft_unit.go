// SPDX-License-Identifier: Apache-2.0

// migration_network_nft_unit.go converges the nftables loader unit on
// already-provisioned hosts. Gated on host state, not on a version boundary, so
// it fires whenever the unit drifts. See docs/dev/traffic-shaper.md.

package workflows

import (
	"context"
	"os"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
)

// Seams so tests can script host state without touching /usr/lib or /etc.
var (
	networkNftUnitNeedsConverge = firewall.NetworkNftUnitNeedsConverge
	ensureNetworkNftUnit        = firewall.EnsureNetworkNftUnit
	nftUnitGeteuid              = os.Geteuid
)

// NetworkNftUnitMigration converges the nftables loader unit when it drifts from
// the embedded one, or when it is byte-current but disabled at boot.
type NetworkNftUnitMigration struct{}

// NewNetworkNftUnitMigration constructs the migration.
func NewNetworkNftUnitMigration() *NetworkNftUnitMigration {
	return &NetworkNftUnitMigration{}
}

// ID implements migration.Migration. No version suffix — not tied to a release boundary.
func (m *NetworkNftUnitMigration) ID() string { return "network-nft-loader-unit" }

// Description implements migration.Migration.
func (m *NetworkNftUnitMigration) Description() string {
	return "Converge the solo-provisioner-network-nft.service loader unit on already-provisioned " +
		"hosts so boot ordering changes reach hosts that only receive a new binary, and re-enable " +
		"it when it is present but would not start at boot"
}

// Applies reports whether the loader unit needs converging. A probe error counts
// as "no", so a transient read failure cannot fail every command on the host.
func (m *NetworkNftUnitMigration) Applies(mctx *migration.Context) (bool, error) {
	// Writing and enabling the unit need root; skip for non-root callers instead of
	// failing every later command.
	if euid := nftUnitGeteuid(); euid != 0 {
		logx.As().Debug().Int("euid", euid).
			Msg("network nft unit migration: not root; leaving the loader unit to a privileged invocation")
		return false, nil
	}

	// Applies takes no context of its own; the probe only needs one for a short
	// DBus enablement query.
	needsConverge, err := networkNftUnitNeedsConverge(context.Background())
	if err != nil {
		logx.As().Debug().Err(err).
			Msg("network nft unit migration: failed to probe the loader unit; skipping")
		return false, nil
	}
	return needsConverge, nil
}

// Execute writes the unit, daemon-reloads, and enables it at boot — enabling
// even when the bytes are already current. It never restarts the unit, so live
// policy sets survive and the new ordering applies at the next boot. Failures
// are only warned about, so a stuck host still runs every other command.
func (m *NetworkNftUnitMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	logx.As().Info().Msg("Converging the nftables loader unit on this host (issue #982)")

	if err := ensureNetworkNftUnit(ctx); err != nil {
		logx.As().Warn().Err(err).
			Str("unit", firewall.NetworkNftService).
			Msg("could not converge the nftables loader unit — the weaver tables may not survive a reboot " +
				"alongside an enabled nftables/ufw/firewalld. Check that /usr/lib/systemd/system is " +
				"writable, then verify with: systemctl cat " + firewall.NetworkNftService)
	}
	return nil
}

// Rollback is a no-op; the previous unit is what this migration replaces.
func (m *NetworkNftUnitMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	logx.As().Warn().Msg("Rollback for the network nft loader unit migration is not supported")
	return nil
}
