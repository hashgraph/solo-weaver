package mirrorcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/internal/core"
)

var (
	nodeType = core.NodeTypeMirror

	flagProfile string

	subCommands = []*cobra.Command{
		mirrorNodeCheckCmd,
		mirrorNodeInstallCmd,
	}

	mirrorCmd = &cobra.Command{
		Use:   "mirror-node",
		Short: "Manage lifecycle of a Hedera mirror Node",
		Long:  "Manage lifecycle of a Hedera mirror Node",
	}
)

func init() {
	// add profile flag to all sub commands
	for _, cmd := range subCommands {
		cmd.Flags().StringVarP(&flagProfile, "profile", "p", "",
			fmt.Sprintf("Deployment profiles %s", core.AllProfiles()))
		_ = cmd.MarkFlagRequired("profile")

	}

	mirrorCmd.AddCommand(subCommands...)
}

func Get() *cobra.Command {
	return mirrorCmd
}
