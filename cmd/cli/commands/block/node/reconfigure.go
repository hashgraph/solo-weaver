// SPDX-License-Identifier: Apache-2.0

package node

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/state"
	workflowsteps "github.com/hashgraph/solo-weaver/internal/workflows/steps"
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
			// and only an explicit toggle enables or tears a feature down. Traffic
			// shaping's current state comes from the persisted install decision; the
			// host firewall's from whether the inet host table currently exists.
			stateDefaults, err := state.ReadPromptDefaultsFromDisk()
			if err != nil {
				logx.As().Debug().Err(err).Msg("could not read state file for reconfigure seeds; using conservative defaults")
			}
			currentTrafficShaping := !stateDefaults.BlockNode.TrafficShapingDisabled
			firewallSeed := currentFirewallEnabledSeed(cmd, cmd.Context())

			// Traffic shaping is independently gated from the host firewall — it covers
			// the BN workload network-policy plane, tc HTB shaping, and the daemon's
			// traffic-shaper monitor. Enabling on a previously-declined install creates
			// all three; explicitly disabling tears them down (see networkPlaneSteps).
			trafficShapingEnabled, err := common.ResolveTrafficShapingConfig(cmd, args, cv, currentTrafficShaping)
			if err != nil {
				return err
			}
			inputs.Custom.TrafficShapingEnabled = trafficShapingEnabled

			// Egress NIC/bandwidth and the daemon binary source are only needed when
			// enabling traffic shaping — declining or disabling leaves nothing to
			// configure or activate here.
			var daemonSource workflowsteps.DaemonBinarySource
			if trafficShapingEnabled {
				if err := common.ResolveEgressConfig(cmd, args, cv, &flagEgressInterface, &flagLinkRate); err != nil {
					return err
				}
				daemonSource, err = resolveDaemonBinarySource(cmd, args, inputs.Custom.Profile, cv)
				if err != nil {
					return err
				}
			}
			// prepareBlocknodeInputs ran before the prompts; patch in the final values.
			inputs.Custom.EgressInterface = flagEgressInterface
			inputs.Custom.LinkRate = flagLinkRate

			if err := common.ResolveHostFirewallConfig(cmd, args, cv, firewallSeed); err != nil {
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

// currentFirewallEnabledSeed returns the default the reconfigure host-firewall
// prompt should fall back to: whether the inet host table is currently present on
// the host. When the flag was set on the CLI the seed is unused (the flag wins),
// so it returns false without probing. If the probe fails it biases to enabled so
// an unreadable state never leads to an accidental teardown on default-accept.
func currentFirewallEnabledSeed(cmd *cobra.Command, ctx context.Context) bool {
	if cmd.Flags().Changed(common.FlagNameFirewallEnabled) {
		return false
	}
	active, err := firewall.NewManager().IsActive(ctx)
	if err != nil {
		logx.As().Debug().Err(err).Msg(
			"could not probe the inet host table; seeding the firewall prompt as enabled to avoid accidental teardown")
		return true
	}
	return active
}

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
}
