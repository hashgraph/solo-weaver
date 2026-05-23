// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package eventlog_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon/eventlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleEvent(reason string) eventlog.Event {
	return eventlog.Event{
		Ts:          time.Date(2026, 4, 15, 14, 30, 0, 0, time.UTC),
		Level:       eventlog.LevelInfo,
		Reason:      reason,
		Msg:         "test message",
		OperationID: "upgrade-20260415T143000-v0.75.0",
		NodeID:      "0.0.3",
	}
}

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var lines []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &m))
		lines = append(lines, m)
	}
	require.NoError(t, sc.Err())
	return lines
}

func Test_NewOperation_WritesAndFsyncs(t *testing.T) {
	dir := t.TempDir()
	const opID = "upgrade-20260415T143000-v0.75.0"

	l, err := eventlog.NewOperation(dir, opID)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(l.Path()), "Path() must be absolute")
	assert.Equal(t, filepath.Join(dir, "consensus-"+opID+".jsonl"), l.Path())

	require.NoError(t, l.Log(sampleEvent("ExecuteWorkflowStarted")))
	require.NoError(t, l.Log(sampleEvent("FilesPlaced")))
	require.NoError(t, l.Close())

	lines := readLines(t, l.Path())
	require.Len(t, lines, 2)
	assert.Equal(t, "ExecuteWorkflowStarted", lines[0]["reason"])
	assert.Equal(t, "FilesPlaced", lines[1]["reason"])
	assert.Equal(t, opID, lines[0]["operationId"])
	assert.Equal(t, "0.0.3", lines[0]["nodeId"])
}

func Test_NewOperation_TruncatesPreviousFile(t *testing.T) {
	dir := t.TempDir()
	const opID = "upgrade-20260415T143000-v0.75.0"

	l, err := eventlog.NewOperation(dir, opID)
	require.NoError(t, err)
	require.NoError(t, l.Log(sampleEvent("ExecuteWorkflowStarted")))
	require.NoError(t, l.Close())

	// Open again — must start fresh, not append.
	l2, err := eventlog.NewOperation(dir, opID)
	require.NoError(t, err)
	require.NoError(t, l2.Log(sampleEvent("ExecuteWorkflowCompleted")))
	require.NoError(t, l2.Close())

	lines := readLines(t, l2.Path())
	require.Len(t, lines, 1, "NewOperation must truncate; file should contain only the second session's event")
	assert.Equal(t, "ExecuteWorkflowCompleted", lines[0]["reason"])
}

func Test_NewAppend_AppendsAcrossOpens(t *testing.T) {
	dir := t.TempDir()
	const fileName = "consensus-migrate-events.jsonl"

	l, err := eventlog.NewAppend(dir, fileName)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(l.Path()), "Path() must be absolute")
	assert.Equal(t, filepath.Join(dir, fileName), l.Path())
	require.NoError(t, l.Log(sampleEvent("MigrationStarted")))
	require.NoError(t, l.Close())

	l2, err := eventlog.NewAppend(dir, fileName)
	require.NoError(t, err)
	require.NoError(t, l2.Log(sampleEvent("MigrationCompleted")))
	require.NoError(t, l2.Close())

	lines := readLines(t, l.Path())
	require.Len(t, lines, 2, "NewAppend must accumulate entries across opens")
	assert.Equal(t, "MigrationStarted", lines[0]["reason"])
	assert.Equal(t, "MigrationCompleted", lines[1]["reason"])
}

func Test_Log_RejectsEventWithMissingFields(t *testing.T) {
	dir := t.TempDir()
	l, err := eventlog.NewOperation(dir, "upgrade-20260415T143000-v0.75.0")
	require.NoError(t, err)
	defer l.Close()

	cases := []struct {
		name  string
		event eventlog.Event
	}{
		{"zero Ts", eventlog.Event{Level: eventlog.LevelInfo, Reason: "R", Msg: "M", OperationID: "op", NodeID: "node"}},
		{"empty Level", eventlog.Event{Ts: time.Now(), Reason: "R", Msg: "M", OperationID: "op", NodeID: "node"}},
		{"empty Reason", eventlog.Event{Ts: time.Now(), Level: eventlog.LevelInfo, Msg: "M", OperationID: "op", NodeID: "node"}},
		{"empty Msg", eventlog.Event{Ts: time.Now(), Level: eventlog.LevelInfo, Reason: "R", OperationID: "op", NodeID: "node"}},
		{"empty OperationID", eventlog.Event{Ts: time.Now(), Level: eventlog.LevelInfo, Reason: "R", Msg: "M", NodeID: "node"}},
		{"empty NodeID", eventlog.Event{Ts: time.Now(), Level: eventlog.LevelInfo, Reason: "R", Msg: "M", OperationID: "op"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := l.Log(tc.event)
			assert.Error(t, err, "Log must reject event with %s", tc.name)
		})
	}
}

