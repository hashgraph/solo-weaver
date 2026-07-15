// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var setCmd = &cobra.Command{
	Use:   "set",
	Short: "Update bandwidth parameters of an existing class (tc class change, no qdisc churn)",
	Long: "Atomically update one or more bandwidth parameters of an existing class using " +
		"`tc class change` on the live kernel (no qdisc teardown). The boot script is " +
		"re-rendered for reboot persistence. Only the explicitly supplied flags are changed; " +
		"omitted flags keep their current value. --class is required.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagClass == "" {
			return errorx.IllegalArgument.New("--class is required")
		}
		if !cmd.Flags().Changed("rate") && !cmd.Flags().Changed("ceil") && !cmd.Flags().Changed("prio") {
			return errorx.IllegalArgument.New("at least one of --rate, --ceil, or --prio must be supplied")
		}

		var rate, ceil *string
		var prio *int
		if cmd.Flags().Changed("rate") {
			v := flagRate
			rate = &v
		}
		if cmd.Flags().Changed("ceil") {
			v := flagCeil
			ceil = &v
		}
		if cmd.Flags().Changed("prio") {
			v := flagPrio
			prio = &v
		}

		if err := newManager().SetClass(cmd.Context(), flagClass, rate, ceil, prio); err != nil {
			return err
		}
		logx.As().Info().Str("class", flagClass).Msg("network shape class updated")
		return nil
	},
}

func init() {
	setCmd.Flags().StringVar(&flagClass, "class", "", "Class name to update (required)")
	setCmd.Flags().StringVar(&flagRate, "rate", "", "New guaranteed bandwidth rate")
	setCmd.Flags().StringVar(&flagCeil, "ceil", "", "New burst ceiling rate (≥ --rate)")
	setCmd.Flags().IntVar(&flagPrio, "prio", 0, "New HTB scheduling priority [0,7]")
}
