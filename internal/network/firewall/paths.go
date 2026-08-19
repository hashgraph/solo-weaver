// SPDX-License-Identifier: Apache-2.0

// Package firewall implements the node-agnostic `solo-provisioner network
// firewall` scope: the `inet weaver-host-firewall` nftables table that protects the bare-metal
// host (SSH/mgmt allowlist, ICMP policy, in-cluster host-service ports).
//
// It is a generic primitive — it knows nothing about block/consensus/mirror/
// relay nodes. Orchestration (wiring create into `kube cluster install`,
// teardown into `kube cluster uninstall`) is owned by the host/cluster layer
// (#777 → #778/#791); this package only implements the verbs.
//
// The `inet weaver-host-firewall` table is kept deliberately separate from `inet weaver-workload-policy` (the
// BN workload plane). The two tables have opposite lifecycles: `inet weaver-host-firewall` is
// set once and rarely changes, while `inet weaver-workload-policy` churns continuously as the
// daemon rewrites set elements.
package firewall

const (
	// TableName is the nftables table this package owns.
	TableName = "inet weaver-host-firewall"

	// HostNftPath is the on-disk artifact replayed at boot by the shared
	// solo-provisioner-network-nft.service oneshot (authored by #780). It lives
	// under /etc (host OS config on the root filesystem) — not /opt/solo/weaver,
	// which may be a late mount and would leave the firewall unloaded early at
	// boot.
	HostNftPath = "/etc/solo-provisioner/network-weaver-host-firewall.nft"

	// HostConfigPath is the declarative config this table is rendered from, and
	// the source of truth for every mutating verb. It sits beside the nft
	// artifact and holds exactly the schema `network firewall create --from-file`
	// accepts, so `show --output yaml` re-applied through --from-file is a no-op
	// by construction.
	//
	// A single file rather than one per rule: a change to the management
	// allowlist must be all-or-nothing, and a partial write across several files
	// could leave a host reachable by nobody.
	HostConfigPath = "/etc/solo-provisioner/network-weaver-host-firewall.yaml"

	// HostConfigPrevSuffix is appended to HostConfigPath to name the retained
	// previous generation of the config. Derived by suffix rather than declared
	// as an independent path so the two can never be pointed at different
	// directories.
	HostConfigPrevSuffix = ".prev"

	// HostConfigPrevPath is the generation of the config immediately before the
	// one currently applied, retained on every apply so a lost or truncated
	// state file has a recovery path that keeps named allow rules.
	//
	// Deliberately one generation deep: this is a recovery artifact, not a
	// version history. History belongs in the operator's own repository, holding
	// the output of `network firewall show --output yaml`.
	//
	// It is written only when the config it replaces parses, so the invariant is
	// "absent, or a loadable config exactly one generation back" — a retained
	// copy that is itself corrupt would be worthless for recovery, and worse than
	// worthless if an operator trusted it.
	HostConfigPrevPath = HostConfigPath + HostConfigPrevSuffix

	// WeaverNftPath is the inet weaver-workload-policy artifact, owned by `block node install`
	// (TS_2 #743). This package never writes it; it only checks for its presence
	// to decide whether the shared oneshot may be disabled (teardown is #791).
	WeaverNftPath = "/etc/solo-provisioner/network-weaver-workload-policy.nft"

	// NetworkNftService is the oneshot unit that loads network-weaver-host-firewall.nft at boot
	// and is restarted on every live mutation so the kernel and the on-disk file
	// are always in sync. This package authors, installs, and enables the unit;
	// it never disables it — that is orchestrated by `kube cluster uninstall`
	// (#791). The unit is extended by #780 to also load network-weaver-workload-policy.nft.
	NetworkNftService = "solo-provisioner-network-nft.service"

	// NetworkNftServiceUnitPath is the absolute path where the unit file is
	// installed so systemd can discover it.
	NetworkNftServiceUnitPath = "/usr/lib/systemd/system/" + NetworkNftService

	// networkNftServiceTemplate is the embedded unit file rendered and written on
	// first mutation.
	networkNftServiceTemplate = "files/network/solo-provisioner-network-nft.service"

	// LockDir holds the cross-command apply lock. It lives on tmpfs (/run) so it
	// is auto-cleared on reboot and leaves nothing behind on uninstall.
	LockDir = "/run/solo-provisioner/network"

	// LockPath is the flock acquired (LOCK_EX) for the duration of any mutating
	// verb, so a hand-run operator command and the daemon poll loop (#754) can
	// never interleave nft transactions.
	LockPath = "/run/solo-provisioner/network/.applying"

	// hostNftTemplate is the embedded template that renders the full `inet weaver-host-firewall`
	// table. Lives under internal/templates/files, embedded via that package.
	hostNftTemplate = "files/network/network-weaver-host-firewall.nft.tmpl"
)
