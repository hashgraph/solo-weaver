// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var (
	flagNoRestart bool

	reconfigureCmd = &cobra.Command{
		Use:   "reconfigure",
		Short: "Reconfigure a Hedera Block Node",
		Long:  "Re-apply configuration to an existing Hedera Block Node deployment without changing its chart version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateBlockNodeFlags(cmd); err != nil {
				return err
			}
			if err := common.ValidateEgressFlags(cmd, flagLinkRate); err != nil {
				return err
			}
			if err := common.ValidateHostFirewallFlags(cmd); err != nil {
				return err
			}

			shapeOverrides, err := common.ParseShapeOverrides(flagShape)
			if err != nil {
				return err
			}

			inputs, cv, err := prepareBlocknodeInputs(cmd, args)
			if err != nil {
				return err
			}
			inputs.Custom.ShapeOverrides = shapeOverrides

			// Seed the enable/disable prompts from the block node's CURRENT state so a
			// no-flag / default-accept reconfigure keeps whatever is already deployed
			// and only an explicit toggle enables or tears a feature down. Both features
			// now use the same source of truth — the persisted install/reconfigure
			// decision (#947): traffic shaping from BlockNodeState.TrafficShapingDisabled,
			// the host firewall from MachineState.Firewall. (The live inet-table probe is
			// a reconciliation detail inside NetworkFirewallCreate, not a prompt seed.)
			// On an unreadable state file, bias both to enabled so a default-accept never
			// tears an established plane down.
			stateDefaults, err := state.ReadPromptDefaultsFromDisk()
			if err != nil {
				logx.As().Debug().Err(err).Msg("could not read state file for reconfigure seeds; using conservative defaults")
			}
			firewallSeed := true
			currentTrafficShaping := true
			if err == nil {
				firewallSeed = stateDefaults.Firewall != nil && !stateDefaults.Firewall.Disabled
				currentTrafficShaping = !stateDefaults.BlockNode.TrafficShapingDisabled
			}

			// Host firewall is prompted first (before the traffic-shaper question) so
			// the operator answers the firewall gate and its settings before moving on
			// to traffic shaping.
			if err := common.ResolveHostFirewallConfig(cmd, args, cv, firewallSeed); err != nil {
				return err
			}

			// Traffic shaping is independently gated from the host firewall — it covers
			// the BN workload network-policy plane, tc HTB shaping, and the daemon's
			// traffic-shaper monitor. Enabling on a previously-declined install creates
			// all three; explicitly disabling tears them down (see networkPlaneSteps).
			trafficShapingEnabled, err := common.ResolveTrafficShapingConfig(cmd, args, cv, currentTrafficShaping)
			if err != nil {
				return err
			}
			inputs.Custom.TrafficShapingEnabled = trafficShapingEnabled

			// Egress NIC/bandwidth is needed before the workflow because its tc steps
			// consume EgressInterface/LinkRate — declining or disabling leaves nothing
			// to configure here. The daemon binary source is resolved later, AFTER the
			// workflow (see below): resolving it here would let a missing --daemon-bin
			// on --profile=local preempt the clearer "block node is not installed"
			// precondition error on a fresh host.
			if trafficShapingEnabled {
				// Seed the egress prompts from the persisted last-chosen shaping
				// content so a default-accept keeps the operator's previous NIC/link
				// rate instead of reverting to auto-detection. An explicit CLI flag
				// still wins (ResolveEgressConfig skips a prompt whose flag was set).
				if !cmd.Flags().Changed(common.FlagNameEgressInterface) && flagEgressInterface == "" {
					flagEgressInterface = stateDefaults.BlockNode.EgressInterface
				}
				if !cmd.Flags().Changed(common.FlagNameLinkRate) && flagLinkRate == "" {
					flagLinkRate = stateDefaults.BlockNode.LinkRate
				}
				if err := common.ResolveEgressConfig(cmd, args, cv, &flagEgressInterface, &flagLinkRate); err != nil {
					return err
				}
			}
			// prepareBlocknodeInputs ran before the prompts; patch in the final values.
			inputs.Custom.EgressInterface = flagEgressInterface
			inputs.Custom.LinkRate = flagLinkRate

			if cv != nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr())
				cv.Print("Selected Inputs")
			}

			err = initializeDependencies()
			if err != nil {
				return err
			}

			intent := models.Intent{
				Action: models.ActionReconfigure,
				Target: models.TargetBlockNode,
			}

			logx.As().Debug().
				Any("intent", intent).
				Any("inputs", inputs).
				Msg("Reconfiguring Hedera Block Node")

			handler, err := blockNodeHandler.ForAction(intent.Action)
			if err != nil {
				return err
			}

			if err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
				return handler.HandleIntent(cmd.Context(), intent, *inputs)
			}); err != nil {
				return err
			}

			logx.As().Info().Msg("Successfully reconfigured Hedera Block Node")

			// Daemon activation is part of the traffic-shaping bundle (mirroring
			// install): when traffic shaping is enabled, ensure the daemon service is
			// installed and running so the block-node traffic-shaper monitor written
			// into daemon.yaml by the workflow actually runs. It is a no-op when the
			// daemon is already up. Disabling leaves the daemon alone — the workflow's
			// daemon-config step turns the block-node monitor off without uninstalling
			// the shared service.
			if trafficShapingEnabled {
				// Resolve the daemon binary source now — only after the reconfigure
				// workflow (including its "block node is not installed" precondition) has
				// succeeded — so a missing --daemon-bin on --profile=local cannot mask
				// that clearer precondition error. This mirrors upgrade, which likewise
				// activates the daemon post-workflow.
				daemonSource, err := resolveDaemonBinarySource(cmd)
				if err != nil {
					return err
				}
				// bnNamespace is only a fallback orbit for provisionBlockNodeDaemon: the
				// workflow's daemon-config step already persisted the resolved namespace
				// (never empty — it defaults to the block-node orbit) into daemon.yaml,
				// which provisionBlockNodeDaemon reads as the source of truth. So an empty
				// value here (e.g. a state-read miss) does not produce an empty orbit.
				bnNamespace := inputs.Custom.Namespace
				if bnNamespace == "" {
					bnNamespace = stateDefaults.BlockNode.Namespace
				}
				if err := ensureBlockNodeDaemon(cmd, bnNamespace, daemonSource); err != nil {
					return err
				}
			}

			return nil
		},
	}
)

