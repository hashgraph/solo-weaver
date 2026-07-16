// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"sync"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/daemon/privexec"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/joomcode/errorx"
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

// TrafficShaperMonitor is the daemonkit.MonitorRunner for the block-node
// traffic-shaper workflow. It owns two long-lived responsibilities that run
// concurrently under Run:
//
//   - the pod-lifecycle watcher (resolves host-side veths and installs/rebinds
//     ingress HTB qdiscs — implemented in #748/#749), and
//   - the statusz poll loop (diffs statusz membership against live nft sets and
//     applies deltas — implemented in #751/#752/#754/#755).
//
// Each responsibility is independently retried with exponential back-off so a
// fault in one cannot stop the other or crash the daemon.
type TrafficShaperMonitor struct {
	// resolver resolves the host-side veth name for a BN pod (story #747).
	// Used by runPodWatcher once the real watch loop lands in #748.
	resolver *VethResolver
	// lister reads live nft set membership so the poll loop can diff desired
	// statusz-derived membership against the kernel. Satisfied by the network
	// policy Runner; a fake is injected in tests.
	lister elementLister
	// delegator runs privileged solo-provisioner subcommands under sudo. The
	// daemon is unprivileged (User=weaver), so both responsibilities delegate
	// their privileged work through it: the poll loop applies membership via
	// `network policy set` and the watcher installs veth qdiscs via `block node
	// tc-attach`. Held here so the poll loop and watcher can consume it once
	// their apply/attach paths are wired, without a constructor change.
	delegator privexec.Delegator
}

// NewTrafficShaperMonitor constructs a TrafficShaperMonitor with the given
// VethResolver. The resolver is wired into runPodWatcher by #748; it is held
// here so #748 can use it without further constructor changes. The live-set
// reader defaults to the network policy exec Runner, which reads the `inet
// weaver` sets via `nft list set`. The delegator defaults to the sudo-backed
// privileged-exec seam so the poll loop and watcher can perform their
// privileged work without the unprivileged daemon holding root itself.
func NewTrafficShaperMonitor(resolver *VethResolver) *TrafficShaperMonitor {
	return &TrafficShaperMonitor{
		resolver:  resolver,
		lister:    policy.NewExecRunner(),
		delegator: privexec.New(),
	}
}

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

// runPodWatcher is the pod-lifecycle watcher responsibility. Stub replaced by
// the real watch loop in #748; m.resolver is ready for use there.
func (m *TrafficShaperMonitor) runPodWatcher(ctx context.Context) error {
	logx.As().Info().
		Str("reason", "TrafficShaperPodWatcherStub").
		Str("monitor", m.Name()).
		Msg("pod-lifecycle watcher not yet implemented — stub running")
	<-ctx.Done()
	return nil
}

// runStatuszPoll is the statusz poll-loop responsibility. The category→policy
// diff engine is in place; the surrounding loop is not yet wired — statusz fetch
// lands in #751, delta apply in #754, and bootstrap/outage policy in #755. It
// exercises the diff seam once with an empty desired view so the plumbing (the
// live-set reader and reconcilePolicies) is ready for #754 to drive from real
// statusz data. An empty view maps to zero deltas and touches no set.
func (m *TrafficShaperMonitor) runStatuszPoll(ctx context.Context) error {
	deltas, err := m.reconcilePolicies(ctx, nil)
	if err != nil {
		return err
	}
	logx.As().Info().
		Str("reason", "TrafficShaperStatuszPollStub").
		Str("monitor", m.Name()).
		Int("policy_deltas", len(deltas)).
		Msg("statusz poll loop not yet wired — diff engine ready, awaiting statusz client")
	<-ctx.Done()
	return nil
}

// reconcilePolicies computes the per-policy membership deltas between the desired
// category endpoints and the live nft sets. It does NOT apply them — applying is
// #754's responsibility. It is the seam the poll loop drives once the statusz
// client (#751) supplies real endpoints.
func (m *TrafficShaperMonitor) reconcilePolicies(ctx context.Context, ce CategoryEndpoints) ([]PolicyDelta, error) {
	if m.lister == nil {
		return nil, errorx.IllegalState.New("traffic-shaper monitor has no live-set reader")
	}
	return computePolicyDeltas(ctx, m.lister, ce)
}

// minDuration returns the smaller of a and b.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
