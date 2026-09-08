// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package network

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/automa-saga/logx"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/daemon/privexec"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/network/reassert"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
)

// fakeDelegator scripts successive NetworkReassert results, holding the last one
// once exhausted, and counts calls so the gate is observable.
type fakeDelegator struct {
	mu      sync.Mutex
	results []privexec.NetworkReassertResult
	idx     int

	calls atomic.Int32

	err error
	// blockUntilCancel models an exec killed mid-flight by shutdown.
	blockUntilCancel bool
}

func (f *fakeDelegator) NetworkReassert(ctx context.Context) (privexec.NetworkReassertResult, error) {
	f.calls.Add(1)
	if f.blockUntilCancel {
		<-ctx.Done()
		return privexec.NetworkReassertResult{}, ctx.Err()
	}
	if f.err != nil {
		return privexec.NetworkReassertResult{}, f.err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.idx
	if i >= len(f.results) {
		i = len(f.results) - 1
	}
	f.idx++
	return f.results[i], nil
}

func (f *fakeDelegator) Run(context.Context, ...string) ([]byte, error)           { return nil, nil }
func (f *fakeDelegator) NetworkPolicySet(context.Context, string, []string) error { return nil }
func (f *fakeDelegator) TCAttach(context.Context, string) error                   { return nil }
func (f *fakeDelegator) TCDetach(context.Context, string) error                   { return nil }
func (f *fakeDelegator) ReconcileShaper(context.Context, string) error            { return nil }
func (f *fakeDelegator) ReconcileShaperCheck(context.Context, string) (string, error) {
	return "", nil
}

// newTestMonitor builds a monitor with the delegator and the on-disk gate faked.
// provisioned controls what the unprivileged pre-gate sees.
func newTestMonitor(d *fakeDelegator, provisioned func() bool) *ReassertMonitor {
	return &ReassertMonitor{
		delegator:  d,
		gatePaths:  []string{"/fake/host.nft"},
		fileExists: func(string) bool { return provisioned() },
	}
}

// shrinkInterval makes the run loop tick fast enough for a test.
func shrinkInterval(t *testing.T, d time.Duration) {
	t.Helper()
	restore := reassertInterval
	reassertInterval = d
	t.Cleanup(func() { reassertInterval = restore })
}

// shrinkStartupGrace makes the run loop's first check happen fast enough for a test.
func shrinkStartupGrace(t *testing.T, d time.Duration) {
	t.Helper()
	restore := reassertStartupGrace
	reassertStartupGrace = d
	t.Cleanup(func() { reassertStartupGrace = restore })
}

// shrinkTickTimeout makes a hung worker exec time out fast enough for a test.
func shrinkTickTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	restore := reassertTickTimeout
	reassertTickTimeout = d
	t.Cleanup(func() { reassertTickTimeout = restore })
}

// waitForCalls polls until the delegator has been called want times.
func waitForCalls(t *testing.T, d *fakeDelegator, want int32) {
	t.Helper()
	require.Eventually(t, func() bool { return d.calls.Load() >= want },
		2*time.Second, time.Millisecond,
		"expected at least %d worker calls, got %d", want, d.calls.Load())
}

// report is a convenience builder for one worker result.
func report(artifacts ...privexec.NetworkArtifactStatus) privexec.NetworkReassertResult {
	return privexec.NetworkReassertResult{Artifacts: artifacts}
}

// TestGatePathsMatchTheRealArtifactConstants pins the mirrored-by-value paths
// to the packages that own them, which production code cannot import.
func TestGatePathsMatchTheRealArtifactConstants(t *testing.T) {
	assert.Equal(t, firewall.HostNftPath, hostFirewallNftPath)
	assert.Equal(t, policy.WeaverNftPath, workloadPolicyNftPath)
	assert.Equal(t, shape.TcEgressScriptPath, tcEgressScriptPath)

	// And the production monitor actually gates on all three.
	assert.ElementsMatch(t,
		[]string{firewall.HostNftPath, policy.WeaverNftPath, shape.TcEgressScriptPath},
		NewReassertMonitor().gatePaths)
}

