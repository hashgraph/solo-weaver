// SPDX-License-Identifier: Apache-2.0

package service

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage the solo-provisioner-daemon systemd service",
	Long:  "Install, uninstall, or check the solo-provisioner-daemon systemd service unit file.",
	RunE:  common.DefaultRunE,
}

func init() {
	serviceCmd.AddCommand(checkCmd)
	serviceCmd.AddCommand(installCmd)
	serviceCmd.AddCommand(uninstallCmd)
}

func GetCmd() *cobra.Command {
	return serviceCmd
}
