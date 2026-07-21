// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"errors"
	"time"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// bnPodLabelSelector selects the block-node server pods the traffic-shaper
// watches. Mirrors internal/blocknode.PodLabelSelector; duplicated (not
// imported) to keep the daemon's import closure free of the provisioning
// packages (helm/mount/etc.), which it deliberately never pulls in.
const bnPodLabelSelector = "app.kubernetes.io/name=block-node-server"

// vethResolver is the subset of *VethResolver the pod watcher needs, extracted
// so the watcher can be unit-tested with a fake.
type vethResolver interface {
	Resolve(ctx context.Context, pod *corev1.Pod) (string, error)
}

// Veth-resolution retry bounds for a pod that is ContainersReady but whose
// host-side veth is not yet visible (Cilium is still wiring the veth pair).
// Vars, not consts, so tests can shrink them to avoid real-time waits.
var (
	vethResolveAttempts = 15
	vethResolveInterval = 2 * time.Second
)

// runPodWatcher is the pod-lifecycle watcher responsibility (design §8.1.1). It
// lists the current BN pods, installs the $VETH ingress HTB on any that are
// already ContainersReady, then watches for create/update/delete events and
// (re)installs or tears down the hierarchy as pods come and go.
//
// A watch-channel close or error returns an error so the supervisor reconnects
// with back-off; the initial re-list on reconnect re-attaches idempotently
// (tc-attach tears down and rebuilds), so no state is lost across reconnects.
func (m *TrafficShaperMonitor) runPodWatcher(ctx context.Context) error {
	if m.client == nil {
		// Unconfigured (unit-test scaffolding only — NewComponent always wires a
		// client in production). Nothing to watch; block until shutdown.
		logx.As().Warn().
			Str("reason", "TrafficShaperPodWatcherNoClient").
			Str("monitor", m.Name()).
			Msg("pod-lifecycle watcher has no kube client — idle")
		<-ctx.Done()
		return nil
	}

	pods := m.client.CoreV1().Pods(m.namespace)
	list, err := pods.List(ctx, metav1.ListOptions{LabelSelector: bnPodLabelSelector})
	if err != nil {
		return errorx.ExternalError.Wrap(err, "list block-node pods in namespace %s", m.namespace)
	}
	for i := range list.Items {
		m.dispatchUpsert(ctx, &list.Items[i])
	}

	watcher, err := pods.Watch(ctx, metav1.ListOptions{
		LabelSelector:   bnPodLabelSelector,
		ResourceVersion: list.ResourceVersion,
	})
	if err != nil {
		return errorx.ExternalError.Wrap(err, "watch block-node pods in namespace %s", m.namespace)
	}
	defer watcher.Stop()

	logx.As().Info().
		Str("reason", "TrafficShaperPodWatcherStarted").
		Str("monitor", m.Name()).
		Str("namespace", m.namespace).
		Msg("block-node pod-lifecycle watcher started")

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-watcher.ResultChan():
			if !ok {
				// Channel closed (idle timeout / apiserver hiccup): fault so the
				// supervisor reconnects and re-lists.
				return errorx.ExternalError.New("block-node pod watch channel closed")
			}
			if err := m.handleWatchEvent(ctx, ev); err != nil {
				return err
			}
		}
	}
}

// handleWatchEvent dispatches a single watch event to the upsert/delete handlers.
// It returns an error only for a watch-level Error event, which the caller turns
// into a reconnect.
func (m *TrafficShaperMonitor) handleWatchEvent(ctx context.Context, ev watch.Event) error {
	if ev.Type == watch.Error {
		return errorx.ExternalError.New("block-node pod watch error event: %v", ev.Object)
	}
	pod, ok := ev.Object.(*corev1.Pod)
	if !ok {
		// Bookmark or an unexpected object — nothing to do.
		return nil
	}
	switch ev.Type {
	case watch.Added, watch.Modified:
		m.dispatchUpsert(ctx, pod)
	case watch.Deleted:
		m.handlePodDelete(ctx, pod)
	}
	return nil
}

// dispatchUpsert runs handlePodUpsert in a goroutine so a pod whose veth is slow
// to resolve (resolveVethWithRetry can retry for tens of seconds) never blocks
// the watch loop from draining subsequent pod events. An in-flight guard keyed by
// pod UID ensures at most one attach runs per pod at a time; repeated Added/
// Modified events for a pod already being attached are dropped (the eventual
// handlePodUpsert re-reads the pod state on the next event anyway). Delete
// handling stays synchronous — it is a quick map lookup plus a best-effort exec.
func (m *TrafficShaperMonitor) dispatchUpsert(ctx context.Context, pod *corev1.Pod) {
	uid := pod.UID
	m.mu.Lock()
	if m.inflight[uid] {
		m.mu.Unlock()
		return
	}
	m.inflight[uid] = true
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.inflight, uid)
			m.mu.Unlock()
		}()
		m.handlePodUpsert(ctx, pod)
	}()
}

