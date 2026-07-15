// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a shape device or class configuration",
	Long: "Delete a shape configuration. Exactly one of --device or --class must be supplied.\n\n" +
		"  --class <name>  Remove the class configuration. Fails if the class is referenced as\n" +
		"                  the device default or by any network policy --stamp/--reply-stamp.\n\n" +
		"  --device <dir>  Remove the device configuration. Fails if any classes are still\n" +
		"                  configured for this device (delete classes first).\n\n" +
		"For egress targets, tc-egress.sh is re-rendered and tc-egress.service is restarted.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagClass != "" && flagDevice != "" {
			return errorx.IllegalArgument.New("--class and --device are mutually exclusive")
		}
		if flagClass == "" && flagDevice == "" {
			return errorx.IllegalArgument.New("exactly one of --class or --device must be supplied")
		}

		m := newManager()

		if flagClass != "" {
			if err := m.DeleteClass(cmd.Context(), flagClass); err != nil {
				return err
			}
			logx.As().Info().Str("class", flagClass).Msg("network shape class deleted")
			return nil
		}

		if err := m.DeleteDevice(cmd.Context(), flagDevice); err != nil {
			return err
		}
		logx.As().Info().Str("device", flagDevice).Msg("network shape device deleted")
		return nil
	},
}

func init() {
	deleteCmd.Flags().StringVar(&flagClass, "class", "", "Class name to delete")
	deleteCmd.Flags().StringVar(&flagDevice, "device", "", "Device direction to delete (ingress or egress)")
}
