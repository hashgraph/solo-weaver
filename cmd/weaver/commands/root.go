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
	"github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/proxy"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/version"
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
	flagForce              bool
	flagLogLevel           string

	rootCmd = &cobra.Command{
		Use:   "solo-provisioner",
		Short: "A user friendly tool to provision Hedera network components",
		Long:  "Solo Provisioner - A user friendly tool to provision Hedera network components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return common.RunGlobalChecks(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagVersion {
				return version.Print(cmd, flagOutputFormat)
			}

			return cmd.Help()
		},
	}
)

// Register state migrations at startup
func init() {
	common.FlagLogLevel().SetVarP(rootCmd, &flagLogLevel, false)
	common.FlagForce().SetVarP(rootCmd, &flagForce, false)
	common.FlagConfig().SetVarP(rootCmd, &flagConfig, false)
	// support '--version', '-v' to show version information
	common.FlagVersion().SetVarP(rootCmd, &flagVersion, false)
	common.FlagOutputFormat().SetVarP(rootCmd, &flagOutputFormat, false)

	// Verbose output flag — -V enables expanded step-by-step output
	rootCmd.PersistentFlags().CountVarP(&ui.VerboseLevel, "verbose", "V", "Show expanded step-by-step output")

	// --non-interactive disables the TUI and outputs raw zerolog (for CI/pipelines)
	rootCmd.PersistentFlags().BoolVar(&ui.NonInteractive, "non-interactive", false, "Disable TUI and output raw logs (for CI/pipelines)")

	// Hardware checks override flag - hidden to discourage casual use
	common.FlagSkipHardwareChecks().SetVarP(rootCmd, &flagSkipHardwareChecks, false)
	_ = rootCmd.PersistentFlags().MarkHidden(common.FlagSkipHardwareChecks().Name)

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
	rootCmd.AddCommand(version.Cmd())
	rootCmd.AddCommand(tuiDemoCmd)

	if common.DetectShortNameCollisions(rootCmd) {
		logx.As().Warn().Msg("flag short name collisions detected among commands; consider using unique short names " +
			"to avoid confusion when using flags with multiple commands")
	}

	RegisterMigrations()

}

// RegisterMigrations is the single authoritative place where every migration is registered.
// Migrations are listed in chronological order of introduction so that when a user upgrades
// from an older version, all applicable migrations run in the correct sequence.
//
// Scopes:
//   - migration.ScopeStartup ("startup"):    runs before every command in RunGlobalChecks.
//   - migration.ScopeBlockNode ("block-node"): runs explicitly during block node upgrades.
func RegisterMigrations() {
	// ── Startup migrations (run before every CLI invocation) ─────────────────
	migration.Register(migration.ScopeStartup, state.NewUnifiedStateMigration())
	migration.Register(migration.ScopeStartup, state.NewHelmReleaseSchemaV2Migration())
	migration.Register(migration.ScopeStartup, workflows.NewLegacyBinaryMigration())

	// ── Block-node upgrade migrations (run during block node upgrade workflow) ─
	migration.Register(migration.ScopeBlockNode, blocknode.NewVerificationStorageMigration())
	migration.Register(migration.ScopeBlockNode, blocknode.NewPluginsStorageMigration())
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

	// Cap verbose level at 1 (extra -V's are harmless)
	if ui.VerboseLevel > 1 {
		ui.VerboseLevel = 1
	}

	// Propagate verbose flag to the doctor package for error diagnostics
	doctor.VerboseLevel = ui.VerboseLevel

	logConfig := config.Get().Log
	if flagLogLevel != "" {
		logConfig.Level = flagLogLevel
	}

	// Always enable file logging regardless of config so every run produces a log file
	logConfig.FileLogging = true
	if logConfig.Directory == "" {
		logConfig.Directory = models.Paths().LogsDir
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

	if ui.IsUnformatted() {
		// Raw mode: let zerolog write directly to the console (no suppression).
		err = logx.Initialize(logConfig)
		if err != nil {
			doctor.CheckErr(ctx, err)
		}
	} else {
		// Suppress console logging — the TUI owns stdout, not zerolog.
		// Raw log lines go only to the log file.
		logConfig.ConsoleLogging = false
		err = logx.Initialize(logConfig)
		if err != nil {
			doctor.CheckErr(ctx, err)
		}
		ui.SuppressConsoleLogging(logConfig)
	}

	// Activate proxy after logging is initialized so the activation log
	// respects TUI suppression and goes to the log file instead of stdout.
	activateProxy(ctx)
}

func activateProxy(ctx context.Context) {
	proxyCfg := config.Get().Proxy
	if !proxyCfg.Enabled {
		return
	}

	if err := proxy.Activate(proxyCfg); err != nil {
		doctor.CheckErr(ctx, err)
	}
}