func TestTick_UnprovisionedHostNeverEscalates(t *testing.T) {
	// A consensus-only node must not pay a sudo exec a minute for a check that
	// has nothing to look at.
	d := &fakeDelegator{}
	m := newTestMonitor(d, func() bool { return false })

	require.NoError(t, m.tick(context.Background()))

	assert.Zero(t, d.calls.Load(), "no artifact on disk must mean no worker exec")
	assert.Nil(t, m.Snapshot().LastCheckAt)
}

func TestTick_GateIsReEvaluatedEveryTick(t *testing.T) {
	// On a fresh install the daemon starts before the network artifacts exist, so
	// a gate evaluated once at construction would stay closed until a restart.
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{Artifact: "host-firewall", Expected: true, Present: true}),
	}}
	var installed atomic.Bool
	m := newTestMonitor(d, installed.Load)

	require.NoError(t, m.tick(context.Background()))
	require.Zero(t, d.calls.Load())

	installed.Store(true)
	require.NoError(t, m.tick(context.Background()))
	assert.Equal(t, int32(1), d.calls.Load(), "the check must start once the host is provisioned")
}

func TestRun_ChecksAfterTheGraceThenOnEveryTick(t *testing.T) {
	shrinkStartupGrace(t, time.Millisecond)
	shrinkInterval(t, time.Millisecond)
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{Artifact: "host-firewall", Expected: true, Present: true}),
	}}
	m := newTestMonitor(d, func() bool { return true })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()

	waitForCalls(t, d, 3)
	cancel()
	require.NoError(t, <-done)
}

func TestRun_DoesNotProbeBeforeTheStartupGrace(t *testing.T) {
	// At boot the shaper is not up yet; an immediate probe would misreport it.
	shrinkStartupGrace(t, time.Hour)
	shrinkInterval(t, time.Millisecond)
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{Artifact: "egress-qdisc", Expected: true, Present: true}),
	}}
	m := newTestMonitor(d, func() bool { return true })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()

	time.Sleep(20 * time.Millisecond)
	assert.Zero(t, d.calls.Load(), "no worker exec may happen inside the grace")

	cancel()
	require.NoError(t, <-done, "a shutdown inside the grace is a clean stop")
}

func TestRecord_CountsReassertionsAcrossTicks(t *testing.T) {
	// The counter is what tells an operator "this keeps happening" — the repair
	// works every time, but something on the host keeps destroying the table.
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{
			Artifact: "host-firewall", Expected: true, Present: true,
			Reasserted: true, Recovered: true,
		}),
	}}
	m := newTestMonitor(d, func() bool { return true })

	for range 3 {
		require.NoError(t, m.tick(context.Background()))
	}

	got := m.Snapshot()
	require.Len(t, got.Artifacts, 1)
	assert.Equal(t, 3, got.Artifacts[0].ReassertCount)
	assert.NotNil(t, got.Artifacts[0].LastReassertedAt)
	assert.Zero(t, got.Artifacts[0].ConsecutiveFailures, "a repair that works is not a failure")
	assert.NotNil(t, got.LastCheckAt)
}

func TestRecord_StatusReportsARestoredArtifactAsPresent(t *testing.T) {
	// ArtifactState has no recovered field, so present is the only thing telling
	// an operator whether the host is currently protected.
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{
			Artifact: "host-firewall", Expected: true, Present: true,
			Reasserted: true, Recovered: true, Detail: "restored",
		}),
	}}
	m := newTestMonitor(d, func() bool { return true })

	require.NoError(t, m.tick(context.Background()))

	got := m.Snapshot().Artifacts[0]
	assert.True(t, got.Present, "a restored artifact must not be reported absent")
	assert.Equal(t, 1, got.ReassertCount)
	assert.Zero(t, got.ConsecutiveFailures)
}

