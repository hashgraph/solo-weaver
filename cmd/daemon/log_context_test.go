// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/automa-saga/logx"
	"github.com/stretchr/testify/require"
)

// captureStdoutLogs points logx's console sink at a pipe (with ConsoleLogging
// false it writes raw JSON to os.Stdout, which it reads at Initialize time),
// runs emit, and returns the decoded lines.
func captureStdoutLogs(t *testing.T, cfg logx.LoggingConfig, emit func()) []map[string]any {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = origStdout
		logx.SetGlobalContext(nil)
		_ = logx.Initialize(logx.LoggingConfig{Level: "debug", ConsoleLogging: true})
	})

	require.NoError(t, logx.Initialize(cfg))
	emit()
	require.NoError(t, w.Close())

	var lines []map[string]any
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &m), "line: %s", sc.Text())
		lines = append(lines, m)
	}
	require.NoError(t, sc.Err())
	return lines
}

// Both the direct logx path and the slog bridge the daemon kernel uses must
// carry the build fields and the real call site.
func TestInstallLogContext_BuildFieldsAndCallerOnBothPaths(t *testing.T) {
	lines := captureStdoutLogs(t, logx.LoggingConfig{Level: "debug", UTC: true, IncludeCaller: true}, func() {
		installLogContext()
		logx.As().Info().Msg("direct")
		slog.Info("bridged")
	})

	require.Len(t, lines, 2)
	for _, l := range lines {
		require.Contains(t, l, "build_version", "line %v", l)
		require.Contains(t, l, "build_commit", "line %v", l)

		caller, _ := l["caller"].(string)
		require.True(t, strings.HasPrefix(caller, "cmd/daemon/log_context_test.go:"), "caller %q", caller)

		ts, _ := l["time"].(string)
		require.True(t, strings.HasSuffix(ts, "Z"), "time %q", ts)
	}
}
