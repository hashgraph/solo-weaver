package commands

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
	"golang.hedera.com/solo-weaver/cmd/weaver/commands/block"
	"golang.hedera.com/solo-weaver/cmd/weaver/commands/version"
	"golang.hedera.com/solo-weaver/internal/config"
	"golang.hedera.com/solo-weaver/internal/doctor"
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

	rootCmd = &cobra.Command{
		Use:   "weaver",
		Short: "A user friendly tool to provision Hedera network components",
		Long:  "Solo Weaver - A user friendly tool to provision Hedera network components",
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
	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "", "config file path")

	// support '--version', '-v' to show version information
	rootCmd.PersistentFlags().BoolVarP(&flagVersion, "version", "v", false, "Show version")
	rootCmd.PersistentFlags().StringVarP(&flagOutputFormat, "output", "o", "yaml", "Output format (yaml|json)")

	// disable command sorting to keep the order of commands as added
	cobra.EnableCommandSorting = false

	// add subcommands
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
