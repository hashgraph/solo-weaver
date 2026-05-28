// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/hashgraph/solo-weaver/pkg/eventlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test helpers ---

// newTestLogger creates a real EventLogger writing to a temp file and returns
// the logger and the file path. The caller must close the logger when done.
func newTestLogger(t *testing.T) (*eventlog.EventLogger, string) {
	t.Helper()
	dir := t.TempDir()
	logger, err := eventlog.NewAppend(dir, "test-events.jsonl")
	require.NoError(t, err)
	return logger, logger.Path()
}

// readEvents reads all JSON lines from the given JSONL file and returns them
// as a slice of maps.
func readEvents(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var events []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var m map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &m))
		events = append(events, m)
	}
	require.NoError(t, scanner.Err())
	return events
}

// hasReason returns true if any event in the slice has the given reason.
func hasReason(events []map[string]any, reason string) bool {
	for _, e := range events {
		if e["reason"] == reason {
			return true
		}
	}
	return false
}

// countReason counts events with the given reason.
func countReason(events []map[string]any, reason string) int {
	n := 0
	for _, e := range events {
		if e["reason"] == reason {
			n++
		}
	}
	return n
}

// testRequest returns a valid SoakStartRequest with a cutover timestamp in the past.
func testRequest(cutoverOffset time.Duration) consensus.SoakStartRequest {
	return consensus.SoakStartRequest{
		NodeID:            "node0",
		CutoverTimestamp:  time.Now().Add(cutoverOffset),
		MigrationPlanPath: "/tmp/migration-plan.yaml",
	}
}

// mockDecommissioner records calls to Decommission.
type mockDecommissioner struct {
	mu    sync.Mutex
	calls []string
}

func (m *mockDecommissioner) Decommission(_ context.Context, nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, nodeID)
	return nil
}

func (m *mockDecommissioner) Called() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

// alwaysTrueCriterion is a SoakCriterion that always returns true.
type alwaysTrueCriterion struct{ name string }

func (c alwaysTrueCriterion) Name() string { return c.name }
func (c alwaysTrueCriterion) Check(_ context.Context, _ consensus.SoakStartRequest) (bool, error) {
	return true, nil
}

// newMonitor creates a MigrationMonitor wired with the given logger and
// decommissioner, using a fast poll interval for tests.
func newMonitor(
	t *testing.T,
	logger *eventlog.EventLogger,
	d consensus.Decommissioner,
	stateDir string,
	criteria ...consensus.SoakCriterion,
) *consensus.MigrationMonitor {
	t.Helper()
	mm := consensus.NewMigrationMonitorWith(
		"node0",
		logger,
		d,
		consensus.MigrationMonitorConfig{PollInterval: 10 * time.Millisecond},
		stateDir,
	)
	if len(criteria) > 0 {
		mm.WithCriteria(criteria...)
	}
	return mm
}

// --- Tests ---

// Test_MigrationMonitor_EmitsSoakStarted verifies that run() emits a SoakStarted event.
func Test_MigrationMonitor_EmitsSoakStarted(t *testing.T) {
	logger, logPath := newTestLogger(t)
	defer logger.Close()
	stateDir := t.TempDir()

	mm := newMonitor(t, logger, &consensus.NoopDecommissioner{}, stateDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = mm.Run(ctx) }()

	req := testRequest(-50 * time.Hour)
	require.True(t, mm.TryEnqueue(req))

	require.Eventually(t, func() bool {
		events := readEvents(t, logPath)
		return hasReason(events, consensus.ReasonSoakStarted)
	}, 500*time.Millisecond, 10*time.Millisecond, "SoakStarted event not emitted")
}

// Test_MigrationMonitor_EmitsSoakCheck verifies that after one poll tick, a SoakCheck event is written.
func Test_MigrationMonitor_EmitsSoakCheck(t *testing.T) {
	logger, logPath := newTestLogger(t)
	defer logger.Close()
	stateDir := t.TempDir()

	mm := newMonitor(t, logger, &consensus.NoopDecommissioner{}, stateDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = mm.Run(ctx) }()

	req := testRequest(-1 * time.Hour)
	require.True(t, mm.TryEnqueue(req))

	require.Eventually(t, func() bool {
		events := readEvents(t, logPath)
		return hasReason(events, consensus.ReasonSoakCheck)
	}, 500*time.Millisecond, 10*time.Millisecond, "SoakCheck event not emitted")
}

