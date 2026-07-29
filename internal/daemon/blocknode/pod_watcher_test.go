// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// fakeResolver returns queued (veth, err) results in order, repeating the last
// one once exhausted, so tests can model transient not-found → success retries.
type fakeResolver struct {
	results []resolveResult
	calls   int
}

type resolveResult struct {
	veth string
	err  error
}

func (f *fakeResolver) Resolve(_ context.Context, _ *corev1.Pod) (string, error) {
	i := f.calls
	if i >= len(f.results) {
		i = len(f.results) - 1
	}
	f.calls++
	r := f.results[i]
	return r.veth, r.err
}

// fakeDelegator records TCAttach/TCDetach invocations; the poll-loop methods are
// unused here.
type fakeDelegator struct {
	attached  []string
	detached  []string
	attachErr error
}

func (f *fakeDelegator) Run(context.Context, ...string) ([]byte, error) { return nil, nil }
func (f *fakeDelegator) NetworkPolicySet(context.Context, string, []string) error {
	return nil
}
func (f *fakeDelegator) TCAttach(_ context.Context, veth string) error {
	f.attached = append(f.attached, veth)
	return f.attachErr
}
func (f *fakeDelegator) TCDetach(_ context.Context, veth string) error {
	f.detached = append(f.detached, veth)
	return nil
}
func (f *fakeDelegator) ReconcileShaper(context.Context, string) error { return nil }
func (f *fakeDelegator) ReconcileShaperCheck(context.Context, string) (string, error) {
	return "", nil
}

func newTestMonitor(r vethResolver, d *fakeDelegator) *TrafficShaperMonitor {
	return &TrafficShaperMonitor{
		resolver:  r,
		delegator: d,
		attached:  make(map[types.UID]string),
		inflight:  make(map[types.UID]bool),
	}
}

func readyPod(uid, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID(uid), Name: name, Namespace: "bn"},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
			},
		},
	}
}

func notReadyPod(uid, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID(uid), Name: name, Namespace: "bn"},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.ContainersReady, Status: corev1.ConditionFalse},
			},
		},
	}
}

func shrinkResolveRetries(t *testing.T) {
	t.Helper()
	origAttempts, origInterval := vethResolveAttempts, vethResolveInterval
	vethResolveAttempts = 3
	vethResolveInterval = time.Millisecond
	t.Cleanup(func() {
		vethResolveAttempts = origAttempts
		vethResolveInterval = origInterval
	})
}

func TestHandlePodUpsert_NotReady_NoAttach(t *testing.T) {
	d := &fakeDelegator{}
	m := newTestMonitor(&fakeResolver{results: []resolveResult{{veth: "lxc1"}}}, d)

	m.handlePodUpsert(context.Background(), notReadyPod("u1", "bn-0"))
	require.Empty(t, d.attached, "must not attach a pod that is not ContainersReady")
}

func TestHandlePodUpsert_Ready_AttachesResolvedVeth(t *testing.T) {
	d := &fakeDelegator{}
	m := newTestMonitor(&fakeResolver{results: []resolveResult{{veth: "lxcAAA"}}}, d)

	m.handlePodUpsert(context.Background(), readyPod("u1", "bn-0"))
	require.Equal(t, []string{"lxcAAA"}, d.attached)
	require.Equal(t, "lxcAAA", m.attached["u1"])
}

func TestHandlePodUpsert_DedupesSameVeth(t *testing.T) {
	d := &fakeDelegator{}
	m := newTestMonitor(&fakeResolver{results: []resolveResult{{veth: "lxcAAA"}}}, d)

	pod := readyPod("u1", "bn-0")
	m.handlePodUpsert(context.Background(), pod)
	m.handlePodUpsert(context.Background(), pod)
	require.Equal(t, []string{"lxcAAA"}, d.attached, "second identical upsert must not re-attach")
}

func TestHandlePodUpsert_RetriesUntilVethVisible(t *testing.T) {
	shrinkResolveRetries(t)
	d := &fakeDelegator{}
	r := &fakeResolver{results: []resolveResult{
		{err: ErrVethNotFound},
		{veth: "lxcAAA"},
	}}
	m := newTestMonitor(r, d)

	m.handlePodUpsert(context.Background(), readyPod("u1", "bn-0"))
	require.Equal(t, []string{"lxcAAA"}, d.attached)
	require.GreaterOrEqual(t, r.calls, 2, "should retry after ErrVethNotFound")
}

func TestHandlePodUpsert_HardResolveErrorDoesNotRetryOrAttach(t *testing.T) {
	shrinkResolveRetries(t)
	d := &fakeDelegator{}
	// A genuinely non-retryable error (not ErrVethNotFound / ErrVethNotReady).
	// Note RBAC is deliberately NOT used here: the resolver wraps RBAC/exec
	// failures as ErrVethNotReady, which is retryable.
	r := &fakeResolver{results: []resolveResult{{err: errors.New("malformed iflink output")}}}
	m := newTestMonitor(r, d)

	m.handlePodUpsert(context.Background(), readyPod("u1", "bn-0"))
	require.Empty(t, d.attached)
	require.Equal(t, 1, r.calls, "a non-retryable error must not be retried")
}

