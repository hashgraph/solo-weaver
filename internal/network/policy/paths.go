// SPDX-License-Identifier: Apache-2.0

// Package policy implements the `solo-provisioner network policy` verb family:
// it renders per-category classification / ACL rules into the `inet weaver`
// nftables table (the workload traffic plane), maintains a per-policy registry
// under /etc/solo-provisioner/policies/, and applies the full rendered chain to
// the live kernel with `nft -f` while atomically persisting
// /etc/solo-provisioner/network-weaver.nft.
package policy

const (
	// TableName is the nftables table this package owns.
	TableName = "inet weaver"

	// WeaverNftPath is the on-disk artifact replayed at boot by the shared
	// solo-provisioner-network-nft.service oneshot. It lives under /etc (host OS
	// config on the root filesystem) so it is available early at boot, before any
	// late mounts. This package only writes the file and ensures the unit is
	// enabled.
	WeaverNftPath = "/etc/solo-provisioner/network-weaver.nft"

	// HostNftPath is the inet host artifact, owned by internal/network/firewall.
	// This package never writes it; it only checks for its presence to decide
	// whether the shared oneshot may be disabled.
	HostNftPath = "/etc/solo-provisioner/network-host.nft"

	// RegistryDir holds one JSON file per policy. The registry is the source of
	// truth for the static policy definition and drives the tier-order chain
	// re-render on every create/delete. CIDR membership is NOT stored here — it
	// lives in the live nft sets and is owned by the daemon poll loop.
	RegistryDir = "/etc/solo-provisioner/policies"

	// NetworkNftService is the shared oneshot unit that loads the network nft
	// tables at boot. It is shared with internal/network/firewall; this package
	// ensures it is installed and enabled on the first policy mutation but never
	// disables it.
	NetworkNftService = "solo-provisioner-network-nft.service"

	// NetworkNftServiceUnitPath is the absolute path where the shared unit file
	// is installed so systemd can discover it.
	NetworkNftServiceUnitPath = "/usr/lib/systemd/system/" + NetworkNftService

	// networkNftServiceTemplate is the embedded unit file, shared with firewall.
	networkNftServiceTemplate = "files/network/solo-provisioner-network-nft.service"

	// LockDir holds the cross-command apply lock, on tmpfs (/run) so it is
	// auto-cleared on reboot. Shared with internal/network/firewall and the
	// daemon poll loop.
	LockDir = "/run/solo-provisioner/network"

	// LockPath is the flock acquired (LOCK_EX) for the duration of any mutating
	// verb, so a hand-run operator command and the daemon poll loop can never
	// interleave nft transactions on the shared network tables.
	//
	// NOTE: these path/service constants intentionally mirror
	// internal/network/firewall by value. Hoisting them into a shared
	// internal/network package is a deliberate follow-up (kept out of scope here
	// to avoid churning already-merged firewall code); the values MUST stay in
	// sync until then, which the render/lock tests guard indirectly.
	LockPath = "/run/solo-provisioner/network/.applying"
)
