// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

// flagNoScaleUp is shared by reset, reconfigure and upgrade. prepareBlocknodeInputs
// is also used by install and check, which do not register the flag; the zero value
// is the long-standing scale-back-up behaviour, so they are unaffected.
var flagNoScaleUp bool

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset a Hedera Block Node by clearing its storage",
	Long: `Reset a Hedera Block Node by clearing all data stored on disk (blocks, logs, etc.).

This command will:
1. Scale down the block node StatefulSet to stop the pod
2. Wait for the block node pod to terminate
3. Clear all files from the storage directories
4. Scale the StatefulSet back up to restart the pod
5. Wait for the block node to become ready

Pass --no-scale-up to stop after step 3, leaving the StatefulSet at 0 replicas so
the storage tree can be inspected or seeded before the node starts again. Bring the
node back up with 'kubectl scale statefulset <name> -n <namespace> --replicas=1', or
with a plain reconfigure or upgrade. Re-running reset also starts the node, but it
clears the storage directories first and so discards anything staged there.

WARNING: This operation is destructive and cannot be undone. All block data will be lost.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		inputs, cv, err := prepareBlocknodeInputs(cmd, args)
		if err != nil {
			return err
		}
		if cv != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr())
			cv.Print("Selected Inputs")
		}

		err = initializeDependencies()
		if err != nil {
			return err
		}

		intent := models.Intent{
			Action: models.ActionReset,
			Target: models.TargetBlockNode,
		}

		logx.As().Debug().
			Any("intent", intent).
			Any("inputs", inputs).
			Msg("Resetting Hedera Block Node")

		handler, err := blockNodeHandler.ForAction(intent.Action)
		if err != nil {
			return err
		}

		if err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			return handler.HandleIntent(cmd.Context(), intent, *inputs)
		}); err != nil {
			return err
		}

		logx.As().Info().Msg("Successfully reset Hedera Block Node")
		return nil
	},
}

func init() {
	common.FlagNoScaleUp().SetVar(resetCmd, &flagNoScaleUp, false)
}
