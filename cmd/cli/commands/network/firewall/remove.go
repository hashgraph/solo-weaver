// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a single --mgmt-cidr or --in-cluster-port",
	RunE: func(cmd *cobra.Command, _ []string) error {
		mgr := newManager()
		switch {
		case cmd.Flags().Changed("mgmt-cidr"):
			return mgr.RemoveMgmtCIDR(cmd.Context(), flagMgmtCIDR)
		case cmd.Flags().Changed("in-cluster-port"):
			return mgr.RemovePort(cmd.Context(), flagInClusterPort)
		default:
			return errorx.IllegalArgument.New("one of --mgmt-cidr or --in-cluster-port is required")
		}
	},
}

func init() {
	removeCmd.Flags().StringVar(&flagMgmtCIDR, "mgmt-cidr", "", "A single management CIDR to remove")
	removeCmd.Flags().IntVar(&flagInClusterPort, "in-cluster-port", 0, "A single in-cluster host-service port to remove")
	removeCmd.MarkFlagsMutuallyExclusive("mgmt-cidr", "in-cluster-port")
}
