// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"fmt"

	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show shape configuration (device or class)",
	Long: "Show the stored shape configuration. Without flags, shows all configured devices and " +
		"classes. With --device or --class, shows only the named entry.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if flagClass != "" && flagDevice != "" {
			return errorx.IllegalArgument.New("--class and --device are mutually exclusive")
		}

		m := newManager()
		var out string
		var err error

		switch {
		case flagClass != "":
			out, err = m.ShowClass(flagClass)
		case flagDevice != "":
			out, err = m.ShowDevice(flagDevice)
		default:
			out, err = m.ShowAll()
		}
		if err != nil {
			return err
		}
		fmt.Fprint(cmd.OutOrStdout(), out)
		return nil
	},
}

func init() {
	showCmd.Flags().StringVar(&flagClass, "class", "", "Show configuration for the named class")
	showCmd.Flags().StringVar(&flagDevice, "device", "", "Show configuration for the named device (ingress or egress)")
}
