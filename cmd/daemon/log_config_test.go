// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/automa-saga/logx"
	"github.com/stretchr/testify/require"
)

func TestDaemonLogConfig_ForcesJSONConsoleOverDefaults(t *testing.T) {
	cfg := daemonLogConfig(logx.LoggingConfig{Level: "Debug", ConsoleLogging: true, Directory: t.TempDir()}, "")

	require.False(t, cfg.ConsoleLogging, "journald must get JSON, not the coloured console writer")
	require.True(t, cfg.FileLogging)
	require.Equal(t, "solo-provisioner-daemon.log", cfg.Filename)
	require.Equal(t, "Debug", cfg.Level, "no flag keeps the default level")
}

func TestDaemonLogConfig_LevelFlagOverridesDefault(t *testing.T) {
	cfg := daemonLogConfig(logx.LoggingConfig{Level: "Debug", Directory: t.TempDir()}, "info")
	require.Equal(t, "info", cfg.Level)
}

// End to end: what the daemon writes to stdout, and so to journald, parses as
// JSON line by line, with the fields ops alert on intact.
func TestDaemonLogConfig_StdoutIsJSON(t *testing.T) {
	cfg := daemonLogConfig(logx.LoggingConfig{Level: "info", ConsoleLogging: true, Directory: t.TempDir()}, "")

	lines := captureStdoutLogs(t, cfg, func() {
		logx.As().Info().Str("reason", "TrafficShaperStatuszTick").Str("outcome", "failed").Msg("statusz tick")
	})

	require.Len(t, lines, 1)
	require.Equal(t, "TrafficShaperStatuszTick", lines[0]["reason"])
	require.Equal(t, "failed", lines[0]["outcome"])
}
