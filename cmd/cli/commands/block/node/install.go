// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	workflowsteps "github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"setup"}, // deprecated, will be removed soon
	Short:   "Install a Hedera Block Node",
	Long:    "Run safety checks, setup a K8s cluster and install a Hedera Block Node",
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
		// prepareBlocknodeInputs is shared with other block-node commands; patch
		// in the install-only network field here.
		inputs.Custom.ShapeOverrides = shapeOverrides

		// The traffic-shaping gate is independent of the host firewall — it
		// covers the BN workload network-policy plane and tc HTB shaping, not
		// the host's own SSH/mgmt firewall.
		trafficShapingEnabled, err := common.ResolveTrafficShapingConfig(cmd, args, cv)
		if err != nil {
			return err
		}
		inputs.Custom.TrafficShapingEnabled = trafficShapingEnabled

		// Prompt for egress NIC and bandwidth, and resolve where the daemon
		// binary comes from, only when traffic shaping is enabled — declining
		// the gate above means there is nothing left to configure here.
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

		// Host firewall is independently gated from traffic shaping above — it
		// applies regardless of whether the BN policy plane/tc shaping is enabled.
		if err := common.ResolveHostFirewallConfig(cmd, args, cv); err != nil {
			return err
		}

		// Print the unified summary of all prompted values (block node inputs +
		// firewall) now that both prompt sections have completed.
		if cv != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr())
			cv.Print("Selected Inputs")
		}

		err = initializeDependencies()
		if err != nil {
			return err
		}

		intent := models.Intent{
			Action: models.ActionInstall,
			Target: models.TargetBlockNode,
		}

		logx.As().Info().
			Any("intent", intent).
			Any("inputs", inputs).
			Msg("Installing Hedera Block Node")

		handler, err := blockNodeHandler.ForAction(intent.Action)
		if err != nil {
			return err
		}

		if err := common.RunWorkflow(cmd.Context(), func() (*automa.Report, error) {
			return handler.HandleIntent(cmd.Context(), intent, *inputs)
		}); err != nil {
			return err
		}

		logx.As().Info().Msg("Successfully installed Hedera Block Node")

		// Daemon activation is part of the traffic-shaping bundle, not a separate
		// decision: a block node without its daemon has no ingress prioritization
		// and nothing reconciling the daemon-owned nft sets from statusz, so
		// enabling traffic shaping installs and provisions the daemon
		// automatically (no separate prompt/flag). Skipped entirely when traffic
		// shaping is disabled — there would be nothing for the daemon to do.
		if trafficShapingEnabled {
			if err := ensureBlockNodeDaemon(cmd, inputs.Custom.Namespace, daemonSource); err != nil {
				return err
			}
		} else {
			logx.As().Info().Msg("traffic shaping disabled (--traffic-shaping-enabled=false); skipping traffic-shaper daemon activation")
		}

		return nil
	},
}

func init() {
	installCmd.Flags().StringVar(&flagChartVersion, "chart-version", "", "Helm chart version to use")
	common.FlagValuesFile().SetVarP(installCmd, &flagValuesFile, false)
	common.FlagHelmTimeout().SetVarP(installCmd, &flagHelmTimeout, false)
	common.RegisterHostFirewallFlags(installCmd)
	common.RegisterTrafficShapingFlags(installCmd)
	common.RegisterEgressFlags(installCmd, &flagEgressInterface, &flagLinkRate)
	installCmd.Flags().StringArrayVar(&flagShape, common.FlagNameShape, nil,
		"Per-class HTB bandwidth override, repeatable: --shape <class>=rate=<r>,ceil=<c>,prio=<p> "+
			"(e.g. --shape publisher=rate=800mbit,ceil=1gbit,prio=0). Classes not overridden use the profile defaults.")
	common.FlagDaemonBin().SetVarP(installCmd, &flagDaemonBin, false)
	common.FlagDaemonVersion().SetVarP(installCmd, &flagDaemonVersion, false)
}
