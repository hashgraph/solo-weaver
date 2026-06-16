// SPDX-License-Identifier: Apache-2.0

package soak

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/spf13/cobra"
)

var stopKeepState bool

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running consensus-node migration soak watcher",
	Long: "Send a soak stop request to the running solo-provisioner-daemon. " +
		"By default the persisted soak state file is deleted so the daemon does not " +
		"resume the soak on the next restart. Use --keep-state to preserve the state " +
		"file (the daemon will resume the soak automatically on its next startup).",
	RunE: func(cmd *cobra.Command, args []string) error {
		return common.RunWorkflowBuilder(cmd.Context(), workflows.NewSoakStopWorkflow(stopKeepState))
	},
}

func init() {
	stopCmd.Flags().BoolVar(&stopKeepState, "keep-state", false,
		"Preserve cutover-state.jsonl so the daemon resumes the soak on next restart")
}
