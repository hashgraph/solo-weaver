// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/automa-saga/logx"
	fw "github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/spf13/cobra"
)

var reapplyCmd = &cobra.Command{
	Use:   "reapply",
	Short: "Re-apply the persisted `inet weaver-host-firewall` config as-is",
	Long: "Re-render and re-apply the host firewall from its persisted config at " + fw.HostConfigPath + ", " +
		"without changing it.\n\n" +
		"Takes no arguments. This is the verb for \"apply what is already there\" — re-asserting the table after " +
		"something else on the host disturbed it, or after recovering the config from " + fw.HostConfigPrevPath +
		". Because it states no intent, it does not record an enable/disable decision, so a later `block node " +
		"reconfigure` behaves exactly as if it had not been run.\n\n" +
		"There is deliberately no way to point it at a file: given one it would just be `create --from-file` under " +
		"a second name, and the two would then disagree about what the persisted state is. To apply a file, use " +
		"`create --from-file <path> --force`.\n\n" +
		"Only the `inet weaver-host-firewall` table is replaced — the rendered ruleset scopes its flush to that " +
		"table, so any third-party nftables table on the host is left alone. If no config is persisted the command " +
		"fails rather than applying a default table, since a default-drop policy with an empty management allowlist " +
		"would lock the host out.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := newManager().Reapply(cmd.Context()); err != nil {
			return err
		}
		logx.As().Info().Msg("inet weaver-host-firewall firewall re-applied from the persisted config")
		return nil
	},
}