// Test_MigrationMonitor_DecommissionsWhenAllCriteriaGreen verifies that when all criteria
// are green and the fleet threshold flag file exists, decommission is triggered.
func Test_MigrationMonitor_DecommissionsWhenAllCriteriaGreen(t *testing.T) {
	logger, logPath := newTestLogger(t)
	defer logger.Close()
	stateDir := t.TempDir()

	// Create fleet threshold flag file.
	fleetFlagPath := filepath.Join(stateDir, "fleet-threshold-reached")
	require.NoError(t, os.WriteFile(fleetFlagPath, []byte{}, 0o640))

	decommissioner := &mockDecommissioner{}

	mm := consensus.NewMigrationMonitorWith(
		"node0",
		logger,
		decommissioner,
		consensus.MigrationMonitorConfig{
			PollInterval:       10 * time.Millisecond,
			FleetThresholdPath: fleetFlagPath,
		},
		stateDir,
	).WithCriteria(
		alwaysTrueCriterion{"SoakDuration"},
		alwaysTrueCriterion{"UploaderBacklogCleared"},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = mm.Run(ctx) }()

	req := testRequest(-50 * time.Hour)
	require.True(t, mm.TryEnqueue(req))

	require.Eventually(t, func() bool {
		events := readEvents(t, logPath)
		return hasReason(events, consensus.ReasonDecommissionTriggered) && hasReason(events, consensus.ReasonDecommissionCompleted)
	}, 500*time.Millisecond, 10*time.Millisecond, "Decommission events not emitted")

	assert.NotEmpty(t, decommissioner.Called(), "Decommission should have been called")
}

// Test_MigrationMonitor_DoesNotDecommissionUntilFleetThreshold verifies that
// when all criteria are green but the fleet flag file does not exist, decommission
// is NOT triggered.
func Test_MigrationMonitor_DoesNotDecommissionUntilFleetThreshold(t *testing.T) {
	logger, logPath := newTestLogger(t)
	defer logger.Close()
	stateDir := t.TempDir()

	// Fleet flag file does NOT exist.
	fleetFlagPath := filepath.Join(stateDir, "fleet-threshold-reached")

	decommissioner := &mockDecommissioner{}

	mm := consensus.NewMigrationMonitorWith(
		"node0",
		logger,
		decommissioner,
		consensus.MigrationMonitorConfig{
			PollInterval:       10 * time.Millisecond,
			FleetThresholdPath: fleetFlagPath,
		},
		stateDir,
	).WithCriteria(
		alwaysTrueCriterion{"SoakDuration"},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = mm.Run(ctx) }()

	req := testRequest(-50 * time.Hour)
	require.True(t, mm.TryEnqueue(req))

	// Wait for a few SoakCheck events to confirm polling is running.
	require.Eventually(t, func() bool {
		events := readEvents(t, logPath)
		return countReason(events, consensus.ReasonSoakCheck) >= 3
	}, 500*time.Millisecond, 10*time.Millisecond, "SoakCheck events not emitted")

	// Decommission must NOT have been called.
	assert.Empty(t, decommissioner.Called(), "Decommission should not have been called without fleet threshold")
}