func TestRecord_ConsecutiveFailuresRiseThenResetOnRecovery(t *testing.T) {
	missing := privexec.NetworkArtifactStatus{
		Artifact: "egress-qdisc", Expected: true, Present: false,
		Reasserted: true, Recovered: false,
	}
	recovered := privexec.NetworkArtifactStatus{
		Artifact: "egress-qdisc", Expected: true, Present: true,
		Reasserted: true, Recovered: true,
	}
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(missing), report(missing), report(recovered),
	}}
	m := newTestMonitor(d, func() bool { return true })

	require.NoError(t, m.tick(context.Background()))
	assert.Equal(t, 1, m.Snapshot().Artifacts[0].ConsecutiveFailures)

	require.NoError(t, m.tick(context.Background()))
	assert.Equal(t, 2, m.Snapshot().Artifacts[0].ConsecutiveFailures)

	require.NoError(t, m.tick(context.Background()))
	got := m.Snapshot().Artifacts[0]
	assert.Zero(t, got.ConsecutiveFailures)
	assert.Equal(t, 1, got.ReassertCount)
}

func TestRecord_ProbeFailureCountsAsAFailureButNotAReassertion(t *testing.T) {
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{
			Artifact: "egress-qdisc", Expected: true, ProbeFailed: true,
			Detail: "cannot determine: Cannot find device \"eth0\"",
		}),
	}}
	m := newTestMonitor(d, func() bool { return true })

	require.NoError(t, m.tick(context.Background()))
	require.NoError(t, m.tick(context.Background()))

	got := m.Snapshot().Artifacts[0]
	assert.True(t, got.ProbeFailed)
	assert.Equal(t, 2, got.ConsecutiveFailures)
	assert.Zero(t, got.ReassertCount, "an unprobeable artifact was never repaired")
}

func TestTick_WorkerFailureIsReturnedSoTheSupervisorBacksOff(t *testing.T) {
	// A broken sudo grant must not retry once a minute forever; returning the
	// error hands it to daemonkit's exponential back-off.
	d := &fakeDelegator{err: errors.New("sudo: a password is required")}
	m := newTestMonitor(d, func() bool { return true })

	err := m.tick(context.Background())

	require.Error(t, err)
	assert.Contains(t, m.Snapshot().LastError, "password is required")
}

func TestTick_WorkerFailureLeavesTheLastKnownArtifactHistoryIntact(t *testing.T) {
	// The previous run's findings remain the most recent thing actually known
	// about the kernel; an exec failure says nothing about it.
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{Artifact: "host-firewall", Expected: true, Present: true}),
	}}
	m := newTestMonitor(d, func() bool { return true })
	require.NoError(t, m.tick(context.Background()))

	d.err = errors.New("boom")
	require.Error(t, m.tick(context.Background()))

	got := m.Snapshot()
	require.Len(t, got.Artifacts, 1)
	assert.True(t, got.Artifacts[0].Present)
	assert.NotEmpty(t, got.LastError)
}

func TestRecord_ClearsAStaleLastError(t *testing.T) {
	d := &fakeDelegator{err: errors.New("boom")}
	m := newTestMonitor(d, func() bool { return true })
	require.Error(t, m.tick(context.Background()))
	require.NotEmpty(t, m.Snapshot().LastError)

	d.err = nil
	d.results = []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{Artifact: "host-firewall", Expected: true, Present: true}),
	}
	require.NoError(t, m.tick(context.Background()))

	assert.Empty(t, m.Snapshot().LastError)
}

func TestTick_CancellationDuringExecIsNotAFault(t *testing.T) {
	d := &fakeDelegator{blockUntilCancel: true}
	m := newTestMonitor(d, func() bool { return true })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.tick(ctx) }()

	cancel()
	require.NoError(t, <-done, "a shutdown-killed exec is a clean stop, not a fault")
}

// TestTick_HungWorkerIsCutOffAndReportedAsAFault: a wedged sudo must not stall
// the monitor forever, and counts as a fault, not a shutdown.
func TestTick_HungWorkerIsCutOffAndReportedAsAFault(t *testing.T) {
	shrinkTickTimeout(t, 20*time.Millisecond)
	d := &fakeDelegator{blockUntilCancel: true}
	m := newTestMonitor(d, func() bool { return true })

	err := m.tick(context.Background())

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, m.Snapshot().LastError, context.DeadlineExceeded.Error())
}

// TestTickTimeoutOutlivesTheWorkersOwnDeadline: the lock-holding CLI must give
// up before the daemon kills its sudo parent, or the root-owned child keeps the locks.
func TestTickTimeoutOutlivesTheWorkersOwnDeadline(t *testing.T) {
	assert.Greater(t, reassertTickTimeout, reassert.DefaultTimeout)
}

