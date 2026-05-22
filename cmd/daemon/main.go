// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"os/signal"
	"path"
	"strconv"
	"syscall"

	"github.com/automa-saga/logx"
	"github.com/google/uuid"
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

	rootCmd = &cobra.Command{
		Use:   "solo-provisioner-daemon",
		Short: "Long-running daemon for Solo Provisioner host-level work",
		Long:  "Long-lived foreground process started by the solo-provisioner-daemon.service systemd unit.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagVersion {
				return version.Print(cmd, flagOutputFormat)
			}

			logx.As().Info().Msg("Solo Provisioner daemon started")

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

			select {
			case sig := <-sigChan:
				logx.As().Info().Str("signal", sig.String()).Msg("Solo Provisioner daemon shutting down")
			case <-cmd.Context().Done():
				logx.As().Info().Msg("Solo Provisioner daemon context cancelled")
			}

			return nil
		},
	}
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagConfig, "config", "c", "", "Path to config file")
	rootCmd.PersistentFlags().StringVar(&flagLogLevel, "log-level", "", "Set log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVarP(&flagVersion, "version", "v", false, "Print version and exit")
	rootCmd.PersistentFlags().StringVarP(&flagOutputFormat, "output", "o", "json", "Output format (json, yaml)")

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

	// Pre-create the log file with group-readable mode (0640) before lumberjack
	// claims it. For files created with wrong ownership in prior runs, fix them.
	logFilePath := path.Join(logConfig.Directory, logConfig.Filename)
	if _, statErr := os.Stat(logFilePath); os.IsNotExist(statErr) {
		if f, createErr := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY, 0o640); createErr == nil {
			_ = f.Close()
		}
	} else if statErr == nil && gidErr == nil {
		_ = os.Chown(logFilePath, 0, svcGID)
		_ = os.Chmod(logFilePath, 0o640)
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

	cobra.OnInitialize(func() {
		initConfig(ctx)
	})

	if _, err := rootCmd.ExecuteContextC(ctx); err != nil {
		doctor.CheckErr(ctx, errorx.IllegalState.Wrap(err, "failed to execute command"))
	}
}
