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

	"github.com/hashgraph/solo-weaver/pkg/eventlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleEvent(reason string) eventlog.Event {
	return eventlog.Event{
		Ts:          time.Date(2026, 4, 15, 14, 30, 0, 0, time.UTC),
		Level:       eventlog.LevelInfo,
		Reason:      reason,
		Msg:         "test message",
		OperationID: "upgrade-20260415T143000Z-v0.75.0",
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
	const opID = "upgrade-20260415T143000Z-v0.75.0"

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
	const opID = "upgrade-20260415T143000Z-v0.75.0"

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

func Test_NewOperation_RejectsUnsafeOperationID(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"../escape", "/absolute", "sub/dir"} {
		_, err := eventlog.NewOperation(dir, bad)
		assert.Error(t, err, "NewOperation must reject operationID %q", bad)
	}
}

func Test_NewAppend_RejectsUnsafeFileName(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"../escape.jsonl", "/absolute.jsonl", "sub/dir.jsonl"} {
		_, err := eventlog.NewAppend(dir, bad)
		assert.Error(t, err, "NewAppend must reject fileName %q", bad)
	}
}

func Test_Log_RejectsEventWithMissingFields(t *testing.T) {
	dir := t.TempDir()
	l, err := eventlog.NewOperation(dir, "upgrade-20260415T143000Z-v0.75.0")
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

	l, err := eventlog.NewOperation(dir, "upgrade-20260415T143000Z-v0.75.0")
	require.NoError(t, err)

	const goroutines = 20
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			errs <- l.Log(sampleEvent("ConcurrentEvent"))
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.NoError(t, l.Close())

	lines := readLines(t, l.Path())
	assert.Len(t, lines, goroutines, "every concurrent write must produce exactly one valid JSON line")
}