func TestSnapshot_ReturnsACopy(t *testing.T) {
	// Snapshot is called on the HTTP handler's goroutine while the run loop keeps
	// writing; handing out the live slice would race the serialiser.
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{Artifact: "host-firewall", Expected: true, Present: true}),
	}}
	m := newTestMonitor(d, func() bool { return true })
	require.NoError(t, m.tick(context.Background()))

	got := m.Snapshot()
	got.Artifacts[0].Artifact = "mutated"

	assert.Equal(t, "host-firewall", m.Snapshot().Artifacts[0].Artifact)
}

// TestFailureLevel_ErrorOnceThenDebug pins the throttle: a stuck failure must
// not log an ERROR per tick — only the first one, since GET /status has the count.
func TestFailureLevel_ErrorOnceThenDebug(t *testing.T) {
	assert.Equal(t, zerolog.ErrorLevel, failureLevel(1))
	assert.Equal(t, zerolog.DebugLevel, failureLevel(2))
	assert.Equal(t, zerolog.DebugLevel, failureLevel(60))
}

func TestName(t *testing.T) {
	assert.Equal(t, MonitorName, NewReassertMonitor().Name())
}

func TestRecord_SkippedArtifactPreservesTheLastKnownState(t *testing.T) {
	// A run that could not look must not overwrite what the last one saw, nor move
	// either counter: a held lock is contention, not damage.
	restored := privexec.NetworkArtifactStatus{
		Artifact: "host-firewall", Expected: true, Present: true,
		Reasserted: true, Recovered: true, Detail: "restored",
	}
	skipped := privexec.NetworkArtifactStatus{
		Artifact: "host-firewall", Skipped: true,
		Detail: "skipped: an apply is in progress",
	}
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(restored), report(skipped),
	}}
	m := newTestMonitor(d, func() bool { return true })

	require.NoError(t, m.tick(context.Background()))
	require.NoError(t, m.tick(context.Background()))

	got := m.Snapshot().Artifacts[0]
	assert.True(t, got.Expected, "the skip must not zero out what the last run observed")
	assert.Equal(t, "restored", got.Detail)
	assert.Equal(t, 1, got.ReassertCount, "a skip is not a repair")
	assert.Zero(t, got.ConsecutiveFailures, "a skip is not a failure")
}

func TestRecord_AllSkippedDoesNotAdvanceLastCheckAt(t *testing.T) {
	// A run that looked at nothing must not refresh the freshness signal, or a
	// lock held for an hour reads on /status as a kernel checked seconds ago.
	probed := privexec.NetworkArtifactStatus{
		Artifact: "host-firewall", Expected: true, Present: true,
	}
	skipped := privexec.NetworkArtifactStatus{
		Artifact: "host-firewall", Skipped: true,
		Detail: "skipped: an apply is in progress",
	}
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(probed), report(skipped),
	}}
	m := newTestMonitor(d, func() bool { return true })

	require.NoError(t, m.tick(context.Background()))
	first := m.Snapshot().LastCheckAt
	require.NotNil(t, first)

	require.NoError(t, m.tick(context.Background()))
	assert.Equal(t, first, m.Snapshot().LastCheckAt, "a fully skipped run verified nothing")
}

func TestRecord_PartiallySkippedStillAdvancesLastCheckAt(t *testing.T) {
	// One plane locked, the other probed: the run did verify something, so the
	// timestamp is honest.
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(
			privexec.NetworkArtifactStatus{Artifact: "host-firewall", Skipped: true},
			privexec.NetworkArtifactStatus{Artifact: "egress-qdisc", Expected: true, Present: true},
		),
	}}
	m := newTestMonitor(d, func() bool { return true })

	require.NoError(t, m.tick(context.Background()))

	assert.NotNil(t, m.Snapshot().LastCheckAt)
}

func TestRecord_SkippedOnTheVeryFirstRunReportsWhy(t *testing.T) {
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{
			Artifact: "egress-qdisc", Skipped: true,
			Detail: "skipped: an apply is in progress",
		}),
	}}
	m := newTestMonitor(d, func() bool { return true })

	require.NoError(t, m.tick(context.Background()))

	got := m.Snapshot().Artifacts[0]
	assert.Equal(t, "egress-qdisc", got.Artifact)
	assert.Contains(t, got.Detail, "an apply is in progress")
	assert.Zero(t, got.ReassertCount)
	assert.Zero(t, got.ConsecutiveFailures)
}