// Test_MigrationMonitor_EmitsCriterionMet verifies that CriterionMet is emitted
// exactly once when a criterion transitions false→true.
func Test_MigrationMonitor_EmitsCriterionMet(t *testing.T) {
	logger, logPath := newTestLogger(t)
	defer logger.Close()
	stateDir := t.TempDir()

	// Fleet flag file does NOT exist so we don't trigger decommission.
	fleetFlagPath := filepath.Join(stateDir, "fleet-threshold-reached")

	// Use SoakDuration with a cutover well in the past so it goes green immediately.
	mm := consensus.NewMigrationMonitorWith(
		"node0",
		logger,
		&consensus.NoopDecommissioner{},
		consensus.MigrationMonitorConfig{
			PollInterval:       10 * time.Millisecond,
			FleetThresholdPath: fleetFlagPath,
		},
		stateDir,
	).WithCriteria(consensus.SoakDuration{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = mm.Run(ctx) }()

	req := testRequest(-50 * time.Hour)
	require.True(t, mm.TryEnqueue(req))

	// Wait for at least 3 SoakCheck ticks so we can confirm CriterionMet was emitted only once.
	require.Eventually(t, func() bool {
		events := readEvents(t, logPath)
		return countReason(events, consensus.ReasonSoakCheck) >= 3
	}, 500*time.Millisecond, 10*time.Millisecond, "SoakCheck not emitted enough times")

	events := readEvents(t, logPath)
	assert.Equal(t, 1, countReason(events, consensus.ReasonCriterionMet), "CriterionMet should be emitted exactly once")
}

// Test_MigrationMonitor_ResumesOnRestart verifies that a valid cutover-state.jsonl
// causes resumeIfNeeded to spawn a watcher goroutine (soakActive becomes true).
func Test_MigrationMonitor_ResumesOnRestart(t *testing.T) {
	logger, _ := newTestLogger(t)
	defer logger.Close()
	stateDir := t.TempDir()

	req := testRequest(-10 * time.Hour)
	b, err := json.Marshal(req)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(stateDir, "cutover-state.jsonl"), b, 0o640))

	mm := newMonitor(t, logger, &consensus.NoopDecommissioner{}, stateDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = mm.Run(ctx) }()

	require.Eventually(t, func() bool {
		return mm.Status().Active
	}, 500*time.Millisecond, 10*time.Millisecond, "soak watcher should be active after resume")
}

// Test_MigrationMonitor_ResumeIgnoresInvalidState verifies that a malformed
// cutover-state.jsonl causes resumeIfNeeded to delete the file and not spawn a goroutine.
func Test_MigrationMonitor_ResumeIgnoresInvalidState(t *testing.T) {
	logger, _ := newTestLogger(t)
	defer logger.Close()
	stateDir := t.TempDir()

	stateFile := filepath.Join(stateDir, "cutover-state.jsonl")
	require.NoError(t, os.WriteFile(stateFile, []byte("{not valid json"), 0o640))

	mm := newMonitor(t, logger, &consensus.NoopDecommissioner{}, stateDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = mm.Run(ctx) }()

	// Give the resume a moment to run.
	time.Sleep(50 * time.Millisecond)

	assert.False(t, mm.Status().Active, "soak watcher should not be active after invalid state")

	_, err := os.Stat(stateFile)
	assert.True(t, os.IsNotExist(err), "invalid state file should have been deleted")
}

// Test_MigrationMonitor_WriteSoakState_Atomic verifies that writeSoakState
// atomically writes the state file and can be round-tripped back via JSON.
func Test_MigrationMonitor_WriteSoakState_Atomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cutover-state.jsonl")

	req := testRequest(-5 * time.Hour)

	// Call the production helper via the exported test shim.
	require.NoError(t, consensus.WriteSoakState(path, req))

	// Verify no .tmp file was left behind (atomic rename succeeded).
	_, err := os.Stat(path + ".tmp")
	require.True(t, os.IsNotExist(err), "temp file must not exist after successful write")

	// Verify round-trip.
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var got consensus.SoakStartRequest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, req.NodeID, got.NodeID)
	assert.Equal(t, req.MigrationPlanPath, got.MigrationPlanPath)
	assert.WithinDuration(t, req.CutoverTimestamp, got.CutoverTimestamp, time.Second)
}

// Test_SoakDuration_TrueWhenElapsed verifies that SoakDuration returns
// true when the configured period has elapsed since the cutover timestamp.
func Test_SoakDuration_TrueWhenElapsed(t *testing.T) {
	c := consensus.SoakDuration{}
	req := testRequest(-50 * time.Hour)
	ok, err := c.Check(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, ok)
}

// Test_SoakDuration_FalseWhenNotElapsed verifies that SoakDuration returns
// false when the configured period has not elapsed.
func Test_SoakDuration_FalseWhenNotElapsed(t *testing.T) {
	c := consensus.SoakDuration{}
	req := testRequest(-1 * time.Hour)
	ok, err := c.Check(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, ok)
}
