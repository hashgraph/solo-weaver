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
