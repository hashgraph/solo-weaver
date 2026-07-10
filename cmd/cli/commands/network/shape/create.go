// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	shp "github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a shape device root or bandwidth class",
	Long: "Create a tc HTB shape configuration. Exactly one of --device or --class must be supplied:\n\n" +
		"  --device <dir>  Configure the root qdisc and trunk class for \"ingress\" ($VETH) or\n" +
		"                  \"egress\" ($NIC). Required: --rate, --default. Must run before any\n" +
		"                  --class form for the same device (tc parent-before-child requirement).\n\n" +
		"  --class <name>  Add an HTB leaf class + fq_codel qdisc. The device is implied by the\n" +
		"                  class name. Required: --rate. Optional: --ceil, --prio.\n\n" +
		"create-if-missing: an existing entry is left untouched unless --force is passed.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagClass != "" && flagDevice != "" {
			return errorx.IllegalArgument.New("--class and --device are mutually exclusive")
		}
		if flagClass == "" && flagDevice == "" {
			return errorx.IllegalArgument.New("exactly one of --class or --device must be supplied")
		}

		force, err := common.FlagForce().Value(cmd, args)
		if err != nil {
			return err
		}

		m := newManager()

		if flagDevice != "" {
			return runCreateDevice(cmd, m, force)
		}
		return runCreateClass(cmd, m, force)
	},
}

func runCreateDevice(cmd *cobra.Command, m *shp.Manager, force bool) error {
	if !cmd.Flags().Changed("rate") {
		return errorx.IllegalArgument.New("--rate is required for --device")
	}
	if !cmd.Flags().Changed("default") {
		return errorx.IllegalArgument.New("--default is required for --device")
	}

	dev := &shp.DeviceConfig{
		Dir:          flagDevice,
		Rate:         flagRate,
		DefaultClass: flagDefault,
	}
	changed, err := m.CreateDevice(cmd.Context(), dev, force)
	if err != nil {
		return err
	}
	if changed {
		logx.As().Info().Str("device", dev.Dir).Str("rate", dev.Rate).
			Str("default", dev.DefaultClass).Msg("network shape device configured")
	}
	return nil
}

func runCreateClass(cmd *cobra.Command, m *shp.Manager, force bool) error {
	if !cmd.Flags().Changed("rate") {
		return errorx.IllegalArgument.New("--rate is required for --class")
	}

	cls := &shp.ClassConfig{
		Name: flagClass,
		Rate: flagRate,
		Prio: flagPrio,
	}
	if cmd.Flags().Changed("ceil") {
		cls.Ceil = flagCeil
	}

	changed, err := m.CreateClass(cmd.Context(), cls, force)
	if err != nil {
		return err
	}
	if changed {
		ceil := cls.Ceil
		if ceil == "" {
			ceil = cls.Rate
		}
		logx.As().Info().Str("class", cls.Name).Str("rate", cls.Rate).
			Str("ceil", ceil).Int("prio", cls.Prio).
			Msg("network shape class configured")
	}
	return nil
}

func init() {
	createCmd.Flags().StringVar(&flagClass, "class", "", "HTB class name to configure (publisher, backfill-response, reserve-ingress, partner, public, reserve-egress)")
	createCmd.Flags().StringVar(&flagDevice, "device", "", "Traffic direction to configure root for: ingress ($VETH) or egress ($NIC)")
	createCmd.Flags().StringVar(&flagRate, "rate", "", "Bandwidth rate (e.g. 100mbit, 1gbit)")
	createCmd.Flags().StringVar(&flagCeil, "ceil", "", "Burst ceiling rate (≥ --rate; defaults to --rate if omitted)")
	createCmd.Flags().IntVar(&flagPrio, "prio", 0, "HTB scheduling priority [0,7] (0 = highest; default 0)")
	createCmd.Flags().StringVar(&flagDefault, "default", "", "Default class name for unmatched traffic (--device form only)")
}
