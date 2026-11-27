// SPDX-License-Identifier: Apache-2.0

package drift

// adviceFirewall is the same for every kind of host firewall change: the
// right repair depends on what is on the host and on what the run itself did
// to the table, which a warning line cannot know.
const adviceFirewall = "firewall: to repair these, see docs/commands/network/firewall.md, " +
	"\"Changes made outside solo-provisioner are reported\". " +
	"Every `solo-provisioner network firewall` command that applies a table replaces all of it, " +
	"as can a reboot or this run, so an entry only in the live table, such as a management address " +
	"added by hand, is lost unless it is first added to the config"

// FirewallAdvice points to the repair of the host firewall changes among
// changes, in one line.
func FirewallAdvice(changes []Change) []string {
	for _, c := range changes {
		if c.Component == firewallComponent {
			return []string{adviceFirewall}
		}
	}
	return nil
}
