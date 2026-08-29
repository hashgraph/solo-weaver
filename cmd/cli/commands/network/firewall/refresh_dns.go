// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/automa-saga/logx"
	fw "github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/spf13/cobra"
)

var refreshDNSCmd = &cobra.Command{
	Use:   "refresh-dns",
	Short: "Re-resolve the host firewall's domain names and apply any address changes",
	Long: "Re-resolve every fully-qualified domain name in " + fw.HostConfigPath + " — the `mgmt` rule and any " +
		"allow rule — and, if any of them now point somewhere else, re-render and re-apply the table.\n\n" +
		"Takes no arguments. This is what the " + fw.DNSRefreshTimer + " unit runs every few minutes; run it by " +
		"hand to pick up an address change immediately instead of waiting for the next tick.\n\n" +
		"It writes nothing when every name still resolves to the addresses already in the ruleset, so it is safe to " +
		"run repeatedly. That is the difference from `reapply`, which always reloads the kernel because its job is " +
		"to re-assert a table something else disturbed — at which point the files on disk are already correct and " +
		"only the kernel has diverged.\n\n" +
		"A name that does not resolve is not an error: its last-known addresses are kept, so a resolver outage " +
		"never narrows what a rule admits. A name that has never resolved contributes nothing and is logged. If " +
		"that would leave the `mgmt` rule empty the command fails instead, because an empty @mgmt_addrs under the " +
		"input chain's default drop would take every new SSH connection with it — an allow rule left empty the same " +
		"way is only logged, since it costs one rule's traffic rather than administrative access to the host.\n\n" +
		"Nothing to configure: the names come from the persisted config, and only `mgmt` and declared allow rules " +
		"accept them.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := newManager().RefreshDNS(cmd.Context()); err != nil {
			return err
		}
		logx.As().Info().Msg("host firewall domain names re-resolved")
		return nil
	},
}
