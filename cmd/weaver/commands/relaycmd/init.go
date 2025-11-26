package relaycmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/internal/core"
)

var (
	nodeType = core.NodeTypeMirror

	flagProfile string

	subCommands = []*cobra.Command{
		relayNodeCheckCmd,
		relayNodeInstallCmd,
	}

	relayCmd = &cobra.Command{
		Use:   "relay-node",
		Short: "Manage lifecycle of a Hedera relay Node",
		Long:  "Manage lifecycle of a Hedera relay Node",
	}
)

func init() {
	// add profile flag to all sub commands
	for _, cmd := range subCommands {
		cmd.Flags().StringVarP(&flagProfile, "profile", "p", "",
			fmt.Sprintf("Deployment profiles %s", core.AllProfiles()))
		_ = cmd.MarkFlagRequired("profile")

	}

	relayCmd.AddCommand(subCommands...)
}

func Get() *cobra.Command {
	return relayCmd
}
