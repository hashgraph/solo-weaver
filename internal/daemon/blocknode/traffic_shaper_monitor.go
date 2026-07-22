// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"sync"
	"time"

	"github.com/automa-saga/logx"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/hashgraph/solo-weaver/internal/daemon/privexec"
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
//   - the statusz poll loop, which reconciles the nft policy membership from
//     statusz. Its reconcile logic lives in the `block node reconcile-shaper`
//     CLI worker; this loop is the daemon-side scheduler that execs that worker
//     once per poll tick (see runStatuszPoll).
//
// Each responsibility is independently retried with exponential back-off so a
// fault in one cannot stop the other or crash the daemon.
type TrafficShaperMonitor struct {
	// resolver resolves the host-side veth name for a BN pod (story #747). Held
	// as an interface so the pod watcher can be unit-tested with a fake.
	resolver vethResolver
	// delegator runs privileged solo-provisioner subcommands under sudo. The
	// daemon is unprivileged (User=weaver), so both responsibilities delegate
	// their privileged work through it: the poll loop applies membership via
	// `network policy set` and the watcher installs veth qdiscs via `block node
	// tc-attach`.
	delegator privexec.Delegator
	// client watches BN pods in namespace. Nil only in unit tests that exercise
	// the supervisor loop without a real cluster (runPodWatcher degrades to an
	// idle block in that case).
	client kubernetes.Interface
	// namespace is the BN orbit namespace the pod watcher scopes its list/watch
	// to.
	namespace string

	// statuszURL is the base URL of the block node's statusz endpoints the poll
	// loop reconciles from (components.block_node.statusz.base_url). Empty means
	// no source is configured and the poll loop idles.
	statuszURL string
	// pollInterval is the steady-state cadence of the statusz poll loop
	// (components.block_node.statusz.poll_interval). A non-positive value falls
	// back to defaultStatuszPollInterval.
	pollInterval time.Duration

	// mu guards attached and inflight.
	mu sync.Mutex
	// attached maps pod UID → installed veth, used to dedupe redundant attaches
	// and to know which veth to detach on pod delete.
	attached map[types.UID]string
	// inflight tracks pods with an attach goroutine currently running, so the
	// watch loop never launches a second concurrent attach for the same pod
	// while its (retrying) resolve is still in progress.
	inflight map[types.UID]bool
}

// NewTrafficShaperMonitor constructs a TrafficShaperMonitor. resolver and client
// are built from the BN-scoped kubeconfig by NewComponent; namespace is the BN
// orbit. statuszURL and pollInterval configure the poll loop (an empty URL keeps
// it idle). The delegator defaults to the sudo-backed privileged-exec seam so
// both responsibilities delegate their privileged work without the unprivileged
// daemon holding root.
func NewTrafficShaperMonitor(resolver *VethResolver, client kubernetes.Interface, namespace, statuszURL string, pollInterval time.Duration) *TrafficShaperMonitor {
	return &TrafficShaperMonitor{
		resolver:     resolver,
		delegator:    privexec.New(),
		client:       client,
		namespace:    namespace,
		statuszURL:   statuszURL,
		pollInterval: pollInterval,
		attached:     make(map[types.UID]string),
		inflight:     make(map[types.UID]bool),
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

// defaultStatuszPollInterval mirrors daemon.DefaultStatuszPollInterval as a
// local fallback (the daemon package can't be imported here without a cycle) so
// a monitor constructed with a non-positive interval — only reachable in tests;
// daemon.go always passes StatuszConfig.EffectivePollInterval() — still ticks at
// a sane cadence rather than panicking in time.NewTicker.
const defaultStatuszPollInterval = 5 * time.Second

// statuszForceResyncInterval bounds how long the poll loop trusts the
// desired-membership digest before forcing a full apply even when the digest is
// unchanged. The digest is computed over the desired membership (from statusz),
// not the live nft sets, so a pure digest gate would never notice an
// out-of-band edit to the daemon-owned sets; a periodic forced apply re-diffs
// live nft and self-heals that drift. A var (not const) so tests can shrink it.
var statuszForceResyncInterval = time.Minute

// runStatuszPoll is the statusz poll-loop responsibility: the daemon-side
// scheduler that execs the `solo-provisioner block node reconcile-shaper` worker
// once per poll tick to keep the daemon-owned nft policy sets reconciled from
// statusz.
//
// When no statusz base_url is configured it stays inert — it logs once and
// blocks on ctx, touching no set — so installs that never configured statusz
// keep a quiet loop rather than erroring.
//
// Each tick first runs the unprivileged `--check` digest probe; the privileged
// apply (a root sudo exec) fires only when the desired membership changed or the
// force-resync interval has elapsed since the last apply. A worker-exec failure
// is returned to superviseResponsibility, which retries with the 5s→5min
// back-off; a ctx cancellation returns nil (clean shutdown).
func (m *TrafficShaperMonitor) runStatuszPoll(ctx context.Context) error {
	if m.statuszURL == "" {
		logx.As().Info().
			Str("reason", "TrafficShaperStatuszUnconfigured").
			Str("monitor", m.Name()).
			Msg("no statusz base_url configured — traffic-shaper poll loop idle")
		<-ctx.Done()
		return nil
	}

	interval := m.pollInterval
	if interval <= 0 {
		interval = defaultStatuszPollInterval
	}

	logx.As().Info().
		Str("reason", "TrafficShaperStatuszPollStarting").
		Str("monitor", m.Name()).
		Str("statusz_url", m.statuszURL).
		Dur("poll_interval", interval).
		Msg("statusz poll loop starting")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// lastDigest/lastApply are local to this invocation: a fault restarts
	// runStatuszPoll (via superviseResponsibility), which resets them and forces
	// a fresh apply — correct, since post-fault we can't trust the prior state.
	var lastDigest string
	var lastApply time.Time

	reconcile := func() error {
		digest, err := m.delegator.ReconcileShaperCheck(ctx, m.statuszURL)
		if err != nil {
			return err
		}
		// Skip the root apply only when the desired membership is unchanged AND
		// the force-resync window has not elapsed. lastApply.IsZero() forces the
		// first reconcile of the loop to apply.
		if digest == lastDigest && !lastApply.IsZero() && time.Since(lastApply) < statuszForceResyncInterval {
			return nil
		}
		if err := m.delegator.ReconcileShaper(ctx, m.statuszURL); err != nil {
			return err
		}
		lastDigest = digest
		lastApply = time.Now()
		return nil
	}

	// runReconcile absorbs a cancellation-caused fault: when ctx is cancelled
	// mid-exec, the worker exec (exec.CommandContext) is killed and returns a
	// context.Canceled error that reconcile propagates. That is a clean shutdown,
	// not a subsystem fault, so it must surface as nil — both to honor this
	// function's contract and so a future direct caller never mistakes a
	// shutdown for a fault.
	runReconcile := func() error {
		if err := reconcile(); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		return nil
	}

	// Reconcile once on entry so a fresh daemon converges immediately instead of
	// waiting a full interval for the first tick.
	if err := runReconcile(); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := runReconcile(); err != nil {
				return err
			}
		}
	}
}

// minDuration returns the smaller of a and b.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
