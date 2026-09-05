// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// capsuleList builds an UnstructuredList of ConsensusCapsules with the given
// name->phase mapping so the readiness step can be driven without a cluster.
// startPolicy is left unset (operator default Auto).
func capsuleList(phases map[string]string) *unstructured.UnstructuredList {
	list := &unstructured.UnstructuredList{}
	for name, phase := range phases {
		item := unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": name},
			"status":   map[string]any{"phase": phase},
		}}
		list.Items = append(list.Items, item)
	}
	return list
}

// capsuleListWithPolicy is capsuleList but sets spec.startPolicy per node.
func capsuleListWithPolicy(nodes map[string][2]string) *unstructured.UnstructuredList {
	list := &unstructured.UnstructuredList{}
	for name, pp := range nodes {
		phase, policy := pp[0], pp[1]
		item := unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": name},
			"spec":     map[string]any{"startPolicy": policy},
			"status":   map[string]any{"phase": phase},
		}}
		list.Items = append(list.Items, item)
	}
	return list
}

func runNetworkReady(t *testing.T, fake *fakeCapsuleClient, timeout time.Duration) *automa.Report {
	t.Helper()
	b := WaitConsensusNetworkReady("ns", fake.provider(), timeout)
	step, err := b.Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func TestWaitConsensusNetworkReady_AllActive(t *testing.T) {
	fake := &fakeCapsuleClient{listResult: capsuleList(map[string]string{
		"orbit-consensus-0": "Active",
		"orbit-consensus-1": "Active",
	})}
	rpt := runNetworkReady(t, fake, time.Second)
	require.Equal(t, automa.StatusSuccess, rpt.Status)
}

// Running is transient (pod up, not yet platform-ACTIVE) and must NOT count as
// ready — otherwise the wait falsely succeeds while the main container is still
// pulling. With a short timeout the all-Running case fails.
func TestWaitConsensusNetworkReady_RunningIsNotReady(t *testing.T) {
	fake := &fakeCapsuleClient{listResult: capsuleList(map[string]string{
		"orbit-consensus-0": "Running",
	})}
	rpt := runNetworkReady(t, fake, 50*time.Millisecond)
	require.Equal(t, automa.StatusFailed, rpt.Status)
}

func TestWaitConsensusNetworkReady_FailsFastOnFailed(t *testing.T) {
	fake := &fakeCapsuleClient{listResult: capsuleList(map[string]string{
		"orbit-consensus-0": "Running",
		"orbit-consensus-1": "Failed",
	})}
	rpt := runNetworkReady(t, fake, 5*time.Second)
	require.Equal(t, automa.StatusFailed, rpt.Status)
}

func TestWaitConsensusNetworkReady_TimesOutWhilePending(t *testing.T) {
	fake := &fakeCapsuleClient{listResult: capsuleList(map[string]string{
		"orbit-consensus-0": "Pending",
	})}
	rpt := runNetworkReady(t, fake, 50*time.Millisecond)
	require.Equal(t, automa.StatusFailed, rpt.Status)
}

// A Manual-start node resting in Stopped must not be waited on: with the sole
// Auto node Running, readiness succeeds despite the Manual node being down.
func TestWaitConsensusNetworkReady_SkipsManualNodes(t *testing.T) {
	fake := &fakeCapsuleClient{listResult: capsuleListWithPolicy(map[string][2]string{
		"orbit-consensus-0": {"Active", "Auto"},
		"orbit-consensus-1": {"Stopped", "Manual"},
	})}
	rpt := runNetworkReady(t, fake, time.Second)
	require.Equal(t, automa.StatusSuccess, rpt.Status)
}

// When every node is Manual there is nothing to auto-start, so the wait settles
// immediately even though no node is Running.
func TestWaitConsensusNetworkReady_AllManualSettlesImmediately(t *testing.T) {
	fake := &fakeCapsuleClient{listResult: capsuleListWithPolicy(map[string][2]string{
		"orbit-consensus-0": {"Stopped", "Manual"},
		"orbit-consensus-1": {"Stopped", "Manual"},
	})}
	rpt := runNetworkReady(t, fake, time.Second)
	require.Equal(t, automa.StatusSuccess, rpt.Status)
}
