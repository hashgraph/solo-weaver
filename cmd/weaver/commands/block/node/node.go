// SPDX-License-Identifier: Apache-2.0

package node

import (
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/spf13/cobra"
)

var (
	nodeType = core.NodeTypeBlock

	flagValuesFile string

	nodeCmd = &cobra.Command{
		Use:   "node",
		Short: "Manage lifecycle of a Hedera Block Node",
		Long:  "Manage lifecycle of a Hedera Block Node",
	}
)

func init() {
	nodeCmd.AddCommand(checkCmd, installCmd)
}

func GetCmd() *cobra.Command {
	return nodeCmd
}
