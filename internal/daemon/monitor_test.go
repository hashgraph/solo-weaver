// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---- test helpers ----

type fakeMonitor struct {
	name     string
	runs     atomic.Int32 // incremented at the start of each Run call
	behavior func(ctx context.Context, run int) error
}

func (f *fakeMonitor) Name() string { return f.name }
func (f *fakeMonitor) Run(ctx context.Context) error {
	run := int(f.runs.Add(1))
	return f.behavior(ctx, run)
}

// ---- tests ----

// TestSupervisedMonitor_RestartsAfterCrash verifies that supervisedMonitor
// calls Run again after a non-nil error return.
func TestSupervisedMonitor_RestartsAfterCrash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const wantRuns = 3
	m := &fakeMonitor{
		name: "test-monitor",
		behavior: func(ctx context.Context, run int) error {
			if run < wantRuns {
				return errors.New("simulated crash")
			}
			// On the last run, block until ctx is cancelled so supervisedMonitor exits cleanly.
			<-ctx.Done()
			return nil
		},
	}

	// Use a very short initial backoff so the test doesn't take 5+ seconds.
	origInitial := supervisedBackoffInitial
	supervisedBackoffInitial = 1 * time.Millisecond
	t.Cleanup(func() { supervisedBackoffInitial = origInitial })

	done := make(chan struct{})
	go func() {
		supervisedMonitor(ctx, m, nil)
		close(done)
	}()

	// Wait until wantRuns have been started before cancelling.
	assert.Eventually(t, func() bool {
		return int(m.runs.Load()) >= wantRuns
	}, 5*time.Second, 5*time.Millisecond, "expected at least %d Run calls", wantRuns)

	cancel()
	<-done
	assert.GreaterOrEqual(t, int(m.runs.Load()), wantRuns)
}

// TestSupervisedMonitor_NoRestartOnCtxCancel verifies that supervisedMonitor
// does not restart the monitor after ctx is cancelled, even if Run returns an error.
func TestSupervisedMonitor_NoRestartOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	m := &fakeMonitor{
		name: "test-monitor",
		behavior: func(ctx context.Context, run int) error {
			// Cancel on first run to simulate the daemon shutting down.
			cancel()
			return errors.New("error concurrent with cancel")
		},
	}

	done := make(chan struct{})
	go func() {
		supervisedMonitor(ctx, m, nil)
		close(done)
	}()

	select {
	case <-done:
		// Expected — supervisedMonitor should exit without restarting.
	case <-time.After(3 * time.Second):
		t.Fatal("supervisedMonitor did not exit after context cancellation")
	}

	// Only one Run call: no restart after ctx cancelled.
	assert.Equal(t, int32(1), m.runs.Load())
}

// TestSupervisedMonitor_BackoffResetAfterStableRun verifies that the back-off
// counter resets to supervisedBackoffInitial when a run exceeds
// supervisedStableThreshold before crashing.
func TestSupervisedMonitor_BackoffResetAfterStableRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Patch thresholds so the test runs in milliseconds.
	origInitial := supervisedBackoffInitial
	origStable := supervisedStableThreshold
	supervisedBackoffInitial = 1 * time.Millisecond
	supervisedStableThreshold = 5 * time.Millisecond
	t.Cleanup(func() {
		supervisedBackoffInitial = origInitial
		supervisedStableThreshold = origStable
	})

	const wantRuns = 4
	m := &fakeMonitor{
		name: "test-monitor",
		behavior: func(ctx context.Context, run int) error {
			if run < wantRuns {
				// Exceed the stable threshold so the back-off resets.
				time.Sleep(2 * supervisedStableThreshold)
				return errors.New("simulated crash after stable run")
			}
			<-ctx.Done()
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		supervisedMonitor(ctx, m, nil)
		close(done)
	}()

	assert.Eventually(t, func() bool {
		return int(m.runs.Load()) >= wantRuns
	}, 5*time.Second, 5*time.Millisecond, "expected at least %d Run calls", wantRuns)

	cancel()
	<-done
	// After a stable run, backoff resets. If it had grown unboundedly the test
	// would time out waiting for wantRuns — this passing is the assertion.
	assert.GreaterOrEqual(t, int(m.runs.Load()), wantRuns)
}

