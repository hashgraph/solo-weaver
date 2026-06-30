// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var setCmd = &cobra.Command{
	Use:   "set",
	Short: "Atomically replace the full --mgmt-cidrs and/or --in-cluster-ports list",
	RunE: func(cmd *cobra.Command, _ []string) error {
		var mgmt []string
		var ports []int

		// A nil slice leaves that dimension unchanged; a changed flag (even with
		// an empty value) replaces it.
		if cmd.Flags().Changed("mgmt-cidrs") {
			mgmt = flagMgmtCIDRs
			if mgmt == nil {
				mgmt = []string{}
			}
		}
		if cmd.Flags().Changed("in-cluster-ports") {
			ports = flagInClusterPorts
			if ports == nil {
				ports = []int{}
			}
		}

		if mgmt == nil && ports == nil {
			return errorx.IllegalArgument.New("at least one of --mgmt-cidrs or --in-cluster-ports is required")
		}

		return newManager().Set(cmd.Context(), mgmt, ports)
	},
}

func init() {
	setCmd.Flags().StringSliceVar(&flagMgmtCIDRs, "mgmt-cidrs", nil, "Full management allowlist (comma-separated; replaces the existing list)")
	setCmd.Flags().IntSliceVar(&flagInClusterPorts, "in-cluster-ports", nil, "Full in-cluster host-service port list (comma-separated; replaces the existing list)")
}