func Test_Log_ConcurrentWritesProduceValidLines(t *testing.T) {
	dir := t.TempDir()

	l, err := eventlog.NewOperation(dir, "upgrade-20260415T143000-v0.75.0")
	require.NoError(t, err)
	defer l.Close()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = l.Log(sampleEvent("ConcurrentEvent"))
		}()
	}
	wg.Wait()
	require.NoError(t, l.Close())

	lines := readLines(t, l.Path())
	assert.Len(t, lines, goroutines, "every concurrent write must produce exactly one valid JSON line")
}

func Test_PruneOldest_RemovesFilesOlderThanMaxAge(t *testing.T) {
	dir := t.TempDir()

	// Filenames encode 2024 dates — well outside 365 days from 2026.
	old1 := filepath.Join(dir, "consensus-upgrade-20240101T000000-v0.70.0.jsonl")
	old2 := filepath.Join(dir, "consensus-upgrade-20240601T000000-v0.71.0.jsonl")
	recent := filepath.Join(dir, "consensus-upgrade-20260415T143000-v0.75.0.jsonl")

	for _, p := range []string{old1, old2, recent} {
		require.NoError(t, os.WriteFile(p, []byte("{}"), 0o640))
	}

	require.NoError(t, eventlog.PruneOldest(dir, "consensus-upgrade-*.jsonl", 365*24*time.Hour, 50))

	assert.NoFileExists(t, old1)
	assert.NoFileExists(t, old2)
	assert.FileExists(t, recent)
}

func Test_PruneOldest_EnforcesHardCap(t *testing.T) {
	dir := t.TempDir()

	// All filenames within the last year but cap is 3.
	names := []string{
		"consensus-upgrade-20260101T000000-v0.71.0.jsonl",
		"consensus-upgrade-20260201T000000-v0.72.0.jsonl",
		"consensus-upgrade-20260301T000000-v0.73.0.jsonl",
		"consensus-upgrade-20260401T000000-v0.74.0.jsonl",
		"consensus-upgrade-20260415T143000-v0.75.0.jsonl",
	}
	for _, n := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o640))
	}

	require.NoError(t, eventlog.PruneOldest(dir, "consensus-upgrade-*.jsonl", 365*24*time.Hour, 3))

	assert.NoFileExists(t, filepath.Join(dir, names[0]), "oldest should be pruned")
	assert.NoFileExists(t, filepath.Join(dir, names[1]), "second oldest should be pruned")
	assert.FileExists(t, filepath.Join(dir, names[2]))
	assert.FileExists(t, filepath.Join(dir, names[3]))
	assert.FileExists(t, filepath.Join(dir, names[4]))
}

func Test_PruneOldest_BothConditionsApplied(t *testing.T) {
	dir := t.TempDir()

	// 2 files with 2024 dates (old) + 4 files with 2026 dates (recent); cap is 3.
	// Expect: 2 old removed by age, 1 more removed by cap → 3 remain.
	old1 := filepath.Join(dir, "consensus-upgrade-20240101T000000-v0.70.0.jsonl")
	old2 := filepath.Join(dir, "consensus-upgrade-20240601T000000-v0.71.0.jsonl")
	recent := []string{
		"consensus-upgrade-20260101T000000-v0.72.0.jsonl",
		"consensus-upgrade-20260201T000000-v0.73.0.jsonl",
		"consensus-upgrade-20260301T000000-v0.74.0.jsonl",
		"consensus-upgrade-20260415T143000-v0.75.0.jsonl",
	}

	for _, p := range []string{old1, old2} {
		require.NoError(t, os.WriteFile(p, []byte("{}"), 0o640))
	}
	for _, n := range recent {
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o640))
	}

	require.NoError(t, eventlog.PruneOldest(dir, "consensus-upgrade-*.jsonl", 365*24*time.Hour, 3))

	assert.NoFileExists(t, old1)
	assert.NoFileExists(t, old2)
	assert.NoFileExists(t, filepath.Join(dir, recent[0]), "oldest recent should be pruned to satisfy cap")
	assert.FileExists(t, filepath.Join(dir, recent[1]))
	assert.FileExists(t, filepath.Join(dir, recent[2]))
	assert.FileExists(t, filepath.Join(dir, recent[3]))
}
