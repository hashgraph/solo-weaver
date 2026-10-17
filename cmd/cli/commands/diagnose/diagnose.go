// SPDX-License-Identifier: Apache-2.0

package diagnose

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/spf13/cobra"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose",
	Short: "Inspect logs and other diagnostics",
	Long:  "Read-only tools for finding out what solo-provisioner and its daemon have been doing.",
	RunE:  common.DefaultRunE,
}

func init() {
	// Diagnostics must work on a host that is broken or only partly installed.
	common.SkipGlobalChecks(diagnoseCmd)
	diagnoseCmd.AddCommand(newLogsCmd())
}

func GetCmd() *cobra.Command {
	return diagnoseCmd
}
