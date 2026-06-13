// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package daemonkit

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
			<-ctx.Done()
			return nil
		},
	}

	origInitial := supervisedBackoffInitial
	supervisedBackoffInitial = 1 * time.Millisecond
	t.Cleanup(func() { supervisedBackoffInitial = origInitial })

	done := make(chan struct{})
	go func() {
		SupervisedMonitor(ctx, m, nil)
		close(done)
	}()

	assert.Eventually(t, func() bool {
		return int(m.runs.Load()) >= wantRuns
	}, 5*time.Second, 5*time.Millisecond, "expected at least %d Run calls", wantRuns)

	cancel()
	<-done
	assert.GreaterOrEqual(t, int(m.runs.Load()), wantRuns)
}

func TestSupervisedMonitor_NoRestartOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	m := &fakeMonitor{
		name: "test-monitor",
		behavior: func(ctx context.Context, run int) error {
			cancel()
			return errors.New("error concurrent with cancel")
		},
	}

	done := make(chan struct{})
	go func() {
		SupervisedMonitor(ctx, m, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SupervisedMonitor did not exit after context cancellation")
	}

	assert.Equal(t, int32(1), m.runs.Load())
}

func TestSupervisedMonitor_BackoffResetAfterStableRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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
				time.Sleep(2 * supervisedStableThreshold)
				return errors.New("simulated crash after stable run")
			}
			<-ctx.Done()
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		SupervisedMonitor(ctx, m, nil)
		close(done)
	}()

	assert.Eventually(t, func() bool {
		return int(m.runs.Load()) >= wantRuns
	}, 5*time.Second, 5*time.Millisecond, "expected at least %d Run calls", wantRuns)

	cancel()
	<-done
	assert.GreaterOrEqual(t, int(m.runs.Load()), wantRuns)
}

func TestSupervisedMonitor_DegradedEventFired(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	origInitial := supervisedBackoffInitial
	origThreshold := supervisedDegradedThreshold
	supervisedBackoffInitial = 1 * time.Millisecond
	supervisedDegradedThreshold = 3
	t.Cleanup(func() {
		supervisedBackoffInitial = origInitial
		supervisedDegradedThreshold = origThreshold
	})

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
		SupervisedMonitor(ctx, m, nil)
		close(done)
	}()

	assert.Eventually(t, func() bool {
		return int(m.runs.Load()) >= wantRuns
	}, 5*time.Second, 1*time.Millisecond, "expected at least %d Run calls", wantRuns)

	cancel()
	<-done
	assert.GreaterOrEqual(t, int(m.runs.Load()), wantRuns)
}

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

	const wantRuns = 5
	m := &fakeMonitor{
		name: "test-monitor",
		behavior: func(ctx context.Context, run int) error {
			switch run {
			case 1, 2:
				return errors.New("fast crash")
			case 3:
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
		SupervisedMonitor(ctx, m, nil)
		close(done)
	}()

	assert.Eventually(t, func() bool {
		return int(m.runs.Load()) >= wantRuns
	}, 5*time.Second, 1*time.Millisecond, "expected at least %d Run calls", wantRuns)

	cancel()
	<-done
}

func TestSupervisedMonitor_BackoffCapAtMax(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	origInitial := supervisedBackoffInitial
	origCap := supervisedBackoffCap
	supervisedBackoffInitial = 1 * time.Millisecond
	supervisedBackoffCap = 8 * time.Millisecond
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
		SupervisedMonitor(ctx, m, nil)
		close(done)
	}()

	assert.Eventually(t, func() bool {
		return int(m.runs.Load()) >= wantRuns
	}, 1*time.Second, 1*time.Millisecond, "expected at least %d Run calls", wantRuns)

	cancel()
	<-done
}
