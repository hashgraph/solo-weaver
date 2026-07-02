// SPDX-License-Identifier: Apache-2.0

// Package firewall implements the node-agnostic `solo-provisioner network
// firewall` scope: the `inet host` nftables table that protects the bare-metal
// host (SSH/mgmt allowlist, ICMP policy, in-cluster host-service ports).
//
// It is a generic primitive — it knows nothing about block/consensus/mirror/
// relay nodes. Orchestration (wiring create into `kube cluster install`,
// teardown into `kube cluster uninstall`) is owned by the host/cluster layer
// (#777 → #778/#791); this package only implements the verbs.
//
// The `inet host` table is kept deliberately separate from `inet weaver` (the
// BN workload plane). The two tables have opposite lifecycles: `inet host` is
// set once and rarely changes, while `inet weaver` churns continuously as the
// daemon rewrites set elements.
package firewall

const (
	// TableName is the nftables table this package owns.
	TableName = "inet host"

	// HostNftPath is the on-disk artifact replayed at boot by the shared
	// solo-provisioner-network-nft.service oneshot (authored by #780). It lives
	// under /etc (host OS config on the root filesystem) — not /opt/solo/weaver,
	// which may be a late mount and would leave the firewall unloaded early at
	// boot.
	HostNftPath = "/etc/solo-provisioner/network-host.nft"

	// WeaverNftPath is the inet weaver artifact, owned by `block node install`
	// (TS_2 #743). This package never writes it; it only checks for its presence
	// to decide whether the shared oneshot may be disabled (teardown is #791).
	WeaverNftPath = "/etc/solo-provisioner/network-weaver.nft"

	// NetworkNftService is the oneshot unit that loads network-host.nft at boot
	// and is restarted on every live mutation so the kernel and the on-disk file
	// are always in sync. This package authors, installs, and enables the unit;
	// it never disables it — that is orchestrated by `kube cluster uninstall`
	// (#791). The unit is extended by #780 to also load network-weaver.nft.
	NetworkNftService = "solo-provisioner-network-nft.service"

	// NetworkNftServiceUnitPath is the absolute path where the unit file is
	// installed so systemd can discover it.
	NetworkNftServiceUnitPath = "/usr/lib/systemd/system/" + NetworkNftService

	// networkNftServiceTemplate is the embedded unit file rendered and written on
	// first mutation.
	networkNftServiceTemplate = "files/network/solo-provisioner-network-nft.service"

	// NftablesDropInDir is where the nftables.service drop-in is installed.
	// Drop-ins in /etc/systemd/system/ take precedence and survive package
	// upgrades of the nftables package itself.
	NftablesDropInDir = "/etc/systemd/system/nftables.service.d"

	// NftablesDropInPath is the drop-in file that makes nftables.service pull
	// in solo-provisioner-network-nft.service whenever it activates — so a
	// mid-run nftables flush (e.g. triggered by kube cluster install's preflight)
	// is always followed by a re-apply of our inet host rules.
	NftablesDropInPath = NftablesDropInDir + "/solo-provisioner.conf"

	// nftablesDropInTemplate is the embedded drop-in file content.
	nftablesDropInTemplate = "files/network/nftables-dropin.conf"

	// LockDir holds the cross-command apply lock. It lives on tmpfs (/run) so it
	// is auto-cleared on reboot and leaves nothing behind on uninstall.
	LockDir = "/run/solo-provisioner/network"

	// LockPath is the flock acquired (LOCK_EX) for the duration of any mutating
	// verb, so a hand-run operator command and the daemon poll loop (#754) can
	// never interleave nft transactions.
	LockPath = "/run/solo-provisioner/network/.applying"

	// hostNftTemplate is the embedded template that renders the full `inet host`
	// table. Lives under internal/templates/files, embedded via that package.
	hostNftTemplate = "files/network/network-host.nft.tmpl"
)
