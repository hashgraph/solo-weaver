// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus_test

import (
	"context"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon/consensus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// newNoPodRestartsWithClient returns a NoPodRestarts that uses the provided
// fake client. Used only in tests via the exported BuildTypedClientFn override.
func buildNoPodRestartsReq(cutover time.Time) consensus.SoakStartRequest {
	return consensus.SoakStartRequest{
		NodeID:            "0.0.3",
		CutoverTimestamp:  cutover,
		MigrationPlanPath: "/opt/solo/weaver/migration/consensus/plan.yaml",
	}
}

// Test_NoPodRestarts_NoPodsYet verifies that when no pods exist after cutover
// the criterion returns (false, nil) — pod not yet started, not an error.
func Test_NoPodRestarts_NoPodsYet(t *testing.T) {
	cutover := time.Now().Add(-1 * time.Hour)
	clientset := fake.NewSimpleClientset()

	c := consensus.NewNoPodRestartsWithClient(clientset, "test-ns", "app=cn")
	ok, err := c.Check(context.Background(), buildNoPodRestartsReq(cutover))

	require.NoError(t, err)
	assert.False(t, ok, "expected not-green when no post-cutover pods exist")
}

// Test_NoPodRestarts_GreenWhenNoRestarts verifies green when a post-cutover
// pod exists and all containers have zero restarts.
func Test_NoPodRestarts_GreenWhenNoRestarts(t *testing.T) {
	cutover := time.Now().Add(-1 * time.Hour)
	clientset := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cn-0",
			Namespace:         "test-ns",
			Labels:            map[string]string{"app": "cn"},
			CreationTimestamp: metav1.NewTime(cutover.Add(1 * time.Minute)),
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "consensus-node", RestartCount: 0},
			},
		},
	})

	c := consensus.NewNoPodRestartsWithClient(clientset, "test-ns", "app=cn")
	ok, err := c.Check(context.Background(), buildNoPodRestartsReq(cutover))

	require.NoError(t, err)
	assert.True(t, ok, "expected green when post-cutover pod has zero restarts")
}

// Test_NoPodRestarts_NotGreenWhenRestart verifies not-green (no error) when
// a post-cutover pod has container restarts, and that TotalRestarts reflects the count.
func Test_NoPodRestarts_NotGreenWhenRestart(t *testing.T) {
	cutover := time.Now().Add(-1 * time.Hour)
	clientset := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cn-0",
			Namespace:         "test-ns",
			Labels:            map[string]string{"app": "cn"},
			CreationTimestamp: metav1.NewTime(cutover.Add(1 * time.Minute)),
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "consensus-node", RestartCount: 2},
				{Name: "sidecar", RestartCount: 1},
			},
		},
	})

	c := consensus.NewNoPodRestartsWithClient(clientset, "test-ns", "app=cn")
	ok, err := c.Check(context.Background(), buildNoPodRestartsReq(cutover))

	// Pod restarts are an expected non-green state — not an error.
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, 3, c.TotalRestarts(), "TotalRestarts must sum all container restart counts")
}

// Test_NoPodRestarts_IgnoresPreCutoverPods verifies that pods created before
// the cutover are not considered, even if they have restarts.
func Test_NoPodRestarts_IgnoresPreCutoverPods(t *testing.T) {
	cutover := time.Now().Add(-1 * time.Hour)
	clientset := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "cn-old",
			Namespace:         "test-ns",
			Labels:            map[string]string{"app": "cn"},
			CreationTimestamp: metav1.NewTime(cutover.Add(-30 * time.Minute)), // before cutover
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "consensus-node", RestartCount: 5},
			},
		},
	})

	c := consensus.NewNoPodRestartsWithClient(clientset, "test-ns", "app=cn")
	ok, err := c.Check(context.Background(), buildNoPodRestartsReq(cutover))

	require.NoError(t, err)
	assert.False(t, ok, "pre-cutover pods must be ignored; no post-cutover pods → not-green")
}

// Test_NoPodRestarts_Name verifies the criterion name is stable.
func Test_NoPodRestarts_Name(t *testing.T) {
	c := consensus.NoPodRestarts{}
	assert.Equal(t, "NoPodRestarts", c.Name())
}
