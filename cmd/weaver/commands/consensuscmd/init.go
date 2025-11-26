package consensuscmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/internal/core"
)

var (
	nodeType = core.NodeTypeConsensus

	flagProfile string

	subCommands = []*cobra.Command{
		consensusNodeCheckCmd,
		consensusNodeInstallCmd,
	}

	consensusCmd = &cobra.Command{
		Use:   "consensus-node",
		Short: "Manage lifecycle of a Hedera Consensus Node",
		Long:  "Manage lifecycle of a Hedera Consensus Node",
	}
)

func init() {
	// add profile flag to all sub commands
	for _, cmd := range subCommands {
		cmd.Flags().StringVarP(&flagProfile, "profile", "p", "",
			fmt.Sprintf("Deployment profiles %s", core.AllProfiles()))
		_ = cmd.MarkFlagRequired("profile")

	}

	consensusCmd.AddCommand(subCommands...)
}

func Get() *cobra.Command {
	return consensusCmd
}
