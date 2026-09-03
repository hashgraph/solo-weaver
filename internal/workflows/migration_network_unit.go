// SPDX-License-Identifier: Apache-2.0

// migration_network_unit.go converges the boot units that replay network state
// after a reboot, on hosts that only receive a new binary.
// See docs/dev/traffic-shaper.md.

package workflows

import (
	"context"
	"os"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
)

// networkUnitGeteuid is a seam so tests can script the caller's privilege.
var networkUnitGeteuid = os.Geteuid

// networkUnitSpec describes one boot unit to converge. probe and ensure are also
// the seams tests use to script host state.
type networkUnitSpec struct {
	id          string
	description string
	service     string
	// failureHint states the operator-visible consequence of a failed converge.
	failureHint string
	// probe is read-only and filesystem-only.
	probe  func() (bool, error)
	ensure func(context.Context) error
}

// NetworkUnitMigration converges one network boot unit when it drifts from the
// embedded copy, or when it is byte-current but disabled at boot.
type NetworkUnitMigration struct {
	spec networkUnitSpec
}

// NewNetworkNftUnitMigration constructs the nftables loader unit migration.
func NewNetworkNftUnitMigration() *NetworkUnitMigration {
	return &NetworkUnitMigration{spec: networkUnitSpec{
		id: "network-nft-loader-unit",
		description: "Converge the solo-provisioner-network-nft.service loader unit on already-provisioned " +
			"hosts so boot ordering changes reach hosts that only receive a new binary, and re-enable " +
			"it when it is present but would not start at boot",
		service: firewall.NetworkNftService,
		failureHint: "the weaver tables may not survive a reboot alongside an enabled " +
			"nftables/ufw/firewalld",
		probe:  firewall.NetworkNftUnitNeedsConverge,
		ensure: firewall.EnsureNetworkNftUnit,
	}}
}

// NewNetworkShaperUnitMigration constructs the bandwidth-shaper unit migration.
func NewNetworkShaperUnitMigration() *NetworkUnitMigration {
	return &NetworkUnitMigration{spec: networkUnitSpec{
		id: "network-bandwidth-shaper-unit",
		description: "Converge the solo-provisioner-bandwidth-shaper.service unit on already-provisioned " +
			"hosts so boot ordering changes reach hosts that only receive a new binary, and re-enable " +
			"it when it is present but would not start at boot",
		service: shape.TcEgressService,
		failureHint: "the egress HTB hierarchy may not be replayed after a reboot on a " +
			"netplan-created bond, bridge or VLAN",
		probe:  shape.TcEgressUnitNeedsConverge,
		ensure: shape.EnsureTcEgressUnit,
	}}
}

// ID implements migration.Migration. No version suffix — not tied to a release boundary.
func (m *NetworkUnitMigration) ID() string { return m.spec.id }

// Description implements migration.Migration.
func (m *NetworkUnitMigration) Description() string { return m.spec.description }

// Applies reports whether the unit needs converging. A probe error counts as
// "no" and only warns (#1058).
func (m *NetworkUnitMigration) Applies(mctx *migration.Context) (bool, error) {
	// The write lands under /usr/lib, so an unprivileged caller could only fail.
	if euid := networkUnitGeteuid(); euid != 0 {
		logx.As().Debug().Int("euid", euid).Str("unit", m.spec.service).
			Msg("network unit migration: not root; leaving the unit to a privileged invocation")
		return false, nil
	}

	needsConverge, err := m.spec.probe()
	if err != nil {
		logx.As().Warn().Err(err).Str("unit", m.spec.service).Str("hint", m.spec.failureHint).
			Msg("network unit migration: failed to probe the unit; skipping this invocation")
		return false, nil
	}
	return needsConverge, nil
}

// Execute writes, daemon-reloads, and enables the unit — never restarts it.
// Failures only warn, so a stuck host still runs every other command.
func (m *NetworkUnitMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	logx.As().Info().Str("unit", m.spec.service).Msg("Converging the network boot unit on this host")

	if err := m.spec.ensure(ctx); err != nil {
		logx.As().Warn().Err(err).Str("unit", m.spec.service).
			Msgf("could not converge the unit — %s. Check that /usr/lib/systemd/system is "+
				"writable, then verify with: systemctl cat %s", m.spec.failureHint, m.spec.service)
	}
	return nil
}

// Rollback is a no-op; the previous unit is what this migration replaces.
func (m *NetworkUnitMigration) Rollback(ctx context.Context, mctx *migration.Context) error {
	logx.As().Warn().Str("unit", m.spec.service).Msg("Rollback for the network unit migration is not supported")
	return nil
}
