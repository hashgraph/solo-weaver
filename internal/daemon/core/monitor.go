// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/automa-saga/logx"
)

// Back-off and degradation parameters for SupervisedMonitor. Declared as
// package-level vars (not consts) so unit tests can override them without
// sleeping for real durations.
var (
	supervisedBackoffInitial  = 5 * time.Second
	supervisedBackoffCap      = 5 * time.Minute
	supervisedStableThreshold = 60 * time.Second

	// supervisedDegradedThreshold is the number of consecutive crashes (without
	// an intervening stable run) before a MonitorDegraded event is emitted.
	// Fires again on every subsequent multiple of this value (5, 10, 15, …) so
	// ops keeps seeing the alert as long as the monitor stays degraded.
	supervisedDegradedThreshold = 5
)

const supervisedBackoffMultiplier = 2.0

// MonitorState describes the runtime state of a single supervised monitor.
// State values:
//   - "running"         — monitor is executing normally
//   - "backoff:<dur>"   — monitor crashed and is waiting before restart
//   - "stopped"         — monitor exited cleanly (ctx cancelled or nil return)
type MonitorState struct {
	State string `json:"state"`
}

// StatusTracker holds the latest observed state for a set of monitors. It is
// safe for concurrent use; SupervisedMonitor updates it on each state transition.
type StatusTracker struct {
	mu     sync.RWMutex
	states map[string]MonitorState
}

// NewStatusTracker returns an empty StatusTracker.
func NewStatusTracker() *StatusTracker {
	return &StatusTracker{states: make(map[string]MonitorState)}
}

// set records a new state for the named monitor.
func (t *StatusTracker) set(name, state string) {
	t.mu.Lock()
	t.states[name] = MonitorState{State: state}
	t.mu.Unlock()
}

// Snapshot returns a copy of all monitor states at the time of the call.
func (t *StatusTracker) Snapshot() map[string]MonitorState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]MonitorState, len(t.states))
	for k, v := range t.states {
		out[k] = v
	}
	return out
}

// MonitorRunner is the interface that each long-running monitor goroutine must
// implement so it can be managed by SupervisedMonitor.
//
// Implementations must:
//   - Return nil when ctx is cancelled (clean shutdown, no restart).
//   - Return a non-nil error only on unexpected failure (triggers supervised restart).
//   - Be safe to call again after returning an error (the supervisor calls Run again).
type MonitorRunner interface {
	// Run starts the monitor and blocks until ctx is cancelled or the monitor
	// encounters an unrecoverable error. A nil return means clean shutdown; a
	// non-nil return triggers a supervised restart with back-off.
	Run(ctx context.Context) error

	// Name returns a stable, human-readable identifier for the monitor used
	// in structured log entries (e.g. "upgrade-monitor", "migration-monitor").
	Name() string
}

// SupervisedMonitor runs m in a restart loop. When m.Run returns a non-nil
// error the supervisor waits for a back-off delay and then restarts it.
// Clean shutdown (nil return or ctx cancellation) exits the loop immediately
// without restarting.
//
// Back-off strategy:
//   - Starts at supervisedBackoffInitial (5 s).
//   - Doubles on each crash up to supervisedBackoffCap (5 min).
//   - Resets to supervisedBackoffInitial when the monitor runs stably for
//     at least supervisedStableThreshold (60 s) before the next crash.
//
// Degradation alerting:
//   - Tracks consecutive crashes (resets after a stable run).
//   - Emits a MonitorDegraded Error log every supervisedDegradedThreshold
//     consecutive crashes (at crash #5, #10, #15, …) so ops keeps seeing
//     the alert as long as the monitor remains degraded.
//
// This function never returns an error — it absorbs crashes and restarts the
// monitor indefinitely until ctx is cancelled.
//
// tracker may be nil; when non-nil it is updated on every state transition so
// the /status endpoint can report per-monitor state without polling.
func SupervisedMonitor(ctx context.Context, m MonitorRunner, tracker *StatusTracker) {
	backoff := supervisedBackoffInitial
	consecutiveCrashes := 0

	setState := func(state string) {
		if tracker != nil {
			tracker.set(m.Name(), state)
		}
	}

	for {
		start := time.Now()
		setState("running")

		err := m.Run(ctx)

		// ctx cancelled → clean shutdown, do not restart.
		if ctx.Err() != nil {
			setState("stopped")
			logx.As().Info().
				Str("reason", "MonitorStopped").
				Str("monitor", m.Name()).
				Msg("Monitor stopped cleanly")
			return
		}

		// nil return without ctx cancellation → also clean exit.
		if err == nil {
			setState("stopped")
			logx.As().Info().
				Str("reason", "MonitorExited").
				Str("monitor", m.Name()).
				Msg("Monitor exited without error and without context cancellation — not restarting")
			return
		}

		// Crash path.
		consecutiveCrashes++

		logx.As().Error().Err(err).
			Str("reason", "MonitorCrash").
			Str("monitor", m.Name()).
			Int("consecutive_crashes", consecutiveCrashes).
			Dur("backoff", backoff).
			Msg("Monitor crashed — restarting after back-off")

		// Emit MonitorDegraded at every supervisedDegradedThreshold consecutive
		// crashes so ops is alerted at crash #5, #10, #15, …
		if consecutiveCrashes%supervisedDegradedThreshold == 0 {
			logx.As().Error().Err(err).
				Str("reason", "MonitorDegraded").
				Str("monitor", m.Name()).
				Int("consecutive_crashes", consecutiveCrashes).
				Dur("current_backoff", backoff).
				Msg("Monitor is crashing repeatedly — operator intervention may be required")
		}

		// A stable run resets both the back-off and the consecutive-crash counter.
		if time.Since(start) >= supervisedStableThreshold {
			backoff = supervisedBackoffInitial
			consecutiveCrashes = 0
		}

		setState(fmt.Sprintf("backoff:%s", backoff))

		select {
		case <-ctx.Done():
			setState("stopped")
			logx.As().Info().
				Str("reason", "MonitorStopped").
				Str("monitor", m.Name()).
				Msg("Monitor restart cancelled — context done")
			return
		case <-time.After(backoff):
		}

		// Grow backoff for the next potential crash (capped).
		nextBackoff := time.Duration(float64(backoff) * supervisedBackoffMultiplier)
		if nextBackoff > supervisedBackoffCap {
			nextBackoff = supervisedBackoffCap
		}
		backoff = nextBackoff
	}
}