func TestHandlePodUpsert_AttachFailureNotRecorded(t *testing.T) {
	d := &fakeDelegator{attachErr: errors.New("tc: operation not permitted")}
	m := newTestMonitor(&fakeResolver{results: []resolveResult{{veth: "lxcAAA"}}}, d)

	m.handlePodUpsert(context.Background(), readyPod("u1", "bn-0"))
	require.Equal(t, []string{"lxcAAA"}, d.attached, "attach was attempted")
	_, tracked := m.attached["u1"]
	require.False(t, tracked, "a failed attach must not be recorded, so a later event retries it")
}

func TestHandlePodDelete_DetachesRecordedVeth(t *testing.T) {
	d := &fakeDelegator{}
	m := newTestMonitor(&fakeResolver{results: []resolveResult{{veth: "lxcAAA"}}}, d)

	pod := readyPod("u1", "bn-0")
	m.handlePodUpsert(context.Background(), pod)
	m.handlePodDelete(context.Background(), pod)

	require.Equal(t, []string{"lxcAAA"}, d.detached)
	_, stillTracked := m.attached["u1"]
	require.False(t, stillTracked, "deleted pod must be dropped from the attached map")
}

func TestHandlePodDelete_UnknownPodIsNoOp(t *testing.T) {
	d := &fakeDelegator{}
	m := newTestMonitor(&fakeResolver{results: []resolveResult{{veth: "lxcAAA"}}}, d)

	m.handlePodDelete(context.Background(), readyPod("never-attached", "bn-9"))
	require.Empty(t, d.detached, "deleting an untracked pod must not call detach")
}

// blockingResolver blocks in Resolve until gate is closed, so a test can hold a
// dispatch goroutine "in flight" and assert the guard.
type blockingResolver struct {
	veth string
	gate chan struct{}
}

func (b *blockingResolver) Resolve(ctx context.Context, _ *corev1.Pod) (string, error) {
	select {
	case <-b.gate:
		return b.veth, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestDispatchUpsert_InFlightGuardAndAsyncAttach(t *testing.T) {
	release := make(chan struct{})
	d := &fakeDelegator{}
	m := newTestMonitor(&blockingResolver{veth: "lxcAAA", gate: release}, d)
	pod := readyPod("u1", "bn-0")

	m.dispatchUpsert(context.Background(), pod)

	// The first goroutine is blocked inside Resolve; the pod is marked in-flight.
	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.inflight["u1"]
	}, time.Second, time.Millisecond, "pod should be marked in-flight while resolving")

	// A second dispatch while the first is in-flight must not launch another attach.
	m.dispatchUpsert(context.Background(), pod)

	close(release) // let the single in-flight goroutine finish

	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return len(m.inflight) == 0
	}, time.Second, time.Millisecond, "in-flight entry should clear after attach completes")

	require.Equal(t, []string{"lxcAAA"}, d.attached, "exactly one attach despite two dispatches")
}

func TestPodContainersReady(t *testing.T) {
	require.True(t, podContainersReady(readyPod("u1", "bn-0")))
	require.False(t, podContainersReady(notReadyPod("u1", "bn-0")))
	require.False(t, podContainersReady(&corev1.Pod{}), "no condition → not ready")
	require.False(t, podContainersReady(nil))
}

// readyPodWithNet is a ContainersReady pod carrying a PodIP and the given
// containerPorts, for statusz-discovery tests.
func readyPodWithNet(uid, name, ip string, ports ...corev1.ContainerPort) *corev1.Pod {
	pod := readyPod(uid, name)
	pod.Status.PodIP = ip
	pod.Spec.Containers = []corev1.Container{{Name: "block-node", Ports: ports}}
	return pod
}

func TestBNHealthContainerPort(t *testing.T) {
	withHealth := readyPodWithNet("u1", "bn-0", "10.0.0.1",
		corev1.ContainerPort{Name: "metrics", ContainerPort: 16007},
		corev1.ContainerPort{Name: bnHealthPortName, ContainerPort: 40983})
	require.Equal(t, "40983", bnHealthContainerPort(withHealth), "uses the health-named port")

	noHealth := readyPodWithNet("u2", "bn-1", "10.0.0.2",
		corev1.ContainerPort{Name: "metrics", ContainerPort: 16007})
	require.Equal(t, defaultBNHealthPort, bnHealthContainerPort(noHealth),
		"falls back to the default when no health-named port is present")
}

