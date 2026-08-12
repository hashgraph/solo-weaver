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

	// statuszURL is the operator-configured statusz base URL
	// (components.block_node.statusz.base_url). When set it is an explicit
	// override that takes precedence over pod discovery; empty means "discover
	// the endpoint from the watched BN pod" (see discoveredStatuszURL).
	statuszURL string
	// pollInterval is the steady-state cadence of the statusz poll loop
	// (components.block_node.statusz.poll_interval). A non-positive value falls
	// back to defaultStatuszPollInterval.
	pollInterval time.Duration

	// mu guards attached, inflight, and the discovered-statusz fields.
	mu sync.Mutex
	// attached maps pod UID → installed veth, used to dedupe redundant attaches
	// and to know which veth to detach on pod delete.
	attached map[types.UID]string
	// inflight tracks pods with an attach goroutine currently running, so the
	// watch loop never launches a second concurrent attach for the same pod
	// while its (retrying) resolve is still in progress.
	inflight map[types.UID]bool
	// discoveredStatuszURL is the statusz base URL discovered from the watched BN
	// pod (http://<podIP>:<healthPort>), recorded by the pod watcher when a pod
	// becomes ContainersReady and cleared when that pod is deleted. Empty until a
	// ready BN pod is observed. effectiveStatuszURL prefers statuszURL over this.
	discoveredStatuszURL string
	// discoveredStatuszPod is the UID of the pod that set discoveredStatuszURL, so
	// a delete only clears the endpoint when it is that pod (not some other pod in
	// a multi-pod window) that went away.
	discoveredStatuszPod types.UID

	// urlChanged wakes the statusz poll loop when the pod watcher discovers,
	// changes, or loses the statusz endpoint, so convergence is not deferred to
	// the next ticker tick. The two responsibilities start concurrently, so the
	// entry reconcile usually runs before the watcher's initial list has recorded
	// an endpoint; without this signal the loop would sleep a full poll interval
	// with a ready pod and a reachable statusz (#1000).
	//
	// Capacity 1 with a non-blocking send is load-bearing: it makes the signal
	// order-independent. A send that lands before the loop reaches its select is
	// retained in the buffer and returns immediately on arrival, whereas an
	// unbuffered send would find no receiver, drop, and reintroduce the race it
	// exists to close. Extra wake-ups are harmless — the digest gate skips the
	// privileged apply when the desired state has not changed.
	urlChanged chan struct{}
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
		urlChanged:   make(chan struct{}, 1),
	}
}

