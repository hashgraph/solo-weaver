package blockcmd

import (
	"github.com/spf13/cobra"
)

func prepareSubCmdForTest(sub *cobra.Command) *cobra.Command {
	// create an explicit root so test-runner args aren't treated as subcommands
	root := &cobra.Command{Use: "block-node"}
	root.AddCommand(sub)
	return root
}