func init() {
	common.FlagWithStorageReset().SetVarP(reconfigureCmd, &flagWithReset, false)
	common.FlagPurgeStorage().SetVarP(reconfigureCmd, &flagPurgeStorage, false)
	common.FlagValuesFile().SetVarP(reconfigureCmd, &flagValuesFile, false)
	common.FlagNoReuseValues().SetVarP(reconfigureCmd, &flagNoReuseValues, false)
	common.FlagNoRestart().SetVar(reconfigureCmd, &flagNoRestart, false)
	common.RegisterHostFirewallFlags(reconfigureCmd)
	common.RegisterTrafficShapingFlags(reconfigureCmd)
	common.RegisterEgressFlags(reconfigureCmd, &flagEgressInterface, &flagLinkRate)
	reconfigureCmd.Flags().StringArrayVar(&flagShape, common.FlagNameShape, nil,
		"Per-class HTB bandwidth override, repeatable: --shape <class>=rate=<r>,ceil=<c>,prio=<p> "+
			"(e.g. --shape publisher=rate=800mbit,ceil=1gbit,prio=0). Only applied when traffic shaping is enabled.")
	common.FlagDaemonBin().SetVarP(reconfigureCmd, &flagDaemonBin, false)
	common.FlagDaemonVersion().SetVarP(reconfigureCmd, &flagDaemonVersion, false)
	common.FlagStatuszBaseURL().SetVarP(reconfigureCmd, &flagStatuszBaseURL, false)
	common.FlagStatuszPollInterval().SetVarP(reconfigureCmd, &flagStatuszPollInterval, false)
}
