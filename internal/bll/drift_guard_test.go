// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package bll

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/drift"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// destroyingIntent is a noopIntent whose action destroys the owned drift
// destroys reports, as an uninstall does.
type destroyingIntent struct {
	noopIntent
	destroys func(drift.Change) bool
}

func (d destroyingIntent) DestroysDrift(_ models.UserInputs[struct{}], c drift.Change) bool {
	return d.destroys(c)
}

// destroysAll destroys every owned change, as an uninstall does.
func destroysAll(drift.Change) bool { return true }

// destroysNone destroys no owned change, as an uninstall does to drift on a
// part it doesn't remove.
func destroysNone(drift.Change) bool { return false }

// recordedCommon is the common inputs an action history entry records.
func recordedCommon(t *testing.T, e state.ActionHistory) map[string]any {
	t.Helper()
	inputs, ok := e.Inputs.(map[string]any)
	require.True(t, ok)
	common, ok := inputs["common"].(map[string]any)
	require.True(t, ok)
	return common
}

// configuredByHandHost is a host whose node agent was configured out of band,
// against a persisted agent that is installed but not configured.
func configuredByHandHost(t *testing.T) (string, *host) {
	t.Helper()
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	persisted := configuredNodeAgent()
	persisted.NodeAgent.Configured = false
	writeState(t, stateFile, persisted)
	return stateFile, &host{teleport: configuredNodeAgent()}
}

// A destructive action stops before its workflow when owned drift would be
// destroyed. The error names each field, and nothing is flushed or recorded,
// so the next run still sees the drift.
func TestHandleIntent_DestructiveActionStopsOnOwnedDrift(t *testing.T) {
	stateFile, h := configuredByHandHost(t)
	dir := filepath.Dir(stateFile)
	ran := false

	_, err := handleIntent(t, stateFile, h, destroyingIntent{noopIntent{execute: func() { ran = true }}, destroysAll}, commonInputs(), owns(Teleport))

	require.Error(t, err)
	require.False(t, ran, "the workflow must not run")
	require.False(t, readState(t, stateFile).TeleportState.NodeAgent.Configured, "nothing was flushed")
	require.NoFileExists(t, filepath.Join(dir, "action_history.yaml"))

	require.True(t, errorx.IsOfType(err, errorx.IllegalState))
	require.Equal(t, "blocknode install would overwrite out-of-band changes to teleport:\n"+
		`  teleport nodeAgent.configured differs from persisted state: recorded "false", live "true"`,
		errorx.Cast(err).Message())
	require.Contains(t, doctor.OperatorMessage(err), `teleport nodeAgent.configured differs from persisted state: recorded "false", live "true"`)
	require.NotContains(t, doctor.OperatorMessage(err), "reason")

	reason, ok := errx.ReasonOf(err)
	require.True(t, ok)
	require.Equal(t, reasons.PreconditionNotMet, reason)
	hints, ok := errx.Hints(err)
	require.True(t, ok)
	require.Equal(t, []string{
		"Re-run the same command with --override-drift to proceed; the overwritten values are recorded in " + filepath.Join(dir, "action_history.yaml"),
	}, hints)
}

// readActionHistory decodes every entry of dir's action history.
func readActionHistory(t *testing.T, dir string) []state.ActionHistory {
	t.Helper()
	b, err := os.ReadFile(state.ActionHistoryFilePath(dir))
	require.NoError(t, err)
	var entries []state.ActionHistory
	dec := yaml.NewDecoder(bytes.NewReader(b))
	for {
		var e state.ActionHistory
		err := dec.Decode(&e)
		if errors.Is(err, io.EOF) {
			return entries
		}
		require.NoError(t, err)
		entries = append(entries, e)
	}
}

// The override lets a destructive action run over the drift it destroys, and
// its history entry records each field; its inputs record the override.
func TestHandleIntent_OverrideRunsAndRecordsDestroyedDrift(t *testing.T) {
	stateFile, h := configuredByHandHost(t)
	common := commonInputs()
	common.OverrideDrift = true
	ran := false

	report, err := handleIntent(t, stateFile, h, destroyingIntent{noopIntent{execute: func() { ran = true }}, destroysAll}, common, owns(Teleport))

	require.NoError(t, err)
	require.NoError(t, report.Error)
	require.True(t, ran)
	entries := readActionHistory(t, filepath.Dir(stateFile))
	require.Len(t, entries, 1)
	require.Equal(t, []state.OwnedDriftRecord{{
		Component: "teleport", Field: "nodeAgent.configured", Persisted: "false", Live: "true", Destroyed: true,
	}}, entries[0].OwnedDrift)
	require.Equal(t, true, recordedCommon(t, entries[0])["overridedrift"])
}

