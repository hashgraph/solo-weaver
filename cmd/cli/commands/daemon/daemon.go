// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/daemon/service"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the solo-provisioner-daemon process",
	Long:  "Manage the solo-provisioner-daemon long-running background process and its systemd service.",
	RunE:  common.DefaultRunE,
}

func init() {
	daemonCmd.AddCommand(service.GetCmd())
}

func GetCmd() *cobra.Command {
	return daemonCmd
}