// handlePodUpsert installs the $VETH ingress HTB on a pod that has reached
// ContainersReady. It is idempotent and deduped: a pod whose veth is already
// installed is skipped, and tc-attach itself rebuilds the hierarchy cleanly, so
// repeated Modified events are cheap. Pods that are not yet ready are ignored;
// a later Modified event (fired when they become ready) picks them up.
func (m *TrafficShaperMonitor) handlePodUpsert(ctx context.Context, pod *corev1.Pod) {
	if !podContainersReady(pod) {
		return
	}

	veth, err := m.resolveVethWithRetry(ctx, pod)
	if err != nil {
		logx.As().Warn().Err(err).
			Str("reason", "TrafficShaperVethResolveFailed").
			Str("pod", pod.Namespace+"/"+pod.Name).
			Msg("could not resolve host-side veth for BN pod — leaving ingress unprioritised until next event")
		return
	}

	m.mu.Lock()
	already := m.attached[pod.UID] == veth
	m.mu.Unlock()
	if already {
		return
	}

	if err := m.delegator.TCAttach(ctx, veth); err != nil {
		logx.As().Warn().Err(err).
			Str("reason", "TrafficShaperTCAttachFailed").
			Str("pod", pod.Namespace+"/"+pod.Name).
			Str("veth", veth).
			Msg("failed to install $VETH ingress HTB")
		return
	}

	m.mu.Lock()
	m.attached[pod.UID] = veth
	m.mu.Unlock()

	logx.As().Info().
		Str("reason", "TrafficShaperVethAttached").
		Str("pod", pod.Namespace+"/"+pod.Name).
		Str("veth", veth).
		Msg("installed $VETH ingress HTB on BN pod")
}

// handlePodDelete tears down the $VETH ingress HTB for a deleted pod. It is
// best-effort and mostly observational: the kernel auto-removes veth-attached
// qdiscs when the veth disappears, so this only fires the detach for the veth we
// recorded at attach time (a deleted pod can no longer be exec'd to re-resolve).
func (m *TrafficShaperMonitor) handlePodDelete(ctx context.Context, pod *corev1.Pod) {
	m.mu.Lock()
	veth, ok := m.attached[pod.UID]
	delete(m.attached, pod.UID)
	m.mu.Unlock()
	if !ok {
		return
	}

	if err := m.delegator.TCDetach(ctx, veth); err != nil {
		logx.As().Warn().Err(err).
			Str("reason", "TrafficShaperTCDetachFailed").
			Str("pod", pod.Namespace+"/"+pod.Name).
			Str("veth", veth).
			Msg("best-effort $VETH ingress HTB teardown failed (kernel removes it with the veth)")
		return
	}
	logx.As().Info().
		Str("reason", "TrafficShaperVethDetached").
		Str("pod", pod.Namespace+"/"+pod.Name).
		Str("veth", veth).
		Msg("tore down $VETH ingress HTB for deleted BN pod")
}

// resolveVethWithRetry resolves the pod's host-side veth, retrying while the
// veth is not yet visible or the container is not yet exec-capable. It stops on
// success, on a non-retryable error, or on ctx cancellation.
func (m *TrafficShaperMonitor) resolveVethWithRetry(ctx context.Context, pod *corev1.Pod) (string, error) {
	var lastErr error
	for attempt := 0; attempt < vethResolveAttempts; attempt++ {
		veth, err := m.resolver.Resolve(ctx, pod)
		if err == nil {
			return veth, nil
		}
		if !errors.Is(err, ErrVethNotFound) && !errors.Is(err, ErrVethNotReady) {
			// A genuinely non-retryable error — e.g. a malformed iflink value or
			// an unreadable /sys/class/net. Note the resolver wraps exec failures
			// (including RBAC) as ErrVethNotReady, so those are retried above, not
			// here.
			return "", err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(vethResolveInterval):
		}
	}
	return "", lastErr
}

// podContainersReady reports whether the pod's ContainersReady condition is
// True. Binding on ContainersReady (rather than PodReady) shrinks the window
// during which ingress is unprioritised after a pod (re)start.
func podContainersReady(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.ContainersReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// ensure *VethResolver satisfies the vethResolver seam.
var _ vethResolver = (*VethResolver)(nil)