// --force, like the override, lets a destructive action destroy owned drift.
func TestHandleIntent_ForceRunsOverDestroyedDrift(t *testing.T) {
	stateFile, h := configuredByHandHost(t)
	common := commonInputs()
	common.Force = true

	report, err := handleIntent(t, stateFile, h, destroyingIntent{destroys: destroysAll}, common, owns(Teleport))

	require.NoError(t, err)
	require.NoError(t, report.Error)
	entry := readActionHistory(t, filepath.Dir(stateFile))[0]
	require.True(t, entry.OwnedDrift[0].Destroyed)
	require.Equal(t, true, recordedCommon(t, entry)["force"])
}

// A non-interactive run (--non-interactive, --output json) goes ahead over
// drift it destroys.
func TestHandleIntent_NonInteractiveRunsOverDestroyedDrift(t *testing.T) {
	stateFile, h := configuredByHandHost(t)
	common := commonInputs()
	common.NonInteractive = true

	report, err := handleIntent(t, stateFile, h, destroyingIntent{destroys: destroysAll}, common, owns(Teleport))

	require.NoError(t, err)
	require.NoError(t, report.Error)
	entry := readActionHistory(t, filepath.Dir(stateFile))[0]
	require.True(t, entry.OwnedDrift[0].Destroyed)
	require.Equal(t, true, recordedCommon(t, entry)["noninteractive"])
}

// A run that goes ahead over owned drift logs each change at warn level before
// its workflow runs, so the log has it even if the flush never writes the
// history entry.
func TestHandleIntent_GoingAheadOverOwnedDriftWarnsBeforeTheWorkflow(t *testing.T) {
	stateFile, h := configuredByHandHost(t)
	logs := captureLogs(t)
	common := commonInputs()
	common.OverrideDrift = true

	_, err := handleIntent(t, stateFile, h, destroyingIntent{
		noopIntent{execute: func() { logx.As().Info().Msg("workflow ran") }}, destroysAll,
	}, common, owns(Teleport))

	require.NoError(t, err)
	warn := `{"level":"warn","component":"teleport","field":"nodeAgent.configured","persisted":"false","live":"true","destroyed":true,"message":"Going ahead over an out-of-band change"}`
	require.Contains(t, logs.String(), warn)
	require.Less(t, strings.Index(logs.String(), warn), strings.Index(logs.String(), "workflow ran"))
}

// A handler without DriftDestroyer converges: an interactive run goes ahead
// over owned drift, and the entry records the drift as not destroyed.
func TestHandleIntent_HandlerWithoutDriftDestroyerConvergesOverOwnedDrift(t *testing.T) {
	stateFile, h := configuredByHandHost(t)

	report, err := handleIntent(t, stateFile, h, noopIntent{}, commonInputs(), owns(Teleport))

	require.NoError(t, err)
	require.NoError(t, report.Error)
	entry := readActionHistory(t, filepath.Dir(stateFile))[0]
	require.Equal(t, []state.OwnedDriftRecord{{
		Component: "teleport", Field: "nodeAgent.configured", Persisted: "false", Live: "true",
	}}, entry.OwnedDrift)
	require.NotContains(t, recordedCommon(t, entry), "noninteractive", "unset guard flags are left out of the inputs")
	require.NotContains(t, recordedCommon(t, entry), "overridedrift")
}

// A destructive action never stops for owned drift it doesn't destroy, such
// as the other Teleport agent's: the entry records it as not destroyed.
func TestHandleIntent_DriftTheActionDoesNotDestroyNeverStopsIt(t *testing.T) {
	stateFile, h := configuredByHandHost(t)

	report, err := handleIntent(t, stateFile, h, destroyingIntent{destroys: destroysNone}, commonInputs(), owns(Teleport))

	require.NoError(t, err)
	require.NoError(t, report.Error)
	entry := readActionHistory(t, filepath.Dir(stateFile))[0]
	require.Equal(t, []state.OwnedDriftRecord{{
		Component: "teleport", Field: "nodeAgent.configured", Persisted: "false", Live: "true",
	}}, entry.OwnedDrift)
}

