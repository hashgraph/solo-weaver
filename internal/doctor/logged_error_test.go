// SPDX-License-Identifier: Apache-2.0

package doctor

import (
	"bytes"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/doctor/logview"
	"github.com/stretchr/testify/require"
)

const loggedErrorLine = `{"time":"2026-10-17T00:38:42Z","level":"error","caller":"workflows/run.go:42",` +
	`"pid":4242,"build_version":"1.2.3","build_commit":"abcdef0123456789","reason":"FileMissing",` +
	`"hints":["Check the path.","Then retry."],` +
	`"message":"common.IllegalState: boom {reason: FileMissing}\n at stack.frame\n at another.frame"}`

func TestRenderLoggedError(t *testing.T) {
	e := logview.ParseLine("cli", loggedErrorLine)

	var out bytes.Buffer
	RenderLoggedError(&out, e, "/opt/solo/weaver/logs/solo-provisioner.log", false)
	got := out.String()

	require.Contains(t, got, "Error Stacktrace")
	require.Contains(t, got, "at stack.frame\n at another.frame", "the stack trace is shown verbatim")
	require.Contains(t, got, "Error: common.IllegalState: boom\n", "the first line, without the printable reason")
	require.NotContains(t, got, "{reason: FileMissing}", "the reason is shown on its own line")
	require.Contains(t, got, "Reason: FileMissing")
	require.Contains(t, got, "Time: 2026-10-17T00:38:42Z")
	require.Contains(t, got, "Caller: workflows/run.go:42")
	require.Contains(t, got, "Commit: abcdef0123456789")
	require.Contains(t, got, "Version: 1.2.3")
	require.Contains(t, got, "Pid: 4242")
	require.Contains(t, got, "Logfile: /opt/solo/weaver/logs/solo-provisioner.log")
	require.Contains(t, got, "1. Check the path.")
	require.Contains(t, got, "2. Then retry.")
	require.NotContains(t, got, "\033[", "no color unless asked")
}

func TestRenderLoggedError_Minimal(t *testing.T) {
	e := logview.ParseLine("daemon", `{"level":"error","message":"plain failure"}`)

	var out bytes.Buffer
	RenderLoggedError(&out, e, "", true)
	got := out.String()

	require.Contains(t, got, "plain failure")
	require.Contains(t, got, "\033[", "color when asked")
	for _, absent := range []string{"Reason:", "Time:", "Caller:", "Commit:", "Version:", "Pid:", "Logfile:", "Resolution"} {
		require.NotContains(t, got, absent, "an absent field must not render an empty line")
	}
}
