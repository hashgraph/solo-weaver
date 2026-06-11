// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"strconv"
	"syscall"

	"github.com/automa-saga/logx"
	"github.com/google/uuid"
	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/proxy"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	version "github.com/hashgraph/solo-weaver/pkg/version/daemon"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

var (
	flagConfig       string
	flagLogLevel     string
	flagVersion      bool
	flagOutputFormat string

	// Optional overrides — each takes precedence over the corresponding daemon.yaml
	// field when set. The service file stays flag-free; these flags are for
	// operator debugging and integration testing without editing daemon.yaml.
	flagNodeID     string
	flagKubeconfig string
	flagOrbit      string
	flagUpgradeDir string

	rootCmd = &cobra.Command{
		Use:   "solo-provisioner-daemon",
		Short: "Long-running daemon for Solo Provisioner host-level work",
		Long:  "Long-lived foreground process started by the solo-provisioner-daemon.service systemd unit.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagVersion {
				return version.Print(cmd, flagOutputFormat)
			}

			logx.As().Info().Msg("Solo Provisioner daemon started")

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()

			paths := models.Paths()

			// Load daemon.yaml; apply any CLI flag overrides; re-validate.
			cfg, err := daemon.LoadDaemonConfig(paths.DaemonConfigPath)
			if err != nil {
				if ex := errorx.Cast(err); ex != nil {
					return ex.WithProperty(models.ErrPropertyResolution, []string{
						"Verify the config exists: ls -la " + paths.DaemonConfigPath,
						"Reinstall the daemon to recreate the config: sudo solo-provisioner daemon service install",
					})
				}
				return err
			}
			cn := cfg.Components.ConsensusNode
			if flagNodeID != "" {
				cn.NodeID = flagNodeID
			}
			if flagKubeconfig != "" {
				cn.Kubeconfig = flagKubeconfig
			}
			if flagOrbit != "" {
				cn.Orbit = flagOrbit
			}
			if flagUpgradeDir != "" {
				cn.UpgradeDir = flagUpgradeDir
			}
			if err := cfg.Validate(); err != nil {
				if ex := errorx.Cast(err); ex != nil {
					return ex.WithProperty(models.ErrPropertyResolution, []string{
						"Check the config: cat " + paths.DaemonConfigPath,
						"Fix missing fields or reinstall: sudo solo-provisioner daemon service install",
					})
				}
				return err
			}

			d, err := daemon.NewFromConfig(paths, cfg)
			if err != nil {
				if ex := errorx.Cast(err); ex != nil {
					return ex.WithProperty(models.ErrPropertyResolution, []string{
						"Check the daemon config: cat " + paths.DaemonConfigPath,
						"Check the kubeconfig: ls -la " + paths.DaemonCNKubeconfigPath,
						"Reinstall the daemon: sudo solo-provisioner daemon service install",
					})
				}
				return err
			}
			if err := d.Run(ctx); err != nil {
				if ex := errorx.Cast(err); ex != nil {
					return ex.WithProperty(models.ErrPropertyResolution, []string{
						"Check daemon logs: sudo journalctl -u solo-provisioner-daemon -n 100 --no-pager",
						"Check service status: sudo systemctl status solo-provisioner-daemon",
						"Restart the daemon: sudo solo-provisioner daemon service stop && sudo solo-provisioner daemon service start",
					})
				}
				return err
			}

			logx.As().Info().Msg("Solo Provisioner daemon stopped")
			return nil
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "", "Path to config file")
	rootCmd.PersistentFlags().StringVar(&flagLogLevel, "log-level", "", "Set log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVarP(&flagVersion, "version", "v", false, "Print version and exit")
	rootCmd.PersistentFlags().StringVarP(&flagOutputFormat, "output", "o", "json", "Output format (json, yaml)")

	// Optional overrides for daemon.yaml fields. When set, each flag takes
	// precedence over the corresponding value in daemon.yaml. The normal
	// production deployment sets no flags — everything comes from daemon.yaml.
	rootCmd.Flags().StringVar(&flagNodeID, "node-id", "", "Override node_id from daemon.yaml (e.g. 0.0.3)")
	rootCmd.Flags().StringVar(&flagKubeconfig, "kubeconfig", "", "Override kubeconfig path from daemon.yaml")
	rootCmd.Flags().StringVar(&flagOrbit, "orbit", "", "Override orbit (K8s namespace) from daemon.yaml")
	rootCmd.Flags().StringVar(&flagUpgradeDir, "upgrade-dir", "", "Override upgrade_dir from daemon.yaml")

	rootCmd.AddCommand(version.Cmd())
}

// initConfig wires up config, logging, and proxy for the daemon. Mirrors the
// CLI's bootstrap minus the TUI-aware branches — the daemon always uses raw
// (non-interactive) zerolog output. Kept self-contained inside cmd/daemon so
// the daemon binary does not import anything under cmd/cli, satisfying the
// epic's attack-surface constraint.
func initConfig(ctx context.Context) {
	if err := config.Initialize(flagConfig); err != nil {
		doctor.CheckErr(ctx, err)
	}

	logConfig := config.Get().Log
	if flagLogLevel != "" {
		logConfig.Level = flagLogLevel
	}

	logConfig.FileLogging = true
	if logConfig.Directory == "" {
		logConfig.Directory = models.Paths().LogsDir
	}
	// Hardcoded, not config-overridable: the CLI's summary table directs operators
	// to "solo-provisioner-daemon.log" (see cmd/cli/commands/common/run.go), and
	// the CLI + daemon share the same config file but must not share a log file.
	logConfig.Filename = "solo-provisioner-daemon.log"

	// Ensure the log directory exists with setgid + weaver group so files
	// created inside inherit the weaver group automatically.
	svcGID, gidErr := strconv.Atoi(config.WeaverGroupId())
	if mkdirErr := os.MkdirAll(logConfig.Directory, models.DefaultDirOrExecPerm); mkdirErr == nil && gidErr == nil {
		_ = os.Chown(logConfig.Directory, 0, svcGID)
		_ = os.Chmod(logConfig.Directory, models.DefaultStorageDirPerm)
	}

	// Pre-create the log file with world-readable mode (0644) before lumberjack
	// claims it. For files created with wrong ownership in prior runs, fix them.
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
		logConfig.MaxSize = 50
	}
	if logConfig.MaxBackups == 0 {
		logConfig.MaxBackups = 3
	}
	if logConfig.MaxAge == 0 {
		logConfig.MaxAge = 30
	}

	if err := logx.Initialize(logConfig); err != nil {
		doctor.CheckErr(ctx, err)
	}

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

func main() {
	traceId := uuid.NewString()
	ctx := context.WithValue(context.Background(), "traceId", traceId)

	// Handle --version / -v before cobra initialises anything (config, logging,
	// proxy). This keeps the output clean — exactly one JSON line on stdout —
	// which the CLI's daemon-install step parses with exec.Command.Output().
	// Write directly to os.Stdout (not via cobra's cmd.Println) so that the
	// output is captured correctly when invoked via exec.Command.Output().
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			info := version.Get()
			out, err := info.Format("json")
			if err != nil {
				os.Exit(1)
			}
			fmt.Fprintln(os.Stdout, out)
			os.Stdout.Sync() //nolint:errcheck // best-effort flush before os.Exit
			os.Exit(0)
		}
	}

	cobra.OnInitialize(func() {
		initConfig(ctx)
	})

	if _, err := rootCmd.ExecuteContextC(ctx); err != nil {
		doctor.CheckErr(ctx, errorx.IllegalState.Wrap(err, "failed to execute command"))
	}
}