// signalURLChanged wakes the statusz poll loop after the discovered endpoint
// changed. The send is non-blocking: when a signal is already buffered the loop
// has not consumed the previous one yet and will observe the latest URL when it
// wakes, so coalescing loses nothing. Safe to call with no loop running (e.g.
// while runStatuszPoll is restarting after a fault) — the buffered signal is
// consumed by the next loop, costing one extra reconcile, which is idempotent.
//
// Never call this while holding m.mu: the send must not be able to interleave
// with a reader of the discovered-statusz fields.
func (m *TrafficShaperMonitor) signalURLChanged() {
	if m.urlChanged == nil {
		// Zero-value monitor (unit-test scaffolding); nothing to wake.
		return
	}
	select {
	case m.urlChanged <- struct{}{}:
	default:
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
const defaultStatuszPollInterval = 5 * time.Minute

// statuszForceResyncInterval bounds how long the poll loop trusts the
// desired-membership digest before forcing a full apply even when the digest is
// unchanged. The digest is computed over the desired membership (from statusz),
// not the live nft sets, so a pure digest gate would never notice an
// out-of-band edit to the daemon-owned sets; a periodic forced apply re-diffs
// live nft and self-heals that drift. Must be greater than the poll interval so
// the digest-delta optimisation has teeth; at the default 5m poll cadence a 1h
// force-resync means only one in twelve ticks is a forced apply. A var (not
// const) so tests can shrink it.
var statuszForceResyncInterval = time.Hour

// effectiveStatuszURL returns the statusz base URL the poll loop should reconcile
// against this tick: the operator-configured base_url override when set,
// otherwise the URL discovered from the watched BN pod, otherwise "" (no source
// yet — the poll loop idles this tick).
func (m *TrafficShaperMonitor) effectiveStatuszURL() string {
	// statuszURL is set once in the constructor and never mutated, so it needs no
	// lock; the discovered URL is written by the pod watcher and does.
	if m.statuszURL != "" {
		return m.statuszURL
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.discoveredStatuszURL
}

// runStatuszPoll is the statusz poll-loop responsibility: the daemon-side
// scheduler that execs the `solo-provisioner block node reconcile-shaper` worker
// once per poll tick to keep the daemon-owned nft policy sets reconciled from
// statusz.
//
// The statusz endpoint is resolved per tick via effectiveStatuszURL: the
// configured base_url override when set, otherwise the endpoint discovered from
// the watched BN pod. When neither is available the tick is skipped quietly
// (no exec, no error) so a daemon that has not yet observed a BN pod keeps a
// quiet loop and picks the endpoint up once discovery yields one — and follows it
// across pod restarts as the discovered URL changes.
//
// Each tick first runs the unprivileged `--check` digest probe; the privileged
// apply (a root sudo exec) fires only when the desired membership changed, the
// resolved URL changed, or the force-resync interval has elapsed since the last
// apply. A worker-exec failure is returned to superviseResponsibility, which
// retries with exponential back-off; a ctx cancellation returns nil (clean
// shutdown).
func (m *TrafficShaperMonitor) runStatuszPoll(ctx context.Context) error {
	interval := m.pollInterval
	if interval <= 0 {
		interval = defaultStatuszPollInterval
	}

	logx.As().Info().
		Str("reason", "TrafficShaperStatuszPollStarting").
		Str("monitor", m.Name()).
		Str("base_url", m.statuszURL).
		Dur("poll_interval", interval).
		Msg("statusz poll loop starting")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// lastDigest/lastApply/lastURL are local to this invocation: a fault restarts
	// runStatuszPoll (via superviseResponsibility), which resets them and forces
	// a fresh apply — correct, since post-fault we can't trust the prior state.
	var lastDigest string
	var lastApply time.Time
	var lastURL string

	reconcile := func() error {
		statuszURL := m.effectiveStatuszURL()
		if statuszURL != lastURL {
			// Endpoint transition (discovered/lost/changed): log once and reset the
			// digest gate so the next non-empty URL forces a fresh apply rather than
			// trusting a digest computed against the previous endpoint.
			if statuszURL == "" {
				logx.As().Info().
					Str("reason", "TrafficShaperStatuszEndpointLost").
					Str("monitor", m.Name()).
					Msg("no statusz endpoint available — poll loop idle until a BN pod is discovered or base_url is set")
			} else {
				logx.As().Info().
					Str("reason", "TrafficShaperStatuszEndpointResolved").
					Str("monitor", m.Name()).
					Str("statusz_url", statuszURL).
					Msg("statusz endpoint resolved — reconciling")
			}
			lastURL = statuszURL
			lastDigest = ""
			lastApply = time.Time{}
		}
		if statuszURL == "" {
			return nil
		}

		logx.As().Debug().
			Str("reason", "TrafficShaperStatuszPolling").
			Str("monitor", m.Name()).
			Str("statusz_url", statuszURL).
			Msg("polling statusz")

		digest, err := m.delegator.ReconcileShaperCheck(ctx, statuszURL)
		if err != nil {
			return err
		}
		// Skip the root apply only when the desired membership is unchanged AND
		// the force-resync window has not elapsed. lastApply.IsZero() forces the
		// first reconcile against a (new) URL to apply.
		if digest == lastDigest && !lastApply.IsZero() && time.Since(lastApply) < statuszForceResyncInterval {
			logx.As().Debug().
				Str("reason", "TrafficShaperStatuszUnchanged").
				Str("monitor", m.Name()).
				Str("statusz_url", statuszURL).
				Msg("desired membership digest unchanged — skipping apply")
			return nil
		}
		logx.As().Info().
			Str("reason", "TrafficShaperStatuszApplying").
			Str("monitor", m.Name()).
			Str("statusz_url", statuszURL).
			Msg("applying nft policy membership from statusz")
		if err := m.delegator.ReconcileShaper(ctx, statuszURL); err != nil {
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

	// Reconcile once on entry so the daemon converges immediately on startup
	// (including after a host reboot) without waiting a full interval for the
	// first tick. The nft set elements are not boot-persistent; this entry probe
	// is what rehydrates them as soon as the daemon starts and the BN statusz
	// endpoint is reachable. If statusz is not yet up, runReconcile returns an
	// error and superviseResponsibility retries with back-off — convergence is
	// bounded by BN startup time, not the poll interval.
	//
	// Pod-discovery path: when base_url is not configured, effectiveStatuszURL
	// returns "" until the pod watcher observes a ready BN pod, so this entry
	// reconcile is a silent no-op (not an error) on a cold start. Convergence is
	// not deferred to the next tick in that case — the watcher signals urlChanged
	// as soon as it records an endpoint and the select below wakes on it, so both
	// the discovery and base_url paths converge as fast as statusz responds.
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
		case <-m.urlChanged:
			// The pod watcher discovered, changed, or lost the endpoint. Reconcile
			// now rather than waiting out the interval. reconcile() no-ops when the
			// URL is empty (endpoint lost), so a teardown signal costs nothing.
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
