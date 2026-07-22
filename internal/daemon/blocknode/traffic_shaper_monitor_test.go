// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// pollFakeDelegator is a thread-safe Delegator fake for the statusz poll-loop
// tests. It scripts the digest returned by successive ReconcileShaperCheck calls
// (holding the last value once exhausted) and records how many times each
// reconcile path ran, so tests can assert the check-gate/apply behavior while
// runStatuszPoll runs in its own goroutine.
type pollFakeDelegator struct {
	mu       sync.Mutex
	digests  []string // scripted check digests, in call order
	checkErr error
	applyErr error
	lastURL  string

	// blockUntilCancel makes ReconcileShaperCheck block until ctx is cancelled
	// and then return ctx.Err() — simulating a worker exec killed by a shutdown
	// mid-flight, which must surface as a clean (nil) exit from runStatuszPoll.
	blockUntilCancel bool

	checkCalls atomic.Int32
	applyCalls atomic.Int32
}

func (f *pollFakeDelegator) Run(context.Context, ...string) ([]byte, error)           { return nil, nil }
func (f *pollFakeDelegator) NetworkPolicySet(context.Context, string, []string) error { return nil }
func (f *pollFakeDelegator) TCAttach(context.Context, string) error                   { return nil }
func (f *pollFakeDelegator) TCDetach(context.Context, string) error                   { return nil }

func (f *pollFakeDelegator) ReconcileShaperCheck(ctx context.Context, url string) (string, error) {
	n := f.checkCalls.Add(1)
	if f.blockUntilCancel {
		<-ctx.Done()
		return "", ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastURL = url
	if f.checkErr != nil {
		return "", f.checkErr
	}
	if len(f.digests) == 0 {
		return "", nil
	}
	idx := int(n - 1)
	if idx >= len(f.digests) {
		idx = len(f.digests) - 1 // hold the last scripted digest
	}
	return f.digests[idx], nil
}

func (f *pollFakeDelegator) ReconcileShaper(_ context.Context, url string) error {
	f.applyCalls.Add(1)
	f.mu.Lock()
	f.lastURL = url
	f.mu.Unlock()
	return f.applyErr
}

// newPollMonitor builds a monitor wired to a poll fake, bypassing the
// privexec-backed constructor.
func newPollMonitor(d *pollFakeDelegator, statuszURL string, interval time.Duration) *TrafficShaperMonitor {
	return &TrafficShaperMonitor{
		delegator:    d,
		statuszURL:   statuszURL,
		pollInterval: interval,
	}
}

// waitForCount blocks until get() >= want or the deadline elapses.
func waitForCount(t *testing.T, get func() int32, want int32) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if get() >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for count >= %d (last: %d)", want, get())
		case <-time.After(time.Millisecond):
		}
	}
}

// TestRunStatuszPoll_InertWhenUnset verifies that with no statusz base_url the
// poll loop touches no delegator path and returns nil on ctx cancel.
func TestRunStatuszPoll_InertWhenUnset(t *testing.T) {
	d := &pollFakeDelegator{}
	m := newPollMonitor(d, "", time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- m.runStatuszPoll(ctx) }()
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("runStatuszPoll did not return after ctx cancellation")
	}
	require.Zero(t, d.checkCalls.Load(), "no check exec when unconfigured")
	require.Zero(t, d.applyCalls.Load(), "no apply exec when unconfigured")
}

// TestRunStatuszPoll_AppliesOnEntryThenGatesUnchanged verifies the loop applies
// once on entry and then skips the root apply while the digest is unchanged
// (with the force-resync window held open).
func TestRunStatuszPoll_AppliesOnEntryThenGatesUnchanged(t *testing.T) {
	restore := statuszForceResyncInterval
	statuszForceResyncInterval = time.Hour
	t.Cleanup(func() { statuszForceResyncInterval = restore })

	d := &pollFakeDelegator{digests: []string{"D1"}} // holds "D1" for every check
	m := newPollMonitor(d, "http://127.0.0.1:8080", time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- m.runStatuszPoll(ctx) }()

	waitForCount(t, d.checkCalls.Load, 3)
	cancel()
	<-done

	require.Equal(t, int32(1), d.applyCalls.Load(),
		"unchanged digest must apply only once (on entry), then gate")
	d.mu.Lock()
	require.Equal(t, "http://127.0.0.1:8080", d.lastURL)
	d.mu.Unlock()
}

// TestRunStatuszPoll_AppliesWhenDigestChanges verifies a changed digest triggers
// a second apply.
func TestRunStatuszPoll_AppliesWhenDigestChanges(t *testing.T) {
	restore := statuszForceResyncInterval
	statuszForceResyncInterval = time.Hour
	t.Cleanup(func() { statuszForceResyncInterval = restore })

	d := &pollFakeDelegator{digests: []string{"D1", "D2"}} // D1 then D2 (held)
	m := newPollMonitor(d, "http://127.0.0.1:8080", time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- m.runStatuszPoll(ctx) }()

	waitForCount(t, d.applyCalls.Load, 2)
	cancel()
	<-done

	require.GreaterOrEqual(t, d.applyCalls.Load(), int32(2),
		"entry apply for D1 plus one for the D2 change")
}