func TestRecord_ReplacesTheArtifactListRatherThanMerging(t *testing.T) {
	// Each report is the whole truth about that run. An artifact the worker stops
	// reporting must not linger on /status with stale state, while the ones it
	// keeps reporting keep their history.
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(
			privexec.NetworkArtifactStatus{
				Artifact: "host-firewall", Expected: true, Present: true,
				Reasserted: true, Recovered: true,
			},
			privexec.NetworkArtifactStatus{Artifact: "egress-qdisc", Expected: true, Present: true},
		),
		report(privexec.NetworkArtifactStatus{Artifact: "host-firewall", Expected: true, Present: true}),
	}}
	m := newTestMonitor(d, func() bool { return true })

	require.NoError(t, m.tick(context.Background()))
	require.Len(t, m.Snapshot().Artifacts, 2)

	require.NoError(t, m.tick(context.Background()))

	got := m.Snapshot().Artifacts
	require.Len(t, got, 1, "an artifact absent from the report must be dropped, not carried over")
	assert.Equal(t, "host-firewall", got[0].Artifact)
	assert.Equal(t, 1, got[0].ReassertCount, "a reported artifact keeps its history across the replace")
	assert.NotNil(t, got[0].LastReassertedAt)
}

// --- operator-facing logging -----------------------------------------------

// captureLogs points logx at a buffer at DEBUG level, so the throttled repeat
// lines are actually emitted rather than silently dropped by the level filter.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	origLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(origLevel) })

	var buf bytes.Buffer
	origLogger := *logx.As()
	logx.SetLogger(zerolog.New(&buf))
	t.Cleanup(func() { logx.SetLogger(origLogger) })

	return &buf
}

// logLines decodes every event in the buffer and empties it, so each tick can be
// inspected on its own.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m), "not JSON: %q", l)
		out = append(out, m)
	}
	buf.Reset()
	return out
}

// soleLine decodes the one event a tick was expected to emit.
func soleLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := logLines(t, buf)
	require.Len(t, lines, 1, "expected exactly one journal line")
	return lines[0]
}

func TestLogReport_ARestoreWarnsWithItsRunningCount(t *testing.T) {
	// WARN, not INFO: something outside weaver destroyed live state, and the
	// count is what turns a one-off into a pattern an operator must chase.
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{
			Artifact: "host-firewall", Expected: true, Present: true,
			Reasserted: true, Recovered: true, Detail: "restored",
		}),
	}}
	m := newTestMonitor(d, func() bool { return true })
	buf := captureLogs(t)

	require.NoError(t, m.tick(context.Background()))
	got := soleLine(t, buf)
	assert.Equal(t, "warn", got["level"])
	assert.Equal(t, "NetworkArtifactReasserted", got["reason"])
	assert.Equal(t, float64(1), got["reassert_count"])

	require.NoError(t, m.tick(context.Background()))
	assert.Equal(t, float64(2), soleLine(t, buf)["reassert_count"],
		"the count must keep climbing so a repeat offender is visible")
}

// TestLogReport_APostRepairProbeFailureLogsUnknownNotUnrecovered is the branch
// ordering guard. Both flags are set on that artifact, and calling it
// "unrecovered" would send an operator after a repair that may well have worked.
func TestLogReport_APostRepairProbeFailureLogsUnknownNotUnrecovered(t *testing.T) {
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{
			Artifact: "egress-qdisc", Expected: true,
			Reasserted: true, Recovered: false, ProbeFailed: true,
			Detail: "restarted the shaper but the re-probe failed",
		}),
	}}
	m := newTestMonitor(d, func() bool { return true })
	buf := captureLogs(t)

	require.NoError(t, m.tick(context.Background()))

	got := soleLine(t, buf)
	assert.Equal(t, "NetworkArtifactProbeFailed", got["reason"])
	assert.NotEqual(t, "NetworkArtifactUnrecovered", got["reason"])
	assert.Equal(t, "error", got["level"], "the first failure must be loud")
}

