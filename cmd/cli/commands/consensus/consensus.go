// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/keys"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/migration"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/network"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/node"
	"github.com/hashgraph/solo-weaver/internal/featuregate"
	"github.com/spf13/cobra"
)

var consensusCmd = &cobra.Command{
	Use:   "consensus",
	Short: "Manage consensus-node lifecycle and migration",
	Long:  "Commands for managing consensus-node operations including migration soak lifecycle.",
	RunE:  common.DefaultRunE,
}

func init() {
	// The consensus stack isn't production-ready, so gate the whole group behind the
	// ConsensusNodeOperatorManaged alpha feature (hidden until the operator opts in).
	common.GateFeature(consensusCmd, featuregate.ConsensusNodeOperatorManaged)

	consensusCmd.AddCommand(migration.GetCmd())
	consensusCmd.AddCommand(node.GetCmd())
	consensusCmd.AddCommand(network.GetCmd())
	consensusCmd.AddCommand(keys.GetCmd())
}

// GetCmd returns the consensus command group.
func GetCmd() *cobra.Command {
	return consensusCmd
}
