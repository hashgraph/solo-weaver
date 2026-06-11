// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/alloy"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/block"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/daemon"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/kube"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/teleport"
	"github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/proxy"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	version "github.com/hashgraph/solo-weaver/pkg/version/cli"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// examples:
// ./solo-provisioner block node check --profile=local
// ./solo-provisioner block node install --config ./config.yaml --profile=mainnet
// ./solo-provisioner consensus node check --profile=testnet
// ./solo-provisioner consensus node setup --config ./config.yaml --profile=perfnet

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
			return common.RunPersistentPreRun(cmd, args)
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
	common.FlagVerbose().SetVarP(rootCmd, &ui.VerboseLevel)

	common.FlagNonInteractive().SetVarP(rootCmd, &ui.NonInteractive, false)

	// Hardware checks override flag - hidden to discourage casual use
	common.FlagSkipHardwareChecks().SetVarP(rootCmd, &flagSkipHardwareChecks, false)
	_ = rootCmd.PersistentFlags().MarkHidden(common.FlagSkipHardwareChecks().Name)

	// disable command sorting to keep the order of commands as added
	cobra.EnableCommandSorting = false

	// disable global checks for install command since we need to install it first without any checks
	common.SkipGlobalChecks(selfInstallCmd)

	// `version` must be invokable on an uninstalled host (it's the no-install
	// sanity check reviewers run), so it opts out of the global checks too.
	versionCmd := version.Cmd()
	common.SkipGlobalChecks(versionCmd)

	// add subcommands
	rootCmd.AddCommand(selfInstallCmd)
	rootCmd.AddCommand(selfUninstallCmd)
	rootCmd.AddCommand(kube.GetCmd())
	rootCmd.AddCommand(block.GetCmd())
	rootCmd.AddCommand(teleport.GetCmd())
	rootCmd.AddCommand(alloy.GetCmd())
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(tuiDemoCmd)
	rootCmd.AddCommand(daemon.GetCmd())

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
//   - migration.ScopeStartup ("startup"):    runs before every command in RunPersistentPreRun.
//   - migration.ScopeBlockNode ("block-node"): runs explicitly during block node upgrades.
func RegisterMigrations() {
	// ── Startup migrations (run before every CLI invocation) ─────────────────
	migration.Register(migration.ScopeStartup, state.NewUnifiedStateMigration())
	migration.Register(migration.ScopeStartup, state.NewHelmReleaseSchemaV2Migration())
	migration.Register(migration.ScopeStartup, workflows.NewLegacyBinaryMigration())
	migration.Register(migration.ScopeStartup, workflows.NewCiliumAccelerationMigration())

	// ── Block-node upgrade migrations (run during block node upgrade workflow) ─
	migration.Register(migration.ScopeBlockNode, blocknode.NewVerificationStorageMigration())
	migration.Register(migration.ScopeBlockNode, blocknode.NewPluginsStorageMigration())
}

// Execute executes the root command.
func Execute(ctx context.Context) error {
	if ctx == nil {
		return errorx.IllegalArgument.New("context is required")
	}

	// Guard before any file I/O: lumberjack and the state-file reader both
	// require root. Exit early with a clear message so no log-rotation noise
	// appears on screen for non-exempt invocations.
	if os.Getuid() != 0 && !isPrivilegeExemptInvocation(os.Args[1:]) {
		return errorx.RejectedOperation.New("solo-provisioner must be run with superuser privileges").
			WithProperty(doctor.ErrPropertyResolution,
				fmt.Sprintf("Run: sudo %s", strings.Join(os.Args, " ")))
	}

	cobra.OnInitialize(func() {
		initConfig(ctx)
		fmt.Print(ui.RenderVersionHeader())
	})

	// execute the root command
	_, err := rootCmd.ExecuteContextC(ctx)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to execute command")
	}

	return nil
}

func initConfig(ctx context.Context) {
	// Cap verbose level at 1 (extra -V's are harmless) and propagate to the
	// doctor package up front so any error raised during this init function —
	// including config.Initialize below — honors -V.
	if ui.VerboseLevel > 1 {
		ui.VerboseLevel = 1
	}
	doctor.VerboseLevel = ui.VerboseLevel

	var err error
	err = config.Initialize(flagConfig)
	if err != nil {
		doctor.CheckErr(ctx, err)
	}

	logConfig := config.Get().Log
	if flagLogLevel != "" {
		logConfig.Level = flagLogLevel
	}

	// Always enable file logging regardless of config so every run produces a log file
	logConfig.FileLogging = true
	if logConfig.Directory == "" {
		logConfig.Directory = models.Paths().LogsDir
	}
	// Hardcoded, not config-overridable: keeps the CLI's log file distinct from
	// the daemon's (cmd/daemon writes to "solo-provisioner-daemon.log"), avoids
	// any chance of CLI + daemon interleaving into a shared file when both run on
	// the same host, and keeps the summary table's logPath in sync with where
	// the CLI actually writes (see ensureLogConfig in cmd/cli/commands/common/run.go).
	logConfig.Filename = "solo-provisioner.log"
	// Ensure the log directory exists with setgid + weaver group before lumberjack touches
	// it. On first run the directory does not yet exist; MkdirAll creates it, then chown+chmod
	// give it the right ownership so that files created inside (including the log file below
	// and any future lumberjack rotations) inherit the weaver group via setgid.
	svcGID, gidErr := strconv.Atoi(config.WeaverGroupId())
	if mkdirErr := os.MkdirAll(logConfig.Directory, models.DefaultDirOrExecPerm); mkdirErr == nil && gidErr == nil {
		_ = os.Chown(logConfig.Directory, 0, svcGID)
		_ = os.Chmod(logConfig.Directory, models.DefaultStorageDirPerm)
	}

	// Pre-create the log file with world-readable mode (0644) before lumberjack claims it.
	// Lumberjack hardcodes 0600 on first creation; opening an existing file uses O_APPEND
	// which preserves the existing mode. Because the directory now has setgid, the newly
	// created file inherits the weaver group automatically.
	// For files that already exist with wrong ownership (created before the setgid fix),
	// fix their group and mode explicitly.
	logFilePath := path.Join(logConfig.Directory, logConfig.Filename)
	if _, statErr := os.Stat(logFilePath); os.IsNotExist(statErr) {
		if f, createErr := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY, models.DefaultFilePerm); createErr == nil {
			_ = f.Close()
			_ = os.Chmod(logFilePath, models.DefaultFilePerm)
		}
	} else if statErr == nil && gidErr == nil {
		_ = os.Chown(logFilePath, 0, svcGID)
		_ = os.Chmod(logFilePath, models.DefaultFilePerm)
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

// isPrivilegeExemptInvocation reports whether args represents an invocation
// that does not require superuser privileges (version output, help text).
func isPrivilegeExemptInvocation(args []string) bool {
	if len(args) == 0 {
		return true
	}
	for _, arg := range args {
		switch arg {
		case "--version", "-v", "--help", "-h":
			return true
		}
	}
	// Only the leading subcommand word can be exempt; flag values and nested
	// subcommands after it are not checked because they may follow a
	// flag=value pair whose value looks like a word (e.g. --log-level debug).
	if args[0] == "version" || args[0] == "help" {
		return true
	}

	return false
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
