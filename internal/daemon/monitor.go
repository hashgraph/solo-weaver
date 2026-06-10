// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"time"

	"github.com/automa-saga/logx"
)

// Back-off and degradation parameters for supervisedMonitor. Declared as
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
	// Not exposed as config — this is internal supervisor policy. Tune via
	// recompile or, in future, via a --degraded-threshold daemon flag.
	supervisedDegradedThreshold = 5
)

const supervisedBackoffMultiplier = 2.0

// MonitorRunner is the interface that each long-running monitor goroutine must
// implement so it can be managed by supervisedMonitor.
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

// supervisedMonitor runs m in a restart loop. When m.Run returns a non-nil
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
// monitor indefinitely until ctx is cancelled. Callers that need to detect
// monitor death should couple this with the componentSupervisor (S3).
func supervisedMonitor(ctx context.Context, m MonitorRunner) {
	backoff := supervisedBackoffInitial
	consecutiveCrashes := 0

	for {
		start := time.Now()

		err := m.Run(ctx)

		// ctx cancelled → clean shutdown, do not restart.
		if ctx.Err() != nil {
			logx.As().Info().
				Str("reason", "MonitorStopped").
				Str("monitor", m.Name()).
				Msg("Monitor stopped cleanly")
			return
		}

		// nil return without ctx cancellation → also clean exit.
		if err == nil {
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
		// crashes so ops is alerted at crash #5, #10, #15, … Fires immediately
		// (before the backoff sleep) so the log entry is visible without delay.
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

		select {
		case <-ctx.Done():
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