func TestRecordDiscoveredStatusz_UsesHealthContainerPort(t *testing.T) {
	m := newTestMonitor(&fakeResolver{results: []resolveResult{{veth: "lxc1"}}}, &fakeDelegator{})
	pod := readyPodWithNet("u1", "bn-0", "10.1.2.3",
		corev1.ContainerPort{Name: "grpc", ContainerPort: 40840},
		corev1.ContainerPort{Name: bnHealthPortName, ContainerPort: 40983})

	m.recordDiscoveredStatusz(pod)
	require.Equal(t, "http://10.1.2.3:40983", m.discoveredStatuszURL)
	require.Equal(t, types.UID("u1"), m.discoveredStatuszPod)
}

func TestRecordDiscoveredStatusz_FallsBackToDefaultPort(t *testing.T) {
	m := newTestMonitor(&fakeResolver{results: []resolveResult{{veth: "lxc1"}}}, &fakeDelegator{})
	pod := readyPodWithNet("u1", "bn-0", "10.1.2.3",
		corev1.ContainerPort{Name: "grpc", ContainerPort: 40840})

	m.recordDiscoveredStatusz(pod)
	require.Equal(t, "http://10.1.2.3:"+defaultBNHealthPort, m.discoveredStatuszURL)
}

func TestRecordDiscoveredStatusz_NoIPIsNoOp(t *testing.T) {
	m := newTestMonitor(&fakeResolver{results: []resolveResult{{veth: "lxc1"}}}, &fakeDelegator{})
	pod := readyPod("u1", "bn-0") // ready but no PodIP yet

	m.recordDiscoveredStatusz(pod)
	require.Empty(t, m.discoveredStatuszURL, "no endpoint recorded until the pod has an IP")
}

func TestHandlePodDelete_ClearsDiscoveredStatuszForOwningPod(t *testing.T) {
	m := newTestMonitor(&fakeResolver{results: []resolveResult{{veth: "lxc1"}}}, &fakeDelegator{})
	pod := readyPodWithNet("u1", "bn-0", "10.1.2.3",
		corev1.ContainerPort{Name: bnHealthPortName, ContainerPort: 40983})

	m.recordDiscoveredStatusz(pod)
	require.NotEmpty(t, m.discoveredStatuszURL)

	m.handlePodDelete(context.Background(), pod)
	require.Empty(t, m.discoveredStatuszURL, "owning pod delete clears the discovered endpoint")
	require.Equal(t, types.UID(""), m.discoveredStatuszPod)
}

func TestHandlePodDelete_KeepsDiscoveredStatuszForOtherPod(t *testing.T) {
	m := newTestMonitor(&fakeResolver{results: []resolveResult{{veth: "lxc1"}}}, &fakeDelegator{})
	pod := readyPodWithNet("u1", "bn-0", "10.1.2.3",
		corev1.ContainerPort{Name: bnHealthPortName, ContainerPort: 40983})
	m.recordDiscoveredStatusz(pod)

	m.handlePodDelete(context.Background(), readyPod("u2", "bn-1"))
	require.Equal(t, "http://10.1.2.3:40983", m.discoveredStatuszURL,
		"an unrelated pod delete must not clear a still-valid endpoint")
}

// TestDispatchUpsert_RecordsDiscoveryWhileAttachInFlight covers the case where a
// pod is ContainersReady before its IP is populated: the first event launches a
// veth attach that blocks (so the pod stays in-flight), and the follow-up event
// that first carries the IP would be dropped by the in-flight guard. Discovery
// runs ahead of that guard, so the endpoint is still recorded rather than missed.
func TestDispatchUpsert_RecordsDiscoveryWhileAttachInFlight(t *testing.T) {
	release := make(chan struct{})
	d := &fakeDelegator{}
	m := newTestMonitor(&blockingResolver{veth: "lxcAAA", gate: release}, d)

	// First event: ready but no IP yet → veth goroutine blocks (in-flight), and
	// discovery no-ops because there is no IP.
	m.dispatchUpsert(context.Background(), readyPod("u1", "bn-0"))
	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.inflight["u1"]
	}, time.Second, time.Millisecond, "pod should be in-flight while veth resolves")
	m.mu.Lock()
	require.Empty(t, m.discoveredStatuszURL, "no endpoint until the pod has an IP")
	m.mu.Unlock()

	// Second event for the same pod now carries the IP. It is dropped for veth
	// (still in-flight), but discovery must still record the endpoint.
	m.dispatchUpsert(context.Background(), readyPodWithNet("u1", "bn-0", "10.1.2.3",
		corev1.ContainerPort{Name: bnHealthPortName, ContainerPort: 40983}))

	m.mu.Lock()
	got := m.discoveredStatuszURL
	m.mu.Unlock()
	require.Equal(t, "http://10.1.2.3:40983", got,
		"discovery must be recorded even while a veth attach is in-flight")

	// Let the blocked goroutine finish so it does not outlive the test.
	close(release)
	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return len(m.inflight) == 0
	}, time.Second, time.Millisecond)
}
