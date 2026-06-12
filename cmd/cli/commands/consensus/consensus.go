// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/migration"
	"github.com/spf13/cobra"
)

var consensusCmd = &cobra.Command{
	Use:   "consensus",
	Short: "Manage consensus-node lifecycle and migration",
	Long:  "Commands for managing consensus-node operations including migration soak lifecycle.",
	RunE:  common.DefaultRunE,
}

func init() {
	consensusCmd.AddCommand(migration.GetCmd())
}

// GetCmd returns the consensus command group.
func GetCmd() *cobra.Command {
	return consensusCmd
}
