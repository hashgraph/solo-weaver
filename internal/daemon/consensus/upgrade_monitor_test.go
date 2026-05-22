// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

var upgradeExecuteGVR = schema.GroupVersionResource{
	Group:    "hedera.com",
	Version:  "v1alpha1",
	Resource: "networkupgradeexecutes",
}

func newFakeUpgradeMonitor(t *testing.T, namespace string, objects ...runtime.Object) (*consensus.UpgradeMonitor, *fake.FakeDynamicClient) {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	// Register the CR's GVK so the fake client can handle watch events.
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "hedera.com", Version: "v1alpha1", Kind: "NetworkUpgradeExecute"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "hedera.com", Version: "v1alpha1", Kind: "NetworkUpgradeExecuteList"},
		&unstructured.UnstructuredList{},
	)

	client := fake.NewSimpleDynamicClient(scheme, objects...)
	cfg := consensus.UpgradeMonitorConfig{
		KubeconfigPath: "/dev/null", // not used — client is injected
		Namespace:      namespace,
	}
	return consensus.NewUpgradeMonitorWithClient(cfg, client), client
}

func makeExecuteCR(name, namespace, operationID, phase string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "hedera.com/v1alpha1",
			"kind":       "NetworkUpgradeExecute",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"operationId": operationID,
				"orbit":       namespace,
			},
			"status": map[string]interface{}{
				"phase": phase,
			},
		},
	}
}

func Test_UpgradeMonitor_NoEvent_WhenIdle(t *testing.T) {
	const ns = "hedera-network"
	um, _ := newFakeUpgradeMonitor(t, ns)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Run returns nil on clean ctx cancellation — no panics, no errors.
	err := um.Run(ctx)
	assert.NoError(t, err)
}

func Test_UpgradeMonitor_TriggersOnReadyForProvisionerDaemon(t *testing.T) {
	const ns = "hedera-network"
	cr := makeExecuteCR("upgrade-20260522t120000z-execute", ns, "upgrade-20260522T120000-v0.75.0", "ReadyForProvisionerDaemon")

	um, client := newFakeUpgradeMonitor(t, ns)

	var called atomic.Int32
	um.SetOnExecute(func(_ string) { called.Add(1) })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = um.Run(ctx) }()

	// Give the watch loop time to establish.
	time.Sleep(50 * time.Millisecond)

	// Create the CR — fake client emits an Added watch event.
	_, err := client.Resource(upgradeExecuteGVR).Namespace(ns).Create(
		ctx, cr, metav1.CreateOptions{},
	)
	require.NoError(t, err)

	require.Eventually(t, func() bool { return called.Load() == 1 }, 2*time.Second, 10*time.Millisecond,
		"handleExecute should be called exactly once for ReadyForProvisionerDaemon")
	cancel()
}

func Test_UpgradeMonitor_IgnoresNonReadyPhases(t *testing.T) {
	const ns = "hedera-network"
	um, client := newFakeUpgradeMonitor(t, ns)

	var called atomic.Int32
	um.SetOnExecute(func(_ string) { called.Add(1) })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = um.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	for i, phase := range []string{"Pending", "PendingInfraUpgrade", "PendingNodeUpgrade", "Succeeded", "Failed"} {
		cr := makeExecuteCR(fmt.Sprintf("upgrade-execute-%d", i), ns, fmt.Sprintf("op-%d", i), phase)
		_, err := client.Resource(upgradeExecuteGVR).Namespace(ns).Create(ctx, cr, metav1.CreateOptions{})
		require.NoError(t, err)
	}

	// Give the watch loop time to process all events, then assert nothing fired.
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(0), called.Load(), "handleExecute must not be called for non-ReadyForProvisionerDaemon phases")
	cancel()
}

