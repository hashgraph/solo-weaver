// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTrafficShaperMonitor_RunReturnsOnContextCancel verifies Run starts both
// responsibilities and returns nil promptly once ctx is cancelled.
func TestTrafficShaperMonitor_RunReturnsOnContextCancel(t *testing.T) {
	m := NewTrafficShaperMonitor(nil)
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

	m := NewTrafficShaperMonitor(nil)
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
	m := NewTrafficShaperMonitor(nil)
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
