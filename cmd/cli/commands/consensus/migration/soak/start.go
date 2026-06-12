// SPDX-License-Identifier: Apache-2.0

package soak

import (
	"time"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var (
	startNodeID        string
	startCutoverTS     string
	startMigrationPlan string
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the consensus-node migration soak watcher",
	Long: "Send a soak start request to the running solo-provisioner-daemon. " +
		"The daemon enqueues the request and begins monitoring soak criteria on each poll interval.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ts, err := time.Parse(time.RFC3339, startCutoverTS)
		if err != nil {
			return errorx.IllegalArgument.New(
				"--cutover-ts must be an RFC-3339 timestamp (e.g. 2025-09-01T00:00:00Z): %v", err)
		}

		req := consensus.SoakStartRequest{
			NodeID:            startNodeID,
			CutoverTimestamp:  ts,
			MigrationPlanPath: startMigrationPlan,
		}
		if err := req.Validate(); err != nil {
			return errorx.IllegalArgument.New("%v", err)
		}

		return common.RunWorkflowBuilder(cmd.Context(), workflows.NewSoakStartWorkflow(req))
	},
}

func init() {
	startCmd.Flags().StringVar(&startNodeID, "node-id", "", "Consensus node ID (required)")
	startCmd.Flags().StringVar(&startCutoverTS, "cutover-ts", "", "Cutover timestamp in RFC-3339 format, e.g. 2025-09-01T00:00:00Z (required)")
	startCmd.Flags().StringVar(&startMigrationPlan, "migration-plan", "", "Path to the migration plan file on the host (required)")
	_ = startCmd.MarkFlagRequired("node-id")
	_ = startCmd.MarkFlagRequired("cutover-ts")
	_ = startCmd.MarkFlagRequired("migration-plan")
}
