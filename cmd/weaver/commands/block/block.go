// SPDX-License-Identifier: Apache-2.0

package block

import (
	"fmt"

	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/block/node"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/spf13/cobra"
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
	blockCmd.PersistentFlags().StringVarP(&flagProfile,
		common.FlagProfileName, common.FlagProfileNameShort, "",
		fmt.Sprintf("Deployment profiles %s", core.AllProfiles()))

	_ = blockCmd.MarkPersistentFlagRequired(common.FlagProfileName)

	blockCmd.AddCommand(node.GetCmd())
}

func GetCmd() *cobra.Command {
	return blockCmd
}
