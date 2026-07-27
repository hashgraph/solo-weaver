// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/state"
	workflowsteps "github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var (
	flagNoReuseValues bool
	flagWithReset     bool
	flagPurgeStorage  bool

	upgradeCmd = &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade a Hedera Block Node",
		Long:  "Upgrade an existing Hedera Block Node deployment with new configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateBlockNodeFlags(cmd); err != nil {
				return err
			}

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
				Action: models.ActionUpgrade,
				Target: models.TargetBlockNode,
			}

			logx.As().Debug().
				Any("intent", intent).
				Any("inputs", inputs).
				Msg("Uninstalling Hedera Block Node")

			handler, err := blockNodeHandler.ForAction(intent.Action)
			if err != nil {
				return err
			}

			if err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
				return handler.HandleIntent(cmd.Context(), intent, *inputs)
			}); err != nil {
				return err
			}

			logx.As().Info().Msg("Successfully upgraded Hedera Block Node")

			// Silent convergence: upgrade does not prompt or expose a gate — it reads
			// the persisted install-time traffic-shaping decision. When that decision
			// was "enabled", ensure the daemon service is running so the block-node
			// traffic-shaper monitor (re-asserted in daemon.yaml by the workflow)
			// actually runs. It is a no-op when the daemon is already up, which is the
			// normal case for an already-shaped block node.
			//
			// If the persisted decision can't be read, skip activation rather than
			// guessing: TrafficShapingDisabled's zero value reads as "enabled" (negative
			// polarity), so proceeding on a read error would wrongly activate the daemon
			// (with an empty namespace) for a node that may have traffic shaping disabled.
			// The workflow already re-asserted daemon.yaml from the authoritative
			// state-manager view, and a running daemon is unaffected.
			stateDefaults, sErr := state.ReadPromptDefaultsFromDisk()
			switch {
			case sErr != nil:
				logx.As().Warn().Err(sErr).Msg(
					"skipping traffic-shaper daemon activation on upgrade: could not read the persisted traffic-shaping decision")
			case !stateDefaults.BlockNode.TrafficShapingDisabled:
				if err := ensureBlockNodeDaemon(cmd, stateDefaults.BlockNode.Namespace, workflowsteps.DaemonBinarySource{}); err != nil {
					return err
				}
			}
			return nil
		},
	}
)

func init() {
	upgradeCmd.Flags().StringVar(&flagChartVersion, "chart-version", "", "Helm chart version to use")
	common.FlagWithStorageReset().SetVarP(upgradeCmd, &flagWithReset, false)
	common.FlagValuesFile().SetVarP(upgradeCmd, &flagValuesFile, false)
	common.FlagNoReuseValues().SetVarP(upgradeCmd, &flagNoReuseValues, false)
	common.FlagHelmTimeout().SetVarP(upgradeCmd, &flagHelmTimeout, false)
}
