package blockcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/internal/core"
)

var (
	nodeType = core.NodeTypeBlock

	flagProfile    string
	flagValuesFile string

	subCommands = []*cobra.Command{
		blockNodeCheckCmd,
		blockNodeInstallCmd,
	}

	blockCmd = &cobra.Command{
		Use:     "block-node",
		Aliases: []string{"blocknode"},
		Short:   "Manage lifecycle of a Hedera Block Node",
		Long:    "Manage lifecycle of a Hedera Block Node",
	}
)

func init() {
	// add profile flag to all sub commands
	for _, cmd := range subCommands {
		cmd.Flags().StringVarP(&flagProfile, "profile", "p", "",
			fmt.Sprintf("Deployment profiles %s", core.AllProfiles()))
		_ = cmd.MarkFlagRequired("profile")

	}
	blockNodeInstallCmd.Flags().StringVarP(&flagValuesFile, "values", "f", "",
		fmt.Sprintf("Values file"))

	blockCmd.AddCommand(subCommands...)
}

func Get() *cobra.Command {
	return blockCmd
}
