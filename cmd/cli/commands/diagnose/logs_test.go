// SPDX-License-Identifier: Apache-2.0

package diagnose

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

func logLine(ts, level, msg, extra string) string {
	return `{"time":"` + ts + `","level":"` + level + `","caller":"c.go:1","message":"` + msg + `"` + extra + `}`
}

// setupLogs re-roots the weaver paths at a temp dir holding one CLI and one
// daemon log.
func setupLogs(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Cleanup(models.SetPaths(home))

	dir := models.Paths().LogsDir
	require.NoError(t, os.MkdirAll(dir, 0o755))

	cli := strings.Join([]string{
		logLine("2026-10-17T00:00:01Z", "info", "cli one", `,"pid":1`),
		logLine("2026-10-17T00:00:03Z", "error", "cli three", `,"reason":"Boom"`),
		"not json",
	}, "\n") + "\n"
	daemon := logLine("2026-10-17T00:00:02Z", "debug", "daemon two", `,"reason":"Watch"`) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, cliLogFile), []byte(cli), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, daemonLogFile), []byte(daemon), 0o644))
}

func runLogsCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newLogsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestLogsDefaultShowsCLITail(t *testing.T) {
	setupLogs(t)

	out, err := runLogsCmd(t)
	require.NoError(t, err)
	require.Equal(t, strings.Join([]string{
		"2026-10-17T00:00:01Z INF c.go:1 > cli one pid=1",
		"2026-10-17T00:00:03Z ERR c.go:1 > cli three reason=Boom",
		"not json",
	}, "\n")+"\n", out)
}

func TestLogsTailAndHead(t *testing.T) {
	setupLogs(t)

	out, err := runLogsCmd(t, "-n", "1")
	require.NoError(t, err)
	require.Equal(t, "not json\n", out)

	out, err = runLogsCmd(t, "--head", "1")
	require.NoError(t, err)
	require.Contains(t, out, "cli one")
	require.NotContains(t, out, "cli three")
}

func TestLogsSourceAllMergesByTime(t *testing.T) {
	setupLogs(t)

	out, err := runLogsCmd(t, "--source", "all", "--level", "debug")
	require.NoError(t, err)
	require.Equal(t, strings.Join([]string{
		"[cli] 2026-10-17T00:00:01Z INF c.go:1 > cli one pid=1",
		"[daemon] 2026-10-17T00:00:02Z DBG c.go:1 > daemon two reason=Watch",
		"[cli] 2026-10-17T00:00:03Z ERR c.go:1 > cli three reason=Boom",
	}, "\n")+"\n", out)
}

func TestLogsFilters(t *testing.T) {
	setupLogs(t)

	out, err := runLogsCmd(t, "--source", "all", "--field", "reason=Watch")
	require.NoError(t, err)
	require.Contains(t, out, "daemon two")
	require.NotContains(t, out, "cli")

	out, err = runLogsCmd(t, "--level", "error", "--message", "three")
	require.NoError(t, err)
	require.Contains(t, out, "cli three")
	require.NotContains(t, out, "cli one")
}

func TestLogsJSONOutput(t *testing.T) {
	setupLogs(t)
	prev := common.OutputFormat
	common.OutputFormat = "json"
	t.Cleanup(func() { common.OutputFormat = prev })

	out, err := runLogsCmd(t)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2, "the non-JSON line must be dropped")
	require.Equal(t, logLine("2026-10-17T00:00:01Z", "info", "cli one", `,"pid":1`), lines[0])
}

