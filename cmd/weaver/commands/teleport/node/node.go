// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/spf13/cobra"
)

var (
	flagStopOnError     bool
	flagRollbackOnError bool
	flagContinueOnError bool

	// Teleport node agent configuration flags
	flagNodeAgentToken     string
	flagNodeAgentProxyAddr string

	nodeCmd = &cobra.Command{
		Use:   "node",
		Short: "Manage Teleport node agent for SSH access",
		Long:  "Manage Teleport node agent for SSH access to the host machine",
		RunE:  common.DefaultRunE,
	}
)

func init() {
	nodeCmd.PersistentFlags().StringVar(&flagNodeAgentToken, "token", "", "Join token for Teleport node agent SSH access (required)")
	nodeCmd.PersistentFlags().StringVar(&flagNodeAgentProxyAddr, "proxy", "", "Teleport proxy address (required, e.g., proxy.example.com or proxy.example.com:443)")

	nodeCmd.AddCommand(installCmd)
	nodeCmd.AddCommand(uninstallCmd)
}

func GetCmd() *cobra.Command {
	return nodeCmd
}
