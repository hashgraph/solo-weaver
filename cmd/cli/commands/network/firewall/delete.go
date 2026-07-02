// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Remove the `inet host` table and its on-disk artifact",
	Long: "Remove the `inet host` table and /etc/solo-provisioner/network-host.nft. This does NOT " +
		"disable the shared solo-provisioner-network-nft.service (shared with `inet weaver`); host-level " +
		"teardown is orchestrated by `kube cluster uninstall`.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := newManager().Delete(cmd.Context()); err != nil {
			return err
		}
		logx.As().Info().Msg("inet host firewall removed")
		return nil
	},
}