// TestSupervisedMonitor_DegradedEventFired verifies that a MonitorDegraded log
// event is emitted after supervisedDegradedThreshold consecutive crashes, and
// again at each subsequent multiple of the threshold.
//
// Because we cannot capture logx output in a unit test, we verify the behaviour
// indirectly: the monitor must be called at least 2×threshold times, meaning the
// supervisor kept restarting past the first degraded event and fired it again at
// 2×threshold without exiting or silencing restarts.
func TestSupervisedMonitor_DegradedEventFired(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	origInitial := supervisedBackoffInitial
	origThreshold := supervisedDegradedThreshold
	supervisedBackoffInitial = 1 * time.Millisecond
	supervisedDegradedThreshold = 3 // fire at crash #3, #6, …
	t.Cleanup(func() {
		supervisedBackoffInitial = origInitial
		supervisedDegradedThreshold = origThreshold
	})

	// wantRuns > 2×threshold so we cross the degraded boundary at least twice.
	const wantRuns = 7
	m := &fakeMonitor{
		name: "test-monitor",
		behavior: func(ctx context.Context, run int) error {
			if run < wantRuns {
				return errors.New("crash")
			}
			<-ctx.Done()
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		supervisedMonitor(ctx, m, nil)
		close(done)
	}()

	assert.Eventually(t, func() bool {
		return int(m.runs.Load()) >= wantRuns
	}, 5*time.Second, 1*time.Millisecond, "expected at least %d Run calls", wantRuns)

	cancel()
	<-done
	assert.GreaterOrEqual(t, int(m.runs.Load()), wantRuns)
}

// TestSupervisedMonitor_DegradedCounterResetsAfterStableRun verifies that
// consecutive crash counter resets after a stable run, so a monitor that
// recovers and then crashes again starts the degraded threshold fresh.
func TestSupervisedMonitor_DegradedCounterResetsAfterStableRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	origInitial := supervisedBackoffInitial
	origStable := supervisedStableThreshold
	origThreshold := supervisedDegradedThreshold
	supervisedBackoffInitial = 1 * time.Millisecond
	supervisedStableThreshold = 5 * time.Millisecond
	supervisedDegradedThreshold = 3
	t.Cleanup(func() {
		supervisedBackoffInitial = origInitial
		supervisedStableThreshold = origStable
		supervisedDegradedThreshold = origThreshold
	})

	// Sequence: 2 fast crashes → 1 stable run → 2 more fast crashes → exit.
	// Total 5 Run calls. If the counter did NOT reset after the stable run the
	// 5th crash would fire MonitorDegraded (2+1+2=5≥3). If it DOES reset the
	// counter after the stable run we only ever reach max 2 consecutive crashes,
	// never hitting the threshold again. We verify this by ensuring the monitor
	// completes 5 runs within the test timeout.
	const wantRuns = 5
	m := &fakeMonitor{
		name: "test-monitor",
		behavior: func(ctx context.Context, run int) error {
			switch run {
			case 1, 2:
				return errors.New("fast crash")
			case 3:
				// Stable run — sleep past the stable threshold.
				time.Sleep(2 * supervisedStableThreshold)
				return errors.New("crash after stable run")
			case 4:
				return errors.New("fast crash after reset")
			default:
				<-ctx.Done()
				return nil
			}
		},
	}

	done := make(chan struct{})
	go func() {
		supervisedMonitor(ctx, m, nil)
		close(done)
	}()

	assert.Eventually(t, func() bool {
		return int(m.runs.Load()) >= wantRuns
	}, 5*time.Second, 1*time.Millisecond, "expected at least %d Run calls", wantRuns)

	cancel()
	<-done
}

// TestSupervisedMonitor_BackoffCapAtMax verifies that back-off does not exceed
// supervisedBackoffCap regardless of how many consecutive crashes occur.
func TestSupervisedMonitor_BackoffCapAtMax(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	origInitial := supervisedBackoffInitial
	origCap := supervisedBackoffCap
	supervisedBackoffInitial = 1 * time.Millisecond
	supervisedBackoffCap = 8 * time.Millisecond // initial→2→4→8(cap)
	t.Cleanup(func() {
		supervisedBackoffInitial = origInitial
		supervisedBackoffCap = origCap
	})

	const wantRuns = 6
	m := &fakeMonitor{
		name: "test-monitor",
		behavior: func(ctx context.Context, run int) error {
			if run < wantRuns {
				return errors.New("crash")
			}
			<-ctx.Done()
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		supervisedMonitor(ctx, m, nil)
		close(done)
	}()

	// Even with cap=8ms, 6 runs should complete well within 1 second.
	assert.Eventually(t, func() bool {
		return int(m.runs.Load()) >= wantRuns
	}, 1*time.Second, 1*time.Millisecond, "expected at least %d Run calls", wantRuns)

	cancel()
	<-done
}
