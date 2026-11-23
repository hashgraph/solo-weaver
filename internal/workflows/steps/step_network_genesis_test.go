// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
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

// fqdnCapsules lists capsules for node ids 0..n-1 with the operator's ref names.
func fqdnCapsules(n int) *unstructured.UnstructuredList {
	list := &unstructured.UnstructuredList{}
	for i := range n {
		list.Items = append(list.Items, unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": fmt.Sprintf("orbit-consensus-%d", i)},
			"spec": map[string]any{
				"nodeId":                   int64(i),
				"applicationPropertiesRef": fmt.Sprintf("node%d-application-properties", i),
			},
		}})
	}
	return list
}

func runCheckGossipFqdn(t *testing.T, fake *fakeCapsuleClient) *automa.Report {
	t.Helper()
	step, err := CheckGossipFqdnAllowed("ns", fake.provider()).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func TestCheckGossipFqdnAllowed_AllFalse(t *testing.T) {
	fake := &fakeCapsuleClient{listResult: fqdnCapsules(2), existing: map[string]string{
		"node0-application-properties": "netty.mode=DEV\nnodes.gossipFqdnRestricted=false\n",
		"node1-application-properties": "nodes.gossipFqdnRestricted = FALSE\n",
	}}
	rpt := runCheckGossipFqdn(t, fake)
	require.Equal(t, automa.StatusSuccess, rpt.Status)
}

// A deployment package file without the key leaves the network default (true).
func TestCheckGossipFqdnAllowed_UnsetFails(t *testing.T) {
	fake := &fakeCapsuleClient{listResult: fqdnCapsules(2), existing: map[string]string{
		"node0-application-properties": "nodes.gossipFqdnRestricted=false\n",
		"node1-application-properties": "netty.mode=DEV\n",
	}}
	rpt := runCheckGossipFqdn(t, fake)
	require.Equal(t, automa.StatusFailed, rpt.Status)
	require.ErrorContains(t, rpt.Error, "orbit-consensus-1 (unset, network default true)")
	require.NotContains(t, rpt.Error.Error(), "orbit-consensus-0")
}

func TestCheckGossipFqdnAllowed_TrueFails(t *testing.T) {
	fake := &fakeCapsuleClient{listResult: fqdnCapsules(1), existing: map[string]string{
		"node0-application-properties": "nodes.gossipFqdnRestricted=true\n",
	}}
	rpt := runCheckGossipFqdn(t, fake)
	require.Equal(t, automa.StatusFailed, rpt.Status)
	require.ErrorContains(t, rpt.Error, "orbit-consensus-0 (true)")
}

// A capsule without applicationPropertiesRef falls back to the name from spec.nodeId.
func TestCheckGossipFqdnAllowed_RefFallbackFromNodeID(t *testing.T) {
	list := fqdnCapsules(1)
	unstructured.RemoveNestedField(list.Items[0].Object, "spec", "applicationPropertiesRef")
	fake := &fakeCapsuleClient{listResult: list, existing: map[string]string{
		"node0-application-properties": "nodes.gossipFqdnRestricted=false\n",
	}}
	rpt := runCheckGossipFqdn(t, fake)
	require.Equal(t, automa.StatusSuccess, rpt.Status)
}

// A capsule with an IP internalAddress gets an IP gossip endpoint, so it is not checked.
func TestCheckGossipFqdnAllowed_SkipsIPInternalAddress(t *testing.T) {
	list := fqdnCapsules(2)
	require.NoError(t, unstructured.SetNestedField(list.Items[1].Object, "10.0.0.7", "spec", "internalAddress"))
	fake := &fakeCapsuleClient{listResult: list, existing: map[string]string{
		"node0-application-properties": "nodes.gossipFqdnRestricted=false\n",
		"node1-application-properties": "netty.mode=DEV\n",
	}}
	rpt := runCheckGossipFqdn(t, fake)
	require.Equal(t, automa.StatusSuccess, rpt.Status)
}

// A DNS internalAddress is still a FQDN gossip endpoint.
func TestCheckGossipFqdnAllowed_DNSInternalAddressChecked(t *testing.T) {
	list := fqdnCapsules(1)
	require.NoError(t, unstructured.SetNestedField(list.Items[0].Object, "node0.example.com", "spec", "internalAddress"))
	fake := &fakeCapsuleClient{listResult: list, existing: map[string]string{
		"node0-application-properties": "netty.mode=DEV\n",
	}}
	rpt := runCheckGossipFqdn(t, fake)
	require.Equal(t, automa.StatusFailed, rpt.Status)
}

func TestPropertyValue(t *testing.T) {
	const key = "nodes.gossipFqdnRestricted"
	cases := []struct {
		name, content, want string
		found               bool
	}{
		{"equals", "nodes.gossipFqdnRestricted=false", "false", true},
		{"spaces around equals", "nodes.gossipFqdnRestricted = false", "false", true},
		{"colon", "nodes.gossipFqdnRestricted:false", "false", true},
		{"whitespace separator", "nodes.gossipFqdnRestricted false", "false", true},
		{"last value wins", "nodes.gossipFqdnRestricted=false\nnodes.gossipFqdnRestricted=true", "true", true},
		{"comment ignored", "#nodes.gossipFqdnRestricted=false\n! nodes.gossipFqdnRestricted=false", "", false},
		{"key prefix is not a match", "nodes.gossipFqdnRestrictedX=false", "", false},
		{"CRLF", "nodes.gossipFqdnRestricted=false\r\n", "false", true},
		{"missing", "netty.mode=DEV", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := propertyValue(tc.content, key)
			require.Equal(t, tc.found, found)
			require.Equal(t, tc.want, got)
		})
	}
}