// TestRunStatuszPoll_ForceResyncAppliesDespiteUnchangedDigest verifies the
// periodic forced apply: with a zero resync window, an unchanged digest still
// applies every tick (self-healing out-of-band nft drift).
func TestRunStatuszPoll_ForceResyncAppliesDespiteUnchangedDigest(t *testing.T) {
	restore := statuszForceResyncInterval
	statuszForceResyncInterval = 0 // every tick is past due
	t.Cleanup(func() { statuszForceResyncInterval = restore })

	d := &pollFakeDelegator{digests: []string{"D1"}} // never changes
	m := newPollMonitor(d, "http://127.0.0.1:8080", time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- m.runStatuszPoll(ctx) }()

	waitForCount(t, d.applyCalls.Load, 3)
	cancel()
	<-done

	require.GreaterOrEqual(t, d.applyCalls.Load(), int32(3),
		"forced resync must re-apply each tick even when the digest is unchanged")
}

// TestRunStatuszPoll_CheckFaultReturnsError verifies a failing check probe is
// surfaced to the supervisor (returned, not swallowed) so the back-off applies.
func TestRunStatuszPoll_CheckFaultReturnsError(t *testing.T) {
	sentinel := errors.New("statusz unreachable")
	d := &pollFakeDelegator{checkErr: sentinel}
	m := newPollMonitor(d, "http://127.0.0.1:8080", time.Second)

	err := m.runStatuszPoll(context.Background())
	require.ErrorIs(t, err, sentinel)
	require.Zero(t, d.applyCalls.Load(), "apply must not run when the check fails")
}

// TestRunStatuszPoll_ApplyFaultReturnsError verifies a failing apply is returned
// to the supervisor.
func TestRunStatuszPoll_ApplyFaultReturnsError(t *testing.T) {
	sentinel := errors.New("nft: operation not permitted")
	d := &pollFakeDelegator{digests: []string{"D1"}, applyErr: sentinel}
	m := newPollMonitor(d, "http://127.0.0.1:8080", time.Second)

	err := m.runStatuszPoll(context.Background())
	require.ErrorIs(t, err, sentinel)
}

// TestRunStatuszPoll_CancelMidExecReturnsNil verifies that a cancellation while a
// worker exec is in flight surfaces as a clean (nil) exit, not a fault — the loop
// must honor its "ctx cancellation returns nil" contract even when the error
// originates from the killed exec rather than the select.
func TestRunStatuszPoll_CancelMidExecReturnsNil(t *testing.T) {
	d := &pollFakeDelegator{blockUntilCancel: true}
	m := newPollMonitor(d, "http://127.0.0.1:8080", time.Second)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- m.runStatuszPoll(ctx) }()

	// Wait until the entry reconcile is blocked inside the check exec, then cancel.
	waitForCount(t, d.checkCalls.Load, 1)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err, "cancel mid-exec must be a clean shutdown, not a fault")
	case <-time.After(time.Second):
		t.Fatal("runStatuszPoll did not return after ctx cancellation")
	}
	require.Zero(t, d.applyCalls.Load(), "apply must not run when the check was cancelled")
}

// TestTrafficShaperMonitor_RunReturnsOnContextCancel verifies Run starts both
// responsibilities and returns nil promptly once ctx is cancelled.
func TestTrafficShaperMonitor_RunReturnsOnContextCancel(t *testing.T) {
	m := NewTrafficShaperMonitor(nil, nil, "", "", 0)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after ctx cancellation")
	}
}

// TestSuperviseResponsibility_RetriesFaultsWithoutPropagating verifies that a
// faulting responsibility is retried with back-off and never crashes the
// supervisor: superviseResponsibility returns only when ctx is cancelled, and a
// returned error is swallowed (not propagated).
func TestSuperviseResponsibility_RetriesFaultsWithoutPropagating(t *testing.T) {
	origInitial, origMax := responsibilityBackoffInitial, responsibilityBackoffMax
	responsibilityBackoffInitial = time.Millisecond
	responsibilityBackoffMax = 2 * time.Millisecond
	t.Cleanup(func() {
		responsibilityBackoffInitial = origInitial
		responsibilityBackoffMax = origMax
	})

	m := NewTrafficShaperMonitor(nil, nil, "", "", 0)
	ctx, cancel := context.WithCancel(context.Background())

	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		m.superviseResponsibility(ctx, "test", func(context.Context) error {
			// Fault on every invocation; cancel after a few retries so the loop
			// observes ctx.Err() and exits cleanly.
			if calls.Add(1) >= 3 {
				cancel()
			}
			return errors.New("transient subsystem fault")
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("superviseResponsibility did not return after ctx cancellation")
	}

	require.GreaterOrEqual(t, calls.Load(), int32(3),
		"faulting responsibility should be retried multiple times")
}

// TestSuperviseResponsibility_ResetsBackoffAndExitsOnCancel verifies the clean
// path: a responsibility that returns without error (and without ctx being
// cancelled) is re-entered immediately, and the loop exits once ctx is done.
func TestSuperviseResponsibility_ResetsBackoffAndExitsOnCancel(t *testing.T) {
	m := NewTrafficShaperMonitor(nil, nil, "", "", 0)
	ctx, cancel := context.WithCancel(context.Background())

	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		m.superviseResponsibility(ctx, "test", func(context.Context) error {
			if calls.Add(1) >= 3 {
				cancel()
			}
			return nil
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("superviseResponsibility did not return after ctx cancellation")
	}

	require.GreaterOrEqual(t, calls.Load(), int32(3))
}
