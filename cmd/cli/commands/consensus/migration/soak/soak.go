// SPDX-License-Identifier: Apache-2.0

package soak

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var soakCmd = &cobra.Command{
	Use:   "soak",
	Short: "Manage the consensus-node migration soak watcher",
	Long:  "Start, stop, and inspect the consensus-node migration soak watcher running inside solo-provisioner-daemon.",
	RunE:  common.DefaultRunE,
}

func init() {
	soakCmd.AddCommand(startCmd)
	soakCmd.AddCommand(stopCmd)
	soakCmd.AddCommand(statusCmd)
}

// GetCmd returns the soak command group.
func GetCmd() *cobra.Command {
	return soakCmd
}
