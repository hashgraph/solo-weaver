// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- leaf probe test double ----

type fakeLeafProbe struct {
	fn func(ctx context.Context) error
}

func (p *fakeLeafProbe) Probe(ctx context.Context) error { return p.fn(ctx) }

func succeedingProbe() Probe {
	return &fakeLeafProbe{fn: func(_ context.Context) error { return nil }}
}

func failingProbe() Probe {
	return &fakeLeafProbe{fn: func(_ context.Context) error { return errors.New("probe failed") }}
}

func blockingProbe() Probe {
	return &fakeLeafProbe{fn: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
}

func unblockedBy(ch <-chan struct{}) Probe {
	return &fakeLeafProbe{fn: func(ctx context.Context) error {
		select {
		case <-ch:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// ---- ProbableMonitor test double ----

type fakeProbableMonitor struct {
	fakeMonitor
	probe Probe
}

func (m *fakeProbableMonitor) RequiredProbe() Probe { return m.probe }

// ---- CompositeProbe tests ----

func TestCompositeProbe_AllPass(t *testing.T) {
	cp := NewCompositeProbe("test", succeedingProbe(), succeedingProbe())
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	assert.NoError(t, cp.Probe(ctx))
}

func TestCompositeProbe_OneFailCancelsOthers(t *testing.T) {
	cp := NewCompositeProbe("test", failingProbe(), blockingProbe())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := cp.Probe(ctx)
	assert.Error(t, err)
}

func TestCompositeProbe_CtxCancelAborts(t *testing.T) {
	cp := NewCompositeProbe("test", blockingProbe(), blockingProbe())
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- cp.Probe(ctx) }()

	cancel()
	select {
	case err := <-done:
		assert.Error(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("CompositeProbe did not return after ctx cancel")
	}
}

func TestCompositeProbe_NestedComposite(t *testing.T) {
	inner := NewCompositeProbe("inner", succeedingProbe(), succeedingProbe())
	outer := NewCompositeProbe("outer", succeedingProbe(), inner)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	assert.NoError(t, outer.Probe(ctx))
}

// ---- BuildComponentProbe tests ----

func TestBuildComponentProbe_NoProbableMonitors(t *testing.T) {
	monitors := []MonitorRunner{
		&fakeMonitor{name: "plain-monitor"},
	}
	assert.Nil(t, BuildComponentProbe("host-only", monitors))
}

func TestBuildComponentProbe_CollectsFromProbableMonitors(t *testing.T) {
	unblock1 := make(chan struct{})
	unblock2 := make(chan struct{})

	m1 := &fakeProbableMonitor{
		fakeMonitor: fakeMonitor{name: "m1"},
		probe:       unblockedBy(unblock1),
	}
	m2 := &fakeProbableMonitor{
		fakeMonitor: fakeMonitor{name: "m2"},
		probe:       unblockedBy(unblock2),
	}
	plain := &fakeMonitor{name: "plain"}

	probe := BuildComponentProbe("cn", []MonitorRunner{m1, plain, m2})
	require.NotNil(t, probe)
	assert.Equal(t, "cn", probe.ComponentName())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- probe.Probe(ctx) }()

	select {
	case <-done:
		t.Fatal("probe returned before both sub-probes passed")
	case <-time.After(30 * time.Millisecond):
	}

	close(unblock1)
	close(unblock2)

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("probe did not return after all sub-probes passed")
	}
}

// ---- StatusTracker tests ----

func TestStatusTracker_SetAndSnapshot(t *testing.T) {
	tr := NewStatusTracker()
	tr.set("upgrade-monitor", "running")
	tr.set("migration-monitor", "backoff:5s")

	snap := tr.Snapshot()
	require.Len(t, snap, 2)
	assert.Equal(t, MonitorState{State: "running"}, snap["upgrade-monitor"])
	assert.Equal(t, MonitorState{State: "backoff:5s"}, snap["migration-monitor"])
}

func TestStatusTracker_SnapshotIsACopy(t *testing.T) {
	tr := NewStatusTracker()
	tr.set("m", "running")
	snap1 := tr.Snapshot()
	tr.set("m", "stopped")
	snap2 := tr.Snapshot()
	assert.Equal(t, "running", snap1["m"].State)
	assert.Equal(t, "stopped", snap2["m"].State)
}

// ---- SupervisedMonitor + StatusTracker integration ----

func TestSupervisedMonitor_TrackerUpdated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	running := make(chan struct{})
	m := &fakeMonitor{
		name: "tracked-monitor",
		behavior: func(ctx context.Context, run int) error {
			close(running)
			<-ctx.Done()
			return nil
		},
	}

	tracker := NewStatusTracker()
	done := make(chan struct{})
	go func() { SupervisedMonitor(ctx, m, tracker); close(done) }()

	select {
	case <-running:
	case <-time.After(1 * time.Second):
		t.Fatal("monitor did not start")
	}

	assert.Eventually(t, func() bool {
		return tracker.Snapshot()["tracked-monitor"].State == "running"
	}, 500*time.Millisecond, 5*time.Millisecond)

	cancel()
	<-done
	assert.Equal(t, "stopped", tracker.Snapshot()["tracked-monitor"].State)
}

func TestSupervisedMonitor_TrackerBackoffState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	origInitial := supervisedBackoffInitial
	supervisedBackoffInitial = 50 * time.Millisecond
	t.Cleanup(func() { supervisedBackoffInitial = origInitial })

	crashed := make(chan struct{}, 1)
	m := &fakeMonitor{
		name: "backoff-monitor",
		behavior: func(ctx context.Context, run int) error {
			if run == 1 {
				crashed <- struct{}{}
				return errors.New("crash")
			}
			<-ctx.Done()
			return nil
		},
	}

	tracker := NewStatusTracker()
	go SupervisedMonitor(ctx, m, tracker)

	<-crashed
	assert.Eventually(t, func() bool {
		s := tracker.Snapshot()["backoff-monitor"].State
		return len(s) > len("backoff:") && s[:len("backoff:")] == "backoff:"
	}, 500*time.Millisecond, 5*time.Millisecond, "expected backoff state")

	cancel()
}
