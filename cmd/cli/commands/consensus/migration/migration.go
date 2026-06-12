// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/migration/soak"
	"github.com/spf13/cobra"
)

var migrationCmd = &cobra.Command{
	Use:   "migration",
	Short: "Manage consensus-node migration lifecycle",
	Long:  "Commands for managing the consensus-node migration soak watcher inside solo-provisioner-daemon.",
	RunE:  common.DefaultRunE,
}

func init() {
	migrationCmd.AddCommand(soak.GetCmd())
}

// GetCmd returns the migration command group.
func GetCmd() *cobra.Command {
	return migrationCmd
}
