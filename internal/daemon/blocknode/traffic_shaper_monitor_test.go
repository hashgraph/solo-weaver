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
		urlChanged:   make(chan struct{}, 1),
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

// TestEffectiveStatuszURL verifies the precedence: a configured base_url override
// wins over discovery; otherwise the discovered URL is used; otherwise "".
func TestEffectiveStatuszURL(t *testing.T) {
	override := &TrafficShaperMonitor{statuszURL: "http://override:8080", discoveredStatuszURL: "http://disc:40983"}
	require.Equal(t, "http://override:8080", override.effectiveStatuszURL(),
		"configured base_url overrides discovery")

	discovered := &TrafficShaperMonitor{discoveredStatuszURL: "http://disc:40983"}
	require.Equal(t, "http://disc:40983", discovered.effectiveStatuszURL(),
		"discovered URL used when no base_url override")

	none := &TrafficShaperMonitor{}
	require.Empty(t, none.effectiveStatuszURL(), "empty when neither configured nor discovered")
}

// TestRunStatuszPoll_ReconcilesOnceDiscovered verifies the loop stays quiet (no
// exec) while no endpoint is available, then reconciles once discovery yields one
// — the zero-config path where a pod appears after the loop starts.
func TestRunStatuszPoll_ReconcilesOnceDiscovered(t *testing.T) {
	d := &pollFakeDelegator{digests: []string{"D1"}}
	m := newPollMonitor(d, "", time.Millisecond) // no base_url override
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- m.runStatuszPoll(ctx) }()

	// A few ticks with no discovered endpoint must not exec anything.
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, d.checkCalls.Load(), "no exec while the endpoint is undiscovered")

	// Discovery lands (as the pod watcher would set it), then the loop reconciles.
	m.mu.Lock()
	m.discoveredStatuszURL = "http://10.1.2.3:40983"
	m.mu.Unlock()

	waitForCount(t, d.applyCalls.Load, 1)
	cancel()
	<-done

	d.mu.Lock()
	require.Equal(t, "http://10.1.2.3:40983", d.lastURL, "reconciled against the discovered URL")
	d.mu.Unlock()
}

// TestSignalURLChanged_RetainsSignalForLateReceiver pins the buffering guarantee
// that closes #1000. The pod watcher and the poll loop start concurrently, so
// discovery routinely signals before the loop reaches its select. With capacity 1
// the signal waits in the buffer; an unbuffered channel would drop it for want of
// a receiver and the loop would sleep a full poll interval with a ready pod.
func TestSignalURLChanged_RetainsSignalForLateReceiver(t *testing.T) {
	m := &TrafficShaperMonitor{urlChanged: make(chan struct{}, 1)}

	m.signalURLChanged() // no receiver is waiting yet

	select {
	case <-m.urlChanged:
	default:
		t.Fatal("signal dropped with no receiver waiting — the poll loop would sleep a full interval (#1000)")
	}
}

// TestSignalURLChanged_CoalescesAndNeverBlocks verifies repeated signals with no
// reader neither block nor queue: extra wake-ups are redundant because the loop
// re-reads the current URL when it wakes. Also covers the zero-value monitor,
// where the channel is nil (unit-test scaffolding constructs monitors directly).
func TestSignalURLChanged_CoalescesAndNeverBlocks(t *testing.T) {
	m := &TrafficShaperMonitor{urlChanged: make(chan struct{}, 1)}
	for range 100 {
		m.signalURLChanged() // must not block once the buffer is full
	}
	require.Len(t, m.urlChanged, 1, "signals coalesce into a single pending wake-up")

	require.NotPanics(t, (&TrafficShaperMonitor{}).signalURLChanged,
		"a monitor with no channel wired must be safe to signal")
}

// TestRunStatuszPoll_DiscoverySignalWakesLoopBeforeNextTick is the regression test
// for #1000. The poll interval is an hour, so the ticker cannot account for any
// reconcile: the only way the loop can converge is by waking on the discovery
// signal. Before the fix this test would hang until the deadline.
func TestRunStatuszPoll_DiscoverySignalWakesLoopBeforeNextTick(t *testing.T) {
	d := &pollFakeDelegator{digests: []string{"D1"}}
	m := newPollMonitor(d, "", time.Hour) // no base_url override; tick is unreachable
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- m.runStatuszPoll(ctx) }()

	// The entry reconcile runs with no endpoint and must stay quiet.
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, d.checkCalls.Load(), "no exec while the endpoint is undiscovered")

	// The pod watcher records an endpoint and signals, as recordDiscoveredStatusz does.
	m.mu.Lock()
	m.discoveredStatuszURL = "http://10.1.2.3:40983"
	m.mu.Unlock()
	m.signalURLChanged()

	waitForCount(t, d.applyCalls.Load, 1)
	cancel()
	require.NoError(t, <-done)

	d.mu.Lock()
	require.Equal(t, "http://10.1.2.3:40983", d.lastURL, "reconciled against the newly discovered URL")
	d.mu.Unlock()
}

// TestRunStatuszPoll_EndpointLossSignalIsHarmless verifies a teardown signal (the
// owning pod went away, so the URL is now empty) wakes the loop without exec'ing
// anything — it only lets the endpoint-lost transition be logged promptly.
func TestRunStatuszPoll_EndpointLossSignalIsHarmless(t *testing.T) {
	d := &pollFakeDelegator{digests: []string{"D1"}}
	m := newPollMonitor(d, "", time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- m.runStatuszPoll(ctx) }()

	m.signalURLChanged() // endpoint still empty, as after a pod delete
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, d.checkCalls.Load(), "a wake-up with no endpoint must not exec")

	cancel()
	require.NoError(t, <-done)
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