// Drift on a component the command does not own never stops it.
func TestHandleIntent_UnownedDriftNeverStopsADestructiveAction(t *testing.T) {
	stateFile, h := configuredByHandHost(t)

	report, err := handleIntent(t, stateFile, h, destroyingIntent{destroys: destroysAll}, commonInputs(), owns(BlockNode))

	require.NoError(t, err)
	require.NoError(t, report.Error)
	require.Empty(t, readActionHistory(t, filepath.Dir(stateFile))[0].OwnedDrift)
}

// The action's own preconditions come first: a run that would fail anyway is
// not told to override drift.
func TestHandleIntent_PreconditionErrorWinsOverTheDriftGuard(t *testing.T) {
	stateFile, h := configuredByHandHost(t)
	precondition := errors.New("block node is not installed")

	_, err := handleIntent(t, stateFile, h, destroyingIntent{noopIntent{buildErr: precondition}, destroysAll}, commonInputs(), owns(Teleport))

	require.ErrorIs(t, err, precondition)
}

// A removal made out of band leaves a destructive action nothing of it to
// destroy: the run goes ahead and records it as not destroyed.
func TestHandleIntent_RemovalMadeOutOfBandIsNeverDestroyed(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())
	h := &host{}

	report, err := handleIntent(t, stateFile, h, destroyingIntent{destroys: destroysAll}, commonInputs(), owns(Teleport))

	require.NoError(t, err)
	require.NoError(t, report.Error)
	entry := readActionHistory(t, filepath.Dir(stateFile))[0]
	require.Equal(t, []state.OwnedDriftRecord{{
		Component: "teleport", Field: "nodeAgent.installed", Persisted: "true", Live: "false",
	}}, entry.OwnedDrift)
}

// A run whose workflow fails to build never logs that it went ahead.
func TestHandleIntent_WorkflowBuildFailureLogsNoGoingAhead(t *testing.T) {
	stateFile, h := configuredByHandHost(t)
	logs := captureLogs(t)
	common := commonInputs()
	common.OverrideDrift = true

	_, err := handleIntent(t, stateFile, h, destroyingIntent{noopIntent{noSteps: true}, destroysAll}, common, owns(Teleport))

	require.ErrorContains(t, err, "failed to build workflow")
	require.NotContains(t, logs.String(), "Going ahead over an out-of-band change")
}

// keepClusterAgent is a node command's flush scope: the cluster agent stays as
// it was on disk.
func keepClusterAgent(live *state.State, baseline state.State) {
	live.TeleportState.ClusterAgent = baseline.TeleportState.ClusterAgent
}

// A run that keeps one Teleport agent at its baseline leaves a hand change to
// it on record, and records nothing about it in its own entry; that agent's own
// destructive command still stops for it.
func TestHandleIntent_KeptAtBaselineLeavesTheOtherAgentsDriftOnRecord(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, installedClusterAgent())
	h := &host{teleport: installedClusterAgent()}
	h.teleport.ClusterAgent.ChartVersion = "18.7.1-hotfix"

	report, err := handleIntent(t, stateFile, h, noopIntent{}, commonInputs(), owns(Teleport),
		WithKeptAtBaseline[struct{}](keepClusterAgent, drift.OnTeleportClusterAgent))
	require.NoError(t, err)
	require.Equal(t, []string{
		`teleport clusterAgent.chartVersion differs from persisted state: recorded "18.6.4", live "18.7.1-hotfix"`,
	}, ui.CollectWarnings(report), "the summary still warns about the kept change")
	require.Equal(t, "18.6.4", readState(t, stateFile).TeleportState.ClusterAgent.ChartVersion)
	require.Empty(t, readActionHistory(t, filepath.Dir(stateFile))[0].OwnedDrift, "the run kept the change, so it records none")

	_, err = handleIntent(t, stateFile, h, destroyingIntent{destroys: destroysAll}, commonInputs(), owns(Teleport))
	require.Error(t, err)
}
