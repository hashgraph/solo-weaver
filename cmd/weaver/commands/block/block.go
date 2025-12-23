// SPDX-License-Identifier: Apache-2.0

package block

import (
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/block/node"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/spf13/cobra"
)

var (
	flagProfile string

	blockCmd = &cobra.Command{
		Use:   "block",
		Short: "Manage a Hedera Block Node & its components",
		Long:  "Manage a Hedera Block Node & its components",
		RunE:  common.DefaultRunE, // ensure we have a default action to make it runnable and sub-commands would inherit parent flags
	}
)

func init() {
	common.FlagProfile.SetVarP(blockCmd, &flagProfile, false)
	blockCmd.AddCommand(node.GetCmd())
}

func GetCmd() *cobra.Command {
	return blockCmd
}