func Test_UpgradeMonitor_DeduplicatesSameOperationID(t *testing.T) {
	const ns = "hedera-network"
	const opID = "upgrade-20260522T120000-v0.75.0"

	cr := makeExecuteCR("upgrade-execute", ns, opID, "ReadyForProvisionerDaemon")
	um, client := newFakeUpgradeMonitor(t, ns)

	var called atomic.Int32
	// Block handleExecute so the dedup window stays open for the second event.
	block := make(chan struct{})
	um.SetOnExecute(func(_ string) {
		called.Add(1)
		<-block
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = um.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	// First event — handleExecute fires and blocks on <-block.
	_, err := client.Resource(upgradeExecuteGVR).Namespace(ns).Create(ctx, cr, metav1.CreateOptions{})
	require.NoError(t, err)

	// Wait until handleExecute is running before sending the duplicate.
	require.Eventually(t, func() bool { return called.Load() == 1 }, 2*time.Second, 10*time.Millisecond)

	// Second event with same operationId — must be deduplicated.
	cr2 := cr.DeepCopy()
	cr2.SetResourceVersion("2")
	_, err = client.Resource(upgradeExecuteGVR).Namespace(ns).Update(ctx, cr2, metav1.UpdateOptions{})
	require.NoError(t, err)

	// Give the watch loop time to process the duplicate before unblocking the first execution,
	// ensuring the dedup check runs while the operationId is still in activeOps.
	time.Sleep(100 * time.Millisecond)

	// Unblock the first execution and assert the duplicate was not dispatched.
	close(block)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), called.Load(), "handleExecute must be called exactly once for a duplicate operationId")
	cancel()
}

func Test_UpgradeMonitor_RejectsDifferentOperationWhileBusy(t *testing.T) {
	// Verifies the single-slot invariant: only one handleExecute runs at a time.
	// A different operationId arriving while op-A is active must be rejected —
	// concurrent upgrades on the same node would corrupt InfraConfig files and K8s CRs.
	const ns = "hedera-network"
	const opA = "upgrade-op-a"
	const opB = "upgrade-op-b"

	crA := makeExecuteCR("upgrade-execute-a", ns, opA, "ReadyForProvisionerDaemon")
	crB := makeExecuteCR("upgrade-execute-b", ns, opB, "ReadyForProvisionerDaemon")

	um, client := newFakeUpgradeMonitor(t, ns)

	var calls atomic.Int32
	blockA := make(chan struct{})
	um.SetOnExecute(func(id string) {
		calls.Add(1)
		if id == opA {
			<-blockA
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = um.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	// Trigger op-A — it will block inside handleExecute.
	_, err := client.Resource(upgradeExecuteGVR).Namespace(ns).Create(ctx, crA, metav1.CreateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return calls.Load() == 1 }, 2*time.Second, 10*time.Millisecond)

	// Trigger op-B while op-A is still running — must be rejected (single execution slot).
	_, err = client.Resource(upgradeExecuteGVR).Namespace(ns).Create(ctx, crB, metav1.CreateOptions{})
	require.NoError(t, err)

	// Give the watch loop time to process the op-B event before unblocking op-A.
	time.Sleep(100 * time.Millisecond)

	// Unblock op-A and assert op-B was never dispatched.
	close(blockA)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), calls.Load(), "op-B must be rejected while op-A is still running")
	cancel()
}

func Test_isAuthError_DetectsUnauthorizedAndForbidden(t *testing.T) {
	unauthorized := k8serrors.NewUnauthorized("token expired")
	forbidden := k8serrors.NewForbidden(
		schema.GroupResource{Group: "hedera.com", Resource: "networkupgradeexecutes"},
		"upgrade-execute",
		fmt.Errorf("denied"),
	)
	unrelated := fmt.Errorf("some other error")

	// Plain typed errors — direct path.
	assert.True(t, consensus.IsAuthError(unauthorized), "plain 401 should be detected")
	assert.True(t, consensus.IsAuthError(forbidden), "plain 403 should be detected")
	assert.False(t, consensus.IsAuthError(unrelated), "unrelated error must not match")
	assert.False(t, consensus.IsAuthError(nil), "nil must not match")

	// Wrapped through ErrWatchFailed — the watch.Error event path.
	assert.True(t, consensus.IsAuthError(consensus.ErrWatchFailed.Wrap(unauthorized, "watch error event")), "wrapped 401 should be detected")
	assert.True(t, consensus.IsAuthError(consensus.ErrWatchFailed.Wrap(forbidden, "watch error event")), "wrapped 403 should be detected")
	assert.False(t, consensus.IsAuthError(consensus.ErrWatchFailed.Wrap(unrelated, "watch error event")), "wrapped unrelated error must not match")
}
