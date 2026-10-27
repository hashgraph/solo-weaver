// SPDX-License-Identifier: Apache-2.0

package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	pkgconfig "github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/stretchr/testify/require"
)

// TestPrintStateWarnings_OneLinePerWarning: human output is one marked line per
// warning; --output=json gives one NDJSON warn record per warning, so a reader
// of the JSON stream can parse every line.
func TestPrintStateWarnings_OneLinePerWarning(t *testing.T) {
	orig := OutputFormat
	t.Cleanup(func() { OutputFormat = orig })
	warnings := []string{"The provisioner version was not recorded: disk full.", `quote " inside`}

	OutputFormat = "text"
	var human bytes.Buffer
	PrintStateWarnings(&human, warnings)
	require.Equal(t, "⚠ "+warnings[0]+"\n⚠ "+warnings[1]+"\n", human.String())

	OutputFormat = "json"
	var machine bytes.Buffer
	PrintStateWarnings(&machine, warnings)
	lines := strings.Split(strings.TrimSuffix(machine.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	for i, line := range lines {
		var rec map[string]string
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		require.Equal(t, map[string]string{"level": "warn", "message": warnings[i]}, rec)
	}

	var none bytes.Buffer
	PrintStateWarnings(&none, nil)
	require.Empty(t, none.String())
}

// TestStateWarnings_StartupPassWarningShowsOnce: a warning the startup pass
// queues shows once: in the run summary when the command's workflow ran, and
// otherwise in the drain when the command ends.
func TestStateWarnings_StartupPassWarningShowsOnce(t *testing.T) {
	saved := pkgconfig.Get()
	t.Cleanup(func() { _ = pkgconfig.Set(&saved) })
	cfg := pkgconfig.Get()
	cfg.Log.Directory = t.TempDir()
	require.NoError(t, pkgconfig.Set(&cfg))
	orig := OutputFormat
	t.Cleanup(func() { OutputFormat = orig })
	OutputFormat = "text"
	state.TakeWarnings()
	t.Cleanup(func() { state.TakeWarnings() })

	failedStamp := func() (bool, error) { return false, errors.New("disk full") }
	noMigrations := func() (state.StartupPassRun, error) { return state.StartupPassRun{}, nil }

	// A workflow ran: its summary takes the warning, and the end prints nothing.
	require.NoError(t, state.RecordStartupPass(t.TempDir(), &state.StartupPass{}, failedStamp, noMigrations))
	r := automa.StepSuccessReport("root")
	require.NoError(t, finalizeWorkflowReport(r))
	require.Len(t, ui.CollectWarnings(r), 1)
	var end bytes.Buffer
	PrintStateWarnings(&end, state.TakeWarnings())
	require.Empty(t, end.String())

	// No workflow ran: the end prints it, once.
	require.NoError(t, state.RecordStartupPass(t.TempDir(), &state.StartupPass{}, failedStamp, noMigrations))
	PrintStateWarnings(&end, state.TakeWarnings())
	require.Equal(t, 1, strings.Count(end.String(), "The provisioner version was not recorded"))
}
