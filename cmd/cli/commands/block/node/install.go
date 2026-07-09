// SPDX-License-Identifier: Apache-2.0

package node

import (
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/hashgraph/solo-weaver/internal/ui/prompt"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"setup"}, // deprecated, will be removed soon
	Short:   "Install a Hedera Block Node",
	Long:    "Run safety checks, setup a K8s cluster and install a Hedera Block Node",
	RunE: func(cmd *cobra.Command, args []string) error {
		inputs, cv, err := prepareBlocknodeInputs(cmd, args)
		if err != nil {
			return err
		}

		// Resolve the host firewall config. The egress-interface prompt is
		// prepended so it appears in the same TUI panel as the firewall fields.
		// cv is passed so all values fold into the unified "Selected Inputs"
		// summary rather than a separate section.
		detectedNIC, _ := shape.DetectEgressInterface()
		if err := common.ResolveHostFirewallConfig(cmd, args, cv,
			prompt.EgressInterfaceInputPrompt(detectedNIC, &flagEgressInterface),
		); err != nil {
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
	installCmd.Flags().StringVar(&flagEgressInterface, "egress-interface", "",
		"Physical NIC for the $EGRESS HTB traffic-shaper hierarchy (e.g. eth0). "+
			"Auto-detected from the default route when omitted; use this flag to override on multi-NIC hosts.")
}
