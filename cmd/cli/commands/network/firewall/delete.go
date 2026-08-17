// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete one allow rule (--name), or the whole table (--all)",
	Long: "Delete a single named allow rule with --name, or tear the whole `inet weaver-host-firewall` table down " +
		"with --all (the default when no flag is given, which is what this verb has always done).\n\n" +
		"The reserved blocks cannot be deleted individually — clear their addresses instead (`network firewall set " +
		"--name mgmt --cidrs \"\"`). --all removes the table and " +
		"/etc/solo-provisioner/network-weaver-host-firewall.{nft,yaml}, leaving the host with no weaver-managed " +
		"firewall at all, so it asks for confirmation in an interactive session. It does NOT disable the shared " +
		"solo-provisioner-network-nft.service (shared with `inet weaver-workload-policy`); host-level teardown is " +
		"orchestrated by `kube cluster uninstall`.",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := newManager()

		if cmd.Flags().Changed("name") {
			if err := mgr.DeleteRule(cmd.Context(), flagName); err != nil {
				return err
			}
			logx.As().Info().Str("rule", flagName).Msg("allow rule removed from the host firewall")
			return nil
		}

		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return err
		}
		// No flag at all means --all: that is the behaviour this verb shipped with,
		// and the callers relying on it are non-interactive, where ShouldPrompt is
		// false and nothing changes for them.
		if prompt.ShouldPrompt(force) {
			ok, err := prompt.RunConfirm(
				"Delete the whole host firewall?",
				"This removes the inet weaver-host-firewall table and its on-disk artifacts. The host will have no "+
					"weaver-managed firewall — including no management allowlist — until one is created again.",
				false)
			if err != nil {
				return err
			}
			if !ok {
				return errorx.IllegalState.New("aborted: host firewall not deleted")
			}
		}

		if err := mgr.Delete(cmd.Context()); err != nil {
			return err
		}
		logx.As().Info().Msg("inet weaver-host-firewall firewall removed")
		return nil
	},
}

func init() {
	deleteCmd.Flags().StringVar(&flagName, "name", "", "Named allow rule to delete (the reserved blocks cannot be deleted)")
	deleteCmd.Flags().BoolVar(&flagAll, "all", false, "Delete the whole table and its on-disk artifacts (the default when --name is omitted)")
	deleteCmd.MarkFlagsMutuallyExclusive("name", "all")
}
