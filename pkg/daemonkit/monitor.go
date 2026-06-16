// SPDX-License-Identifier: Apache-2.0

// Package daemonkit provides a reusable kernel for long-running daemons: a
// supervised-monitor restart loop, a Unix-socket HTTP control plane, and
// sd_notify integration. It depends only on the standard library, errorx, and
// golang.org/x/sync/errgroup, and intentionally imports nothing under
// internal/... or cmd/... so it can be shared across daemons (e.g. the
// solo-provisioner daemon and a future solo-operator daemon).
package daemonkit

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
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

// StatusError is a rich, operator-facing error descriptor used in /status for
// both monitor connectivity failures and component probe (disk prerequisite)
// failures. Every populated field gives the operator enough context to act
// without opening journalctl.
type StatusError struct {
	// Reason is a stable, machine-readable key matching the log reason field
	// (e.g. "UpgradeMonitorListError", "UpgradeDirOwnershipCheckFailed").
	Reason string `json:"reason"`

	// Message is the human-readable error string.
	Message string `json:"message"`

	// Resolution is an actionable command or instruction the operator should
	// run to resolve the issue. Empty when no specific remediation is known.
	Resolution string `json:"resolution,omitempty"`

	// Since is the RFC 3339 timestamp of when this error was first observed.
	Since string `json:"since"`
}

// MonitorState describes the runtime state of a single supervised monitor.
// State values:
//   - "running"         — monitor is executing normally
//   - "degraded"        — monitor is running but its last operation failed;
//     see Error for details; the monitor continues retrying automatically
//   - "backoff:<dur>"   — monitor crashed (Run returned non-nil) and is
//     waiting before restart
//   - "stopped"         — monitor exited cleanly (ctx cancelled or nil return)
type MonitorState struct {
	State string       `json:"state"`
	Error *StatusError `json:"error,omitempty"`
}

// ConnectivityMonitor is optionally implemented by monitors that maintain an
// in-process record of their last connectivity error (e.g. a K8s watch failure).
// statusSnapshot overlays ConnectivityError onto the tracker state so failures
// are visible via /status even while the goroutine is alive and retrying inside
// Run() — a goroutine in a retry loop is "running" by the supervisor's
// definition, but operators need to see the connectivity problem.
type ConnectivityMonitor interface {
	MonitorRunner
	// ConnectivityError returns the current connectivity failure, or nil when
	// the monitor's last operation completed successfully. Recovery (a
	// successful list + watch cycle) must clear the error within one cycle.
	//
	// Concurrency: implementations MUST make this safe for concurrent read.
	// The daemon's HTTP server goroutine calls ConnectivityError (via
	// statusSnapshot) while the monitor's own Run goroutine is writing the
	// underlying field. Guard the field with an atomic (e.g.
	// atomic.Pointer[StatusError]) or a mutex; a plain field read/written
	// from both goroutines is a data race.
	ConnectivityError() *StatusError
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
			slog.Info("Monitor stopped cleanly",
				"reason", "MonitorStopped",
				"monitor", m.Name())
			return
		}

		// nil return without ctx cancellation → also clean exit.
		if err == nil {
			setState("stopped")
			slog.Info("Monitor exited without error and without context cancellation — not restarting",
				"reason", "MonitorExited",
				"monitor", m.Name())
			return
		}

		// Crash path.
		//
		// A stable run before this crash means the previous failure streak
		// recovered: reset both the back-off and the consecutive-crash counter
		// BEFORE counting this crash, so the crash that ended a stable period is
		// counted as #1 of a fresh streak. Doing this after the increment would
		// (a) let a post-stable crash spuriously trip the degraded threshold when
		// its pre-reset count happened to land on a multiple of it, and (b) drop
		// that crash from the new streak's count entirely.
		if time.Since(start) >= supervisedStableThreshold {
			backoff = supervisedBackoffInitial
			consecutiveCrashes = 0
		}

		consecutiveCrashes++

		slog.Error("Monitor crashed — restarting after back-off",
			"error", err,
			"reason", "MonitorCrash",
			"monitor", m.Name(),
			"consecutive_crashes", consecutiveCrashes,
			"backoff", backoff)

		// Emit MonitorDegraded at every supervisedDegradedThreshold consecutive
		// crashes so ops is alerted at crash #5, #10, #15, …
		if consecutiveCrashes%supervisedDegradedThreshold == 0 {
			slog.Error("Monitor is crashing repeatedly — operator intervention may be required",
				"error", err,
				"reason", "MonitorDegraded",
				"monitor", m.Name(),
				"consecutive_crashes", consecutiveCrashes,
				"current_backoff", backoff)
		}

		setState(fmt.Sprintf("backoff:%s", backoff))

		select {
		case <-ctx.Done():
			setState("stopped")
			slog.Info("Monitor restart cancelled — context done",
				"reason", "MonitorStopped",
				"monitor", m.Name())
			return
		case <-time.After(backoff):
		}

		// Grow backoff for the next potential crash (capped).
		// Note: the updated value takes effect on the *next* crash, not the current
		// one. So the wait sequence for consecutive crashes is:
		//   crash 1 → sleep 5s  (initial)
		//   crash 2 → sleep 10s
		//   crash 3 → sleep 20s … up to 5 min cap
		// This is intentional: the first sleep gives the system a moment to recover;
		// subsequent sleeps grow to reduce pressure during sustained failures.
		nextBackoff := time.Duration(float64(backoff) * supervisedBackoffMultiplier)
		if nextBackoff > supervisedBackoffCap {
			nextBackoff = supervisedBackoffCap
		}
		backoff = nextBackoff
	}
}
