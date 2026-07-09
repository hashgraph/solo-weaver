// SPDX-License-Identifier: Apache-2.0

// Package policy implements the `solo-provisioner network policy create` verb:
// it renders per-category classification / ACL rules into the `inet weaver`
// nftables table (the workload traffic plane, design §7.2.4), maintains a
// per-policy registry under /etc/solo-provisioner/policies/, and applies the
// full rendered chain to the live kernel with `nft -f` while atomically
// persisting /etc/solo-provisioner/network-weaver.nft.
//
// Like internal/network/firewall, this is a generic, category-agnostic
// primitive — it knows nothing about block/consensus/mirror/relay nodes; the
// CLI is statusz-agnostic and takes CIDRs and class names directly. It is the
// sibling of internal/network/firewall (the `inet host` node firewall) and
// deliberately shares that package's on-disk artifact conventions, the
// cross-command apply lock, and the boot oneshot unit — but targets a different
// table with an opposite lifecycle: `inet weaver` set membership churns
// continuously as the daemon monitor (TS_3) rewrites it from statusz, whereas
// the chain structure and port sets rendered here are static for the lifetime
// of the deployment. Today `block node install` is the only caller.
//
// Scope boundary (epic #738): this package implements only `create`. The
// element verbs (add/remove/set/show/delete) are Story 1.3 (#759); overlap
// rejection, the created_at intra-tier tiebreaker, and last-policy service
// teardown are Story 1.5 (#772).
package policy

const (
	// TableName is the nftables table this package owns.
	TableName = "inet weaver"

	// WeaverNftPath is the on-disk artifact replayed at boot by the shared
	// solo-provisioner-network-nft.service oneshot. It lives under /etc (host OS
	// config on the root filesystem) so it is available early at boot, before any
	// late mounts. Extending the oneshot's ExecStart to also load this file is
	// owned by #780; this package only writes the file and ensures the unit is
	// enabled.
	WeaverNftPath = "/etc/solo-provisioner/network-weaver.nft"

	// HostNftPath is the inet host artifact, owned by internal/network/firewall.
	// This package never writes it; it only checks for its presence to decide
	// whether the shared oneshot may be disabled (teardown is Story 1.5 / #791).
	HostNftPath = "/etc/solo-provisioner/network-host.nft"

	// RegistryDir holds one JSON file per policy (design §8.4.7). The registry is
	// the source of truth for the static policy definition and drives the
	// tier-order chain re-render on every create/delete. CIDR membership is NOT
	// stored here — it lives in the live nft sets and is owned by the daemon poll
	// loop (§8.3.1).
	RegistryDir = "/etc/solo-provisioner/policies"

	// NetworkNftService is the shared oneshot unit that loads the network nft
	// tables at boot. It is shared with internal/network/firewall; this package
	// ensures it is installed and enabled on the first policy mutation but never
	// disables it (teardown is Story 1.5 / #791).
	NetworkNftService = "solo-provisioner-network-nft.service"

	// NetworkNftServiceUnitPath is the absolute path where the shared unit file
	// is installed so systemd can discover it.
	NetworkNftServiceUnitPath = "/usr/lib/systemd/system/" + NetworkNftService

	// networkNftServiceTemplate is the embedded unit file, shared with firewall.
	networkNftServiceTemplate = "files/network/solo-provisioner-network-nft.service"

	// LockDir holds the cross-command apply lock, on tmpfs (/run) so it is
	// auto-cleared on reboot. Shared with internal/network/firewall and the
	// daemon poll loop (#754).
	LockDir = "/run/solo-provisioner/network"

	// LockPath is the flock acquired (LOCK_EX) for the duration of any mutating
	// verb, so a hand-run operator command and the daemon poll loop can never
	// interleave nft transactions on the shared network tables.
	//
	// NOTE: these path/service constants intentionally mirror
	// internal/network/firewall by value. Hoisting them into a shared
	// internal/network package is a deliberate follow-up (kept out of this story
	// to avoid churning already-merged firewall code); the values MUST stay in
	// sync until then, which the render/lock tests guard indirectly.
	LockPath = "/run/solo-provisioner/network/.applying"
)
