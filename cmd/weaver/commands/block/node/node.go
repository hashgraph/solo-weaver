package node

import (
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/internal/core"
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
