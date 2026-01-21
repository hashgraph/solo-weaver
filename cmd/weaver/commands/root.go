// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/block"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/kube"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/version"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// examples:
// ./weaver block node check --profile=local
// ./weaver block node setup --config ./config.yaml --profile=mainnet
// ./weaver consensus node check --profile=testnet
// ./weaver consensus node setup --config ./config.yaml --profile=perfnet

// rootCmd represents the base command when called without any subcommands
var (
	// Used for flags.
	flagConfig       string
	flagVersion      bool
	flagOutputFormat string
	flagForce        bool

	rootCmd = &cobra.Command{
		Use:   "weaver",
		Short: "A user friendly tool to provision Hedera network components",
		Long:  "Solo Weaver - A user friendly tool to provision Hedera network components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return common.RunGlobalChecks(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagVersion {
				version.PrintVersion(cmd, flagOutputFormat)
				return nil
			}

			return cmd.Help()
		},
	}
)

func init() {
	common.FlagForce.SetVarP(rootCmd, &flagForce, false)
	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "", "config file path")

	// support '--version', '-v' to show version information
	rootCmd.PersistentFlags().BoolVarP(&flagVersion, "version", "v", false, "Show version")
	rootCmd.PersistentFlags().StringVarP(&flagOutputFormat, "output", "o", "yaml", "Output format (yaml|json)")

	// disable command sorting to keep the order of commands as added
	cobra.EnableCommandSorting = false

	// disable global checks for install command since we need to install it first without any checks
	common.SkipGlobalChecks(selfInstallCmd)

	// add subcommands
	rootCmd.AddCommand(selfInstallCmd)
	rootCmd.AddCommand(selfUninstallCmd)
	rootCmd.AddCommand(kube.GetCmd())
	rootCmd.AddCommand(block.GetCmd())
	rootCmd.AddCommand(version.GetCmd())
}

// Execute executes the root command.
func Execute(ctx context.Context) error {
	if ctx == nil {
		return errorx.IllegalArgument.New("context is required")
	}

	cobra.OnInitialize(func() {
		initConfig(ctx)
	})

	// execute the root command
	_, err := rootCmd.ExecuteContextC(ctx)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to execute command")
	}

	return nil
}

func initConfig(ctx context.Context) {
	var err error
	err = config.Initialize(flagConfig)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	logConfig := config.Get().Log
	err = logx.Initialize(logConfig)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}
}
