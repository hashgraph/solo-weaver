// SPDX-License-Identifier: Apache-2.0

// Package shape implements tc HTB hierarchy persistence and the `network shape`
// CLI verb. It renders the boot-replay script (solo-provisioner-tc-egress.sh)
// with bandwidth-parameterised class configurations, manages the
// solo-provisioner-tc-egress.service oneshot unit, and applies live tc class
// changes to the $EGRESS physical NIC (design §8.3.2, §8.4.3, §5).
//
// The $VETH HTB is deliberately NOT persisted here: the veth interface does not
// survive reboot (Cilium recreates it on pod start), so persisting its qdisc
// would be meaningless. The daemon's pod-lifecycle watcher (TS_3) reinstalls
// the $VETH HTB on the next pod create event by reading the class configs stored
// under ClassConfigDir.
package shape

const (
	// TcEgressScriptPath is the shell script that replays the $EGRESS HTB
	// hierarchy at boot. Lives under /usr/local/sbin (root-executable tools).
	TcEgressScriptPath = "/usr/local/sbin/solo-provisioner-tc-egress.sh"

	// TcEgressService is the systemd oneshot unit that executes TcEgressScriptPath
	// at boot, before solo-provisioner-daemon.service starts.
	TcEgressService = "solo-provisioner-tc-egress.service"

	// TcEgressServiceUnitPath is the absolute path where the unit file is
	// installed so systemd can discover it.
	TcEgressServiceUnitPath = "/usr/lib/systemd/system/" + TcEgressService

	// tcEgressScriptTemplate is the embedded Go template for the tc-egress.sh
	// boot script. Interpolation points: {{.NIC}}, {{.Device.*}}, {{range .Classes}}.
	tcEgressScriptTemplate = "files/network/solo-provisioner-tc-egress.sh.tmpl"

	// tcEgressServiceTemplate is the static embedded unit file.
	tcEgressServiceTemplate = "files/network/solo-provisioner-tc-egress.service"

	// ShapeConfigDir is the root of the shape configuration tree persisted by
	// the `network shape` CLI verb.
	ShapeConfigDir = "/etc/solo-provisioner/network/shape"

	// DeviceConfigDir holds one JSON file per configured tc device ("ingress"
	// or "egress"), describing the root qdisc rate and default class.
	DeviceConfigDir = ShapeConfigDir + "/devices"

	// ClassConfigDir holds one JSON file per configured tc class, describing its
	// bandwidth parameters (rate, ceil, prio). The daemon watcher (TS_3) reads
	// this directory to reinstall $VETH classes on each pod create event.
	ClassConfigDir = ShapeConfigDir + "/classes"

	// ShapeLockDir is the directory containing the tc apply lock (on tmpfs so
	// it is auto-cleared on reboot).
	ShapeLockDir = "/run/solo-provisioner/network"

	// ShapeLockPath is the flock acquired (LOCK_EX) for the duration of any
	// tc mutating verb, preventing concurrent modifications.
	ShapeLockPath = ShapeLockDir + "/.tc-applying"

	// policyRegistryDir mirrors internal/network/policy.RegistryDir. Duplicated
	// here to avoid a cross-package import; the values must stay in sync.
	policyRegistryDir = "/etc/solo-provisioner/policies"
)
