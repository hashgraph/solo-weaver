// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"sync"
	"time"

	"github.com/automa-saga/logx"
)

// Per-responsibility back-off bounds. A fault in one subsystem (pod watcher or
// statusz poll loop) is retried in place with exponential back-off rather than
// being propagated to Run — so a subsystem fault never kills the monitor
// goroutine (and thus never trips the top-level supervisor or the daemon
// process). Issue #746 specifies a 5 s floor; the upgrade monitor's hand-rolled
// pattern (consensus/upgrade_monitor.go) uses the same factor/cap shape.
//
// Initial/Max are vars (not consts) only so tests can shrink them to avoid
// real-time waits; production never reassigns them.
var (
	responsibilityBackoffInitial = 5 * time.Second
	responsibilityBackoffMax     = 5 * time.Minute
)

const responsibilityBackoffFactor = 2.0

// TrafficShaperMonitor is the daemonkit.MonitorRunner skeleton for the
// block-node traffic-shaper workflow. It owns two long-lived responsibilities
// that run concurrently under Run:
//
//   - the pod-lifecycle watcher (resolves host-side veths and installs/rebinds
//     ingress HTB qdiscs — implemented in #747/#748/#749), and
//   - the statusz poll loop (diffs statusz membership against live nft sets and
//     applies deltas — implemented in #751/#752/#754/#755).
//
// This story (#746) delivers only the supervision skeleton: both responsibility
// bodies are stubs. Each responsibility is independently retried with
// exponential back-off so a fault in one cannot stop the other or crash the
// daemon.
type TrafficShaperMonitor struct{}

// NewTrafficShaperMonitor constructs a TrafficShaperMonitor.
func NewTrafficShaperMonitor() *TrafficShaperMonitor { return &TrafficShaperMonitor{} }

// Name implements daemonkit.MonitorRunner.
func (m *TrafficShaperMonitor) Name() string { return "bn-traffic-shaper-monitor" }

// Run implements daemonkit.MonitorRunner. It starts the pod-lifecycle watcher
// and the statusz poll loop concurrently and blocks until ctx is cancelled. It
// always returns nil: subsystem faults are absorbed by superviseResponsibility,
// so the only way Run returns is a clean ctx cancellation.
func (m *TrafficShaperMonitor) Run(ctx context.Context) error {
	logx.As().Info().
		Str("reason", "TrafficShaperMonitorStarting").
		Str("monitor", m.Name()).
		Msg("block-node traffic-shaper monitor starting")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		m.superviseResponsibility(ctx, "pod-watcher", m.runPodWatcher)
	}()
	go func() {
		defer wg.Done()
		m.superviseResponsibility(ctx, "statusz-poll", m.runStatuszPoll)
	}()
	wg.Wait()
	return nil
}

// superviseResponsibility runs fn in a retry loop. A non-nil error from fn is
// logged and retried after an exponential back-off (5 s → 5 min); the error is
// never returned, so one responsibility faulting cannot stop the other or crash
// the monitor. The back-off resets after fn runs without error. The loop exits
// only when ctx is cancelled.
func (m *TrafficShaperMonitor) superviseResponsibility(ctx context.Context, name string, fn func(context.Context) error) {
	backoff := responsibilityBackoffInitial
	for {
		if ctx.Err() != nil {
			return
		}

		err := fn(ctx)
		if ctx.Err() != nil {
			// Clean shutdown — ctx cancellation is not a fault.
			return
		}
		if err == nil {
			// Responsibility returned without ctx being cancelled; reset the
			// back-off and re-enter immediately.
			backoff = responsibilityBackoffInitial
			continue
		}

		logx.As().Warn().Err(err).
			Str("reason", "TrafficShaperResponsibilityFaulted").
			Str("monitor", m.Name()).
			Str("responsibility", name).
			Dur("retry_in", backoff).
			Msg("traffic-shaper responsibility faulted — retrying after back-off")

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = minDuration(time.Duration(float64(backoff)*responsibilityBackoffFactor), responsibilityBackoffMax)
	}
}

// runPodWatcher is the pod-lifecycle watcher responsibility. Stub for #746; the
// real implementation lands in #747 (veth resolution), #748 (HTB install) and
// #749 (rebind/cleanup).
func (m *TrafficShaperMonitor) runPodWatcher(ctx context.Context) error {
	logx.As().Info().
		Str("reason", "TrafficShaperPodWatcherStub").
		Str("monitor", m.Name()).
		Msg("pod-lifecycle watcher not yet implemented — stub running")
	<-ctx.Done()
	return nil
}

// runStatuszPoll is the statusz poll-loop responsibility. Stub for #746; the
// real implementation lands in #751 (statusz client), #752 (category→policy
// diff), #754 (membership apply) and #755 (bootstrap/outage policy).
func (m *TrafficShaperMonitor) runStatuszPoll(ctx context.Context) error {
	logx.As().Info().
		Str("reason", "TrafficShaperStatuszPollStub").
		Str("monitor", m.Name()).
		Msg("statusz poll loop not yet implemented — stub running")
	<-ctx.Done()
	return nil
}

// minDuration returns the smaller of a and b.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
