// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/migration"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/network"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/node"
	"github.com/spf13/cobra"
)

var consensusCmd = &cobra.Command{
	Use:   "consensus",
	Short: "Manage consensus-node lifecycle and migration",
	Long:  "Commands for managing consensus-node operations including migration soak lifecycle.",
	RunE:  common.DefaultRunE,
}

func init() {
	// Nothing in the consensus stack is production-ready yet, but solo-provisioner
	// ships to production block-node operators. Gate the whole group behind the
	// experimental feature gate so it is hidden from --help and refuses to run
	// unless the operator opts in (--experimental or SOLO_PROVISIONER_ENABLE_CONSENSUS).
	common.GateExperimental(consensusCmd, "consensus")

	consensusCmd.AddCommand(migration.GetCmd())
	consensusCmd.AddCommand(node.GetCmd())
	consensusCmd.AddCommand(network.GetCmd())
}

// GetCmd returns the consensus command group.
func GetCmd() *cobra.Command {
	return consensusCmd
}
