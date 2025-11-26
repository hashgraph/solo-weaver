package block

import (
	"fmt"

	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/cmd/weaver/commands/block/node"
	"golang.hedera.com/solo-weaver/internal/core"
)

var (
	flagProfile string

	blockCmd = &cobra.Command{
		Use:   "block",
		Short: "Manage a Hedera Block Node & its components",
		Long:  "Manage a Hedera Block Node & its components",
	}
)

func init() {
	blockCmd.PersistentFlags().StringVarP(&flagProfile, "profile", "p", "",
		fmt.Sprintf("Deployment profiles %s", core.AllProfiles()))
	blockCmd.AddCommand(node.GetCmd())
}

func GetCmd() *cobra.Command {
	return blockCmd
}