func TestLogReport_ARepairThatDidNotWorkLogsUnrecovered(t *testing.T) {
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{
			Artifact: "egress-qdisc", Expected: true,
			Reasserted: true, Recovered: false,
			Detail: "restarted the shaper but the artifact is still missing",
		}),
	}}
	m := newTestMonitor(d, func() bool { return true })
	buf := captureLogs(t)

	require.NoError(t, m.tick(context.Background()))

	got := soleLine(t, buf)
	assert.Equal(t, "NetworkArtifactUnrecovered", got["reason"])
	assert.Equal(t, "error", got["level"])
	assert.Equal(t, float64(1), got["consecutive_failures"])
}

// TestLogReport_AStuckFailureIsLoudOnceThenQuiet pins the throttle end to end:
// once a minute forever at ERROR would bury every other line in the journal.
func TestLogReport_AStuckFailureIsLoudOnceThenQuiet(t *testing.T) {
	stuck := privexec.NetworkArtifactStatus{
		Artifact: "egress-qdisc", Expected: true, ProbeFailed: true,
		Detail: "cannot determine: Cannot find device \"eth0\"",
	}
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{report(stuck)}}
	m := newTestMonitor(d, func() bool { return true })
	buf := captureLogs(t)

	require.NoError(t, m.tick(context.Background()))
	assert.Equal(t, "error", soleLine(t, buf)["level"], "the first failure must be loud")

	for i := range 3 {
		require.NoError(t, m.tick(context.Background()))
		got := soleLine(t, buf)
		assert.Equal(t, "debug", got["level"], "repeat %d must be throttled to debug", i+1)
		assert.Equal(t, float64(i+2), got["consecutive_failures"],
			"the count must still climb so GET /status keeps the real number")
	}
}

// TestLogReport_RecoveryAfterAFailureIsAnnounced closes the loop: an operator
// who saw the ERROR needs a line telling them it is over.
func TestLogReport_RecoveryAfterAFailureIsAnnounced(t *testing.T) {
	failing := privexec.NetworkArtifactStatus{
		Artifact: "host-firewall", Expected: true, ProbeFailed: true,
		Detail: "cannot determine: permission denied",
	}
	healthy := privexec.NetworkArtifactStatus{
		Artifact: "host-firewall", Expected: true, Present: true,
	}
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(failing), report(failing), report(healthy),
	}}
	m := newTestMonitor(d, func() bool { return true })
	buf := captureLogs(t)

	require.NoError(t, m.tick(context.Background()))
	require.NoError(t, m.tick(context.Background()))
	buf.Reset()

	require.NoError(t, m.tick(context.Background()))

	got := soleLine(t, buf)
	assert.Equal(t, "info", got["level"])
	assert.Equal(t, "NetworkArtifactHealthyAgain", got["reason"])
	assert.Equal(t, float64(2), got["failed_runs"], "the line must say how long it was broken")
}

func TestLogReport_AHealthyArtifactWithNoHistoryLogsNothing(t *testing.T) {
	// The steady state is once a minute forever; a line per tick per artifact
	// would make the journal useless.
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{Artifact: "host-firewall", Expected: true, Present: true}),
	}}
	m := newTestMonitor(d, func() bool { return true })
	buf := captureLogs(t)

	require.NoError(t, m.tick(context.Background()))
	require.NoError(t, m.tick(context.Background()))

	assert.Empty(t, logLines(t, buf), "a converged host must stay silent")
}

func TestLogReport_ASkippedArtifactLogsNothing(t *testing.T) {
	// Nothing was observed, so there is nothing to report; the reason lives in
	// Detail for GET /status.
	d := &fakeDelegator{results: []privexec.NetworkReassertResult{
		report(privexec.NetworkArtifactStatus{
			Artifact: "host-firewall", Skipped: true,
			Detail: "skipped: an apply is in progress",
		}),
	}}
	m := newTestMonitor(d, func() bool { return true })
	buf := captureLogs(t)

	require.NoError(t, m.tick(context.Background()))

	assert.Empty(t, logLines(t, buf), "contention is not an event an operator must act on")
}
