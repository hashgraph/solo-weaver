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

		inputs, cv, err := prepareBlocknodeInputs(cmd, args)
		if err != nil {
			return err
		}

		// Prompt for egress NIC and bandwidth independently of the host firewall —
		// tc-egress traffic shaping applies regardless of whether the host
		// firewall is enabled.
		if err := common.ResolveEgressConfig(cmd, args, cv, &flagEgressInterface, &flagLinkRate); err != nil {
			return err
		}
		// prepareBlocknodeInputs ran before the prompts; patch in the final values.
		inputs.Custom.EgressInterface = flagEgressInterface
		inputs.Custom.LinkRate = flagLinkRate

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

		return nil
	},
}

func init() {
	installCmd.Flags().StringVar(&flagChartVersion, "chart-version", "", "Helm chart version to use")
	common.FlagValuesFile().SetVarP(installCmd, &flagValuesFile, false)
	common.RegisterHostFirewallFlags(installCmd)
	common.RegisterEgressFlags(installCmd, &flagEgressInterface, &flagLinkRate)
}