func TestLogsMissingFileIsEmpty(t *testing.T) {
	t.Cleanup(models.SetPaths(t.TempDir()))

	out, err := runLogsCmd(t)
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestLogsRejectsBadFlags(t *testing.T) {
	setupLogs(t)

	for _, args := range [][]string{
		{"--source", "nope"},
		{"--level", "loud"},
		{"--since", "yesterday"},
		{"--field", "novalue"},
		{"--grep", "("},
		{"-n", "-1"},
		{"-n", "5", "--head", "5"},
		{"--head", "5", "-f"},
	} {
		_, err := runLogsCmd(t, args...)
		require.Error(t, err, "%v", args)
	}
}

const errorLine = `{"time":"2026-10-17T00:00:05Z","level":"error","caller":"c.go:9","pid":9,` +
	`"build_version":"1.0.0","build_commit":"abc123","reason":"FileMissing","hints":["Check the path."],` +
	`"message":"common.IllegalState: boom {reason: FileMissing}\n at frame.one"}`

func appendLog(t *testing.T, name, line string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(models.Paths().LogsDir, name), os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(line + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func TestLogsLastError(t *testing.T) {
	setupLogs(t)

	out, err := runLogsCmd(t, "--last-error")
	require.NoError(t, err)
	require.Contains(t, out, "cli three", "the only error-level entry in the CLI log")
	require.Contains(t, out, "Reason: Boom")
	require.NotContains(t, out, "cli one")

	appendLog(t, cliLogFile, errorLine)
	out, err = runLogsCmd(t, "--last-error")
	require.NoError(t, err)
	require.Contains(t, out, "at frame.one")
	require.Contains(t, out, "1. Check the path.")
	require.Contains(t, out, "Logfile: "+filepath.Join(models.Paths().LogsDir, cliLogFile))
	require.NotContains(t, out, "cli three", "only the newest error")
}

func TestLogsLastErrorAcrossSourcesAndFilters(t *testing.T) {
	setupLogs(t)
	appendLog(t, daemonLogFile, strings.Replace(errorLine, "00:00:05Z", "00:00:09Z", 1))

	out, err := runLogsCmd(t, "--last-error", "--source", "all")
	require.NoError(t, err)
	require.Contains(t, out, "Logfile: "+filepath.Join(models.Paths().LogsDir, daemonLogFile), "newest error is in the daemon log")

	out, err = runLogsCmd(t, "--last-error", "--source", "all", "--field", "reason=Boom")
	require.NoError(t, err)
	require.Contains(t, out, "cli three", "filters narrow which error is the last")

	out, err = runLogsCmd(t, "--last-error", "--grep", "no-such-text")
	require.NoError(t, err)
	require.Equal(t, "No error entries found.\n", out)
}

func TestLogsLastErrorNone(t *testing.T) {
	t.Cleanup(models.SetPaths(t.TempDir()))

	out, err := runLogsCmd(t, "--last-error")
	require.NoError(t, err)
	require.Equal(t, "No error entries found.\n", out)
}

func TestLogsLastErrorJSON(t *testing.T) {
	setupLogs(t)
	appendLog(t, cliLogFile, errorLine)
	prev := common.OutputFormat
	common.OutputFormat = "json"
	t.Cleanup(func() { common.OutputFormat = prev })

	out, err := runLogsCmd(t, "--last-error")
	require.NoError(t, err)
	require.Equal(t, errorLine+"\n", out)

	out, err = runLogsCmd(t, "--last-error", "--grep", "no-such-text")
	require.NoError(t, err)
	require.Empty(t, out, "JSON mode prints nothing when there is no match")
}

func TestLogsLastErrorRejectsConflictingFlags(t *testing.T) {
	setupLogs(t)

	for _, conflict := range [][]string{{"-n", "5"}, {"--head", "5"}, {"-f"}, {"--level", "warn"}} {
		_, err := runLogsCmd(t, append([]string{"--last-error"}, conflict...)...)
		require.Error(t, err, "%v", conflict)
	}
}

func TestLogsReadsDoubleEncodedDaemonLog(t *testing.T) {
	setupLogs(t)
	inner := `{"level":"warn","caller":"d.go:2","time":"2026-10-17T00:00:07Z","reason":"Watch","message":"wrapped entry"}`
	appendLog(t, daemonLogFile, `{"time":"2026-10-17T00:00:07Z","message":`+strconv.Quote(inner)+`}`)

	out, err := runLogsCmd(t, "--source", "daemon", "--level", "warn", "--field", "reason=Watch")
	require.NoError(t, err)
	require.Equal(t, "2026-10-17T00:00:07Z WRN d.go:2 > wrapped entry reason=Watch\n", out)
}
