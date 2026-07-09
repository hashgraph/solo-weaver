// SPDX-License-Identifier: Apache-2.0

// Package shape implements tc HTB hierarchy persistence for the `$EGRESS`
// physical NIC. It renders the boot-replay script and manages the
// solo-provisioner-tc-egress.service oneshot unit (design §8.3.2).
//
// Scope boundary (TS_2 #742): this package only covers reboot persistence —
// rendering the tc-egress.sh script with the NIC name interpolated at install
// time and installing/enabling the oneshot unit. The full `network shape`
// CLI verb (Story 1.4) will extend this package with bandwidth-parameterised
// rendering and the per-class rate/ceil/prio API.
//
// The $VETH HTB is deliberately NOT persisted here: the veth interface does
// not survive reboot (Cilium recreates it on pod start), so persisting its
// qdisc would be meaningless. The daemon's pod-lifecycle watcher reinstalls
// the $VETH HTB on the next pod create event (TS_3).
package shape

const (
	// TcEgressScriptPath is the shell script that replays the $EGRESS HTB
	// hierarchy at boot. It lives under /usr/local/sbin (system admin tools,
	// root-executable) — not /etc, since it is an executable rather than config.
	TcEgressScriptPath = "/usr/local/sbin/solo-provisioner-tc-egress.sh"

	// TcEgressService is the systemd oneshot unit name that executes
	// TcEgressScriptPath at boot, before solo-provisioner-daemon.service starts.
	TcEgressService = "solo-provisioner-tc-egress.service"

	// TcEgressServiceUnitPath is the absolute path where the unit file is
	// installed so systemd can discover it.
	TcEgressServiceUnitPath = "/usr/lib/systemd/system/" + TcEgressService

	// tcEgressScriptTemplate is the embedded Go template for the tc-egress.sh
	// boot script. The single interpolation point is {{.NIC}}, the egress
	// interface name detected at install time.
	tcEgressScriptTemplate = "files/network/solo-provisioner-tc-egress.sh.tmpl"

	// tcEgressServiceTemplate is the static embedded unit file installed at
	// TcEgressServiceUnitPath on first use.
	tcEgressServiceTemplate = "files/network/solo-provisioner-tc-egress.service"
)
