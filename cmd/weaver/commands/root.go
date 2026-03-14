// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/alloy"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/block"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/kube"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/teleport"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/version"
	"github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/internal/workflows"
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
	flagConfig             string
	flagVersion            bool
	flagOutputFormat       string
	flagSkipHardwareChecks bool

	rootCmd = &cobra.Command{
		Use:   "solo-provisioner",
		Short: "A user friendly tool to provision Hedera network components",
		Long:  "Solo Provisioner - A user friendly tool to provision Hedera network components",
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

// Register state migrations at startup
func init() {
	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "", "config file path")

	// support '--version', '-v' to show version information
	rootCmd.PersistentFlags().BoolVarP(&flagVersion, "version", "v", false, "Show version")
	rootCmd.PersistentFlags().StringVarP(&flagOutputFormat, "output", "o", "yaml", "Output format (yaml|json)")

	// Verbose output flag - shows full error stacktraces and profiling data
	rootCmd.PersistentFlags().BoolVarP(&ui.Verbose, "verbose", "V", false, "Enable verbose output with full error details")

	// TUI override flag - hidden to discourage casual use
	rootCmd.PersistentFlags().BoolVar(&ui.NoTUI, "no-tui", false, "Disable TUI output (use simple line-based output)")
	_ = rootCmd.PersistentFlags().MarkHidden("no-tui")

	// Hardware check override flag - hidden to discourage casual use
	rootCmd.PersistentFlags().BoolVar(&flagSkipHardwareChecks, common.FlagSkipHardwareChecks.Name, false,
		"DANGEROUS: Skip hardware validation checks. May cause node instability or data loss.")
	_ = rootCmd.PersistentFlags().MarkHidden(common.FlagSkipHardwareChecks.Name)

	// disable command sorting to keep the order of commands as added
	cobra.EnableCommandSorting = false

	// disable global checks for install command since we need to install it first without any checks
	common.SkipGlobalChecks(selfInstallCmd)

	// add subcommands
	rootCmd.AddCommand(selfInstallCmd)
	rootCmd.AddCommand(selfUninstallCmd)
	rootCmd.AddCommand(kube.GetCmd())
	rootCmd.AddCommand(block.GetCmd())
	rootCmd.AddCommand(teleport.GetCmd())
	rootCmd.AddCommand(alloy.GetCmd())
	rootCmd.AddCommand(version.GetCmd())

	// Register all migrations at startup
	blocknode.InitMigrations()
	state.InitMigrations()
	workflows.InitMigrations()
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

	// Propagate verbose flag to the doctor package for error diagnostics
	doctor.Verbose = ui.Verbose

	logConfig := config.Get().Log

	// Always enable file logging regardless of config so every run produces a log file
	logConfig.FileLogging = true
	if logConfig.Directory == "" {
		logConfig.Directory = core.Paths().LogsDir
	}
	if logConfig.Filename == "" {
		logConfig.Filename = "solo-provisioner.log"
	}
	if logConfig.MaxSize == 0 {
		logConfig.MaxSize = 50 // 50 MB
	}
	if logConfig.MaxBackups == 0 {
		logConfig.MaxBackups = 3
	}
	if logConfig.MaxAge == 0 {
		logConfig.MaxAge = 30 // 30 days
	}

	// Suppress console logging when the TUI is active to avoid interleaving
	// raw zerolog lines with the Bubble Tea render loop. The TUI owns stdout.
	// NOTE: upstream logx.Initialize() ignores the ConsoleLogging field and
	// always creates a ConsoleWriter, so we must replace the logger afterwards.
	if ui.ShouldUseTUI() {
		logConfig.ConsoleLogging = false
	}

	err = logx.Initialize(logConfig)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	// Replace the logx logger with a file-only writer when TUI is active.
	// This works around upstream logx unconditionally adding a ConsoleWriter.
	if ui.ShouldUseTUI() {
		ui.SuppressConsoleLogging(logConfig)
	}
}
